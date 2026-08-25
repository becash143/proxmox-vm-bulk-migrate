// Package proxmoxapi is a minimal Proxmox VE REST API client covering
// exactly what bulk migration needs: registering an ESXi source as
// storage, creating a VM whose disk is populated via `import-from`
// (the same mechanism the GUI Import Wizard uses), and polling the
// resulting task.
//
// Endpoints and the import-from disk syntax below are the documented/
// forum-confirmed mechanism (Proxmox staff response, "everything the
// GUI does is done via the API... we use the import-from=<source
// volume> syntax for the new volumes that then imports the data from
// esxi", pve forum thread 151974). This is NOT a guess -- but the
// exact per-VM parameter set (which disk bus, how many disks, NIC
// count) is genuinely VM-specific, so BuildImportParams below is a
// starting point you should verify against
// https://<your-pve-host>:8006/pve-docs/api-viewer/ for your exact
// PVE version before relying on it for anything you can't afford to
// get wrong.
package proxmoxapi

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string // e.g. https://pve.example.local:8006
	TokenID    string // e.g. root@pam!vm-migrate
	TokenValue string
	HTTP       *http.Client
}

func New(baseURL, tokenID, tokenValue string, insecureSkipVerify bool) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		TokenID:    tokenID,
		TokenValue: tokenValue,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureSkipVerify}, //nolint:gosec -- ESXi/PVE hosts commonly run self-signed certs internally; make this configurable, don't hardcode it in production.
			},
		},
	}
}

func (c *Client) authHeader() string {
	return fmt.Sprintf("PVEAPIToken=%s=%s", c.TokenID, c.TokenValue)
}

func (c *Client) do(method, path string, form url.Values) (map[string]interface{}, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Authorization", c.authHeader())

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("proxmox API %s %s -> HTTP %d: %s", method, path, resp.StatusCode, string(raw))
	}

	var parsed struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("unexpected response shape from %s: %w (raw: %s)", path, err, string(raw))
	}

	var out map[string]interface{}
	// `data` can be a string (task UPID), an object, or an array
	// depending on endpoint; callers that need typed data should
	// unmarshal parsed.Data themselves. This generic path covers the
	// common object case and stuffs scalars under "_raw".
	if err := json.Unmarshal(parsed.Data, &out); err != nil {
		out = map[string]interface{}{"_raw": string(parsed.Data)}
	}
	return out, nil
}

// NextVMID asks the cluster for a free VMID, mirroring the
// `cluster/resources` scan pattern used in the community PowerShell
// scripts referenced in the Proxmox forums.
func (c *Client) NextVMID() (int, error) {
	data, err := c.do(http.MethodGet, "/api2/json/cluster/nextid", nil)
	if err != nil {
		return 0, err
	}
	raw, ok := data["_raw"].(string)
	if !ok {
		return 0, fmt.Errorf("unexpected nextid response: %+v", data)
	}
	var id int
	if _, err := fmt.Sscanf(strings.Trim(raw, `"`), "%d", &id); err != nil {
		return 0, fmt.Errorf("parsing nextid %q: %w", raw, err)
	}
	return id, nil
}

// ImportSpec describes one VM to create via the ESXi import path.
type ImportSpec struct {
	Node          string // target Proxmox node
	VMID          int
	Name          string
	Cores         int
	MemoryMB      int
	TargetStorage string // e.g. "local-lvm"
	ESXiStorageID string // storage ID you registered for the ESXi host, e.g. "esxi-source"
	ESXiDiskPath  string // path Proxmox reports under that storage, e.g. "ha-datacenter/MyVM/MyVM.vmdk"
	Bridge        string // network bridge, e.g. "vmbr0"
}

// CreateFromESXiImport issues the POST /nodes/{node}/qemu call using
// the import-from disk syntax, and returns the task UPID for polling.
//
// This mirrors, field for field, the working example posted by
// Proxmox forum members automating the same workflow via Ansible/
// PowerShell: scsi0=<storage>:0,import-from=<esxi-storage>:<path>.
func (c *Client) CreateFromESXiImport(spec ImportSpec) (upid string, err error) {
	form := url.Values{}
	form.Set("vmid", fmt.Sprintf("%d", spec.VMID))
	form.Set("name", spec.Name)
	form.Set("cores", fmt.Sprintf("%d", spec.Cores))
	form.Set("memory", fmt.Sprintf("%d", spec.MemoryMB))
	form.Set("scsihw", "virtio-scsi-single")
	form.Set("net0", fmt.Sprintf("virtio,bridge=%s", spec.Bridge))
	form.Set("scsi0", fmt.Sprintf("%s:0,import-from=%s:%s",
		spec.TargetStorage, spec.ESXiStorageID, spec.ESXiDiskPath))

	data, err := c.do(http.MethodPost, fmt.Sprintf("/api2/json/nodes/%s/qemu", spec.Node), form)
	if err != nil {
		return "", err
	}
	if raw, ok := data["_raw"].(string); ok {
		return strings.Trim(raw, `"`), nil
	}
	return "", fmt.Errorf("unexpected create-VM response, no task UPID found: %+v", data)
}

// TaskStatus polls a running task's state.
type TaskStatus struct {
	Status     string `json:"status"`     // "running" or "stopped"
	ExitStatus string `json:"exitstatus"` // "OK" on success when stopped
}

func (c *Client) TaskStatus(node, upid string) (TaskStatus, error) {
	path := fmt.Sprintf("/api2/json/nodes/%s/tasks/%s/status", node, url.PathEscape(upid))
	data, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return TaskStatus{}, err
	}
	b, _ := json.Marshal(data)
	var ts TaskStatus
	_ = json.Unmarshal(b, &ts)
	return ts, nil
}

// WaitForTask polls until the task stops or the timeout elapses.
func (c *Client) WaitForTask(node, upid string, timeout time.Duration) (TaskStatus, error) {
	deadline := time.Now().Add(timeout)
	for {
		ts, err := c.TaskStatus(node, upid)
		if err != nil {
			return ts, err
		}
		if ts.Status == "stopped" {
			return ts, nil
		}
		if time.Now().After(deadline) {
			return ts, fmt.Errorf("timed out waiting for task %s on node %s", upid, node)
		}
		time.Sleep(3 * time.Second)
	}
}

// GetVMConfig fetches the live config of an imported VM, used by the
// drift checker to compare against the pre-migration vSphere baseline.
func (c *Client) GetVMConfig(node string, vmid int) (map[string]interface{}, error) {
	path := fmt.Sprintf("/api2/json/nodes/%s/qemu/%d/config", node, vmid)
	return c.do(http.MethodGet, path, nil)
}
