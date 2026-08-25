// Package steampipe shells out to the `steampipe` CLI to run SQL
// against whatever plugins are configured in the caller's workspace
// (theapsgroup/vsphere and your proxmox plugin, in this tool's case).
//
// We deliberately do NOT link against Steampipe's Postgres wire
// protocol or FDW internals. `steampipe query --output json` is the
// stable, documented integration surface, and it's what lets this
// tool work against any plugin without caring how that plugin
// authenticates or paginates internally.
package steampipe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// Client runs queries against a local steampipe installation.
type Client struct {
	// Binary is the path to the steampipe executable. Defaults to
	// "steampipe" (resolved via PATH) if empty.
	Binary string
	// Timeout bounds how long a single query is allowed to run.
	Timeout time.Duration
}

func New() *Client {
	return &Client{Binary: "steampipe", Timeout: 2 * time.Minute}
}

// Query runs sql and unmarshals the JSON array of row objects into
// dest (a pointer to a slice, e.g. *[]model.VSphereVM).
//
// Steampipe's `--output json` prints an array of row objects keyed by
// column name, which is why this works generically for any query
// shape as long as the destination struct's `json` tags match the
// column names selected in sql.
func (c *Client) Query(sql string, dest interface{}) error {
	bin := c.Binary
	if bin == "" {
		bin = "steampipe"
	}

	cmd := exec.Command(bin, "query", sql, "--output", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("steampipe query failed: %w\nstderr: %s", err, stderr.String())
	}

	if err := json.Unmarshal(stdout.Bytes(), dest); err != nil {
		return fmt.Errorf("failed to parse steampipe JSON output: %w\nraw: %s", err, stdout.String())
	}
	return nil
}

// QueryFile is identical to Query but reads SQL from disk, which is
// how the readiness-check controls are shipped (see internal/readiness).
func (c *Client) QueryFile(path string, dest interface{}) error {
	sql, err := readFile(path)
	if err != nil {
		return err
	}
	return c.Query(sql, dest)
}
