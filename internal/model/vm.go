// Package model defines the normalized facts pulled from Steampipe's
// vsphere and proxmox plugins. Steampipe returns plugin-specific JSON
// column sets; these structs normalize just the fields the migration
// tool actually needs, so the rest of the codebase never touches raw
// Steampipe output directly.
package model

import "time"

// VSphereVM mirrors the subset of columns exposed by the
// theapsgroup/vsphere plugin's vsphere_vm table that matter for
// migration planning and readiness checks.
//
// Reference columns (hub.steampipe.io/plugins/theapsgroup/vsphere):
// name, moref, num_cpu, memory_size, power, guest_full_name,
// host_moref, storageconsumed (jsonb per-datastore), tags/custom
// attributes (via join, plugin-dependent on your install).
type VSphereVM struct {
	Name           string    `json:"name"`
	MoRef          string    `json:"moref"`
	NumCPU         int       `json:"num_cpu"`
	MemoryMB       int64     `json:"memory_size"`
	Power          string    `json:"power"` // poweredOn / poweredOff
	GuestFullName  string    `json:"guest_full_name"`
	HostMoRef      string    `json:"host_moref"`
	Firmware       string    `json:"firmware"`        // bios / efi
	DiskController string    `json:"disk_controller"` // best-effort, see readiness notes
	AppGroup       string    `json:"app_group"`       // sourced from vSphere custom attribute/tag, optional
	CapturedAt     time.Time `json:"captured_at"`
}

// ProxmoxVM mirrors the subset of columns exposed by the becash143/proxmox
// Steampipe plugin's proxmox_vm table (hub.steampipe.io/plugins/becash143/proxmox/tables/proxmox_vm).
// Confirmed against the plugin's published docs: name, vm_id, node,
// status, cpus, max_mem.
//
// Unit note: max_mem passes through Proxmox's own API value, which is
// bytes -- unlike VSphereVM.MemoryMB above (vSphere's memory_size is
// natively MB). Don't compare these two fields directly without
// converting; nothing in this codebase does today (MaxMemBytes is
// captured but not yet consumed anywhere), but it will be a trap for
// whoever wires it into a capacity check next.
type ProxmoxVM struct {
	VMID        int       `json:"vm_id"`
	Name        string    `json:"name"`
	Node        string    `json:"node"`
	Status      string    `json:"status"` // running / stopped
	Cores       int       `json:"cpus"`
	MaxMemBytes int64     `json:"max_mem"`
	CapturedAt  time.Time `json:"captured_at"`
}

// VSphereHost mirrors the theapsgroup/vsphere plugin's vsphere_host
// table (source-verified against table_host.go in
// theapsgroup/steampipe-plugin-vsphere @ v0.2.1 -- the Steampipe Hub
// docs page for this table doesn't render its column list statically,
// so the columns below are read off the plugin's Go table definition
// rather than the hub page).
//
// memory is bytes (host physical RAM); memory_usage is MB (current
// usage, per the plugin's own doc comment) -- another same-plugin
// unit mismatch like the vsphere/proxmox memory one noted on
// VSphereVM/ProxmoxVM, so don't compare memory and memory_usage
// directly without converting.
type VSphereHost struct {
	Name        string `json:"name"`
	MoRef       string `json:"moref"`
	Vendor      string `json:"vendor"`
	Model       string `json:"model"`
	CPU         string `json:"cpu"`
	CPUCores    int16  `json:"cpu_cores"`
	CPUThreads  int16  `json:"cpu_threads"`
	CPUMhz      int32  `json:"cpu_mhz"`
	NumNics     int32  `json:"num_nics"`
	NumHbas     int32  `json:"num_hbas"`
	MemoryBytes int64  `json:"memory"`
	Status      string `json:"status"` // green / yellow / red
	CPUUsageMhz int32  `json:"cpu_usage"`
	MemoryUsage int32  `json:"memory_usage"` // MB, not bytes -- see struct doc
	UptimeSec   int32  `json:"uptime"`
	Product     string `json:"product"`

	CapturedAt time.Time `json:"captured_at"`
}

// VSphereDatastore mirrors vsphere_host's sibling table,
// vsphere_datastore (same plugin/version, same sourcing method --
// table_datastore.go). All size fields are bytes.
type VSphereDatastore struct {
	Name             string `json:"name"`
	MoRef            string `json:"moref"`
	CapacityBytes    int64  `json:"capacity"`
	UncommittedBytes int64  `json:"uncommitted"`
	FreeBytes        int64  `json:"free"`
	Accessible       bool   `json:"accessible"`
	Type             string `json:"type"` // VMFS / NFS / vSAN etc.

	CapturedAt time.Time `json:"captured_at"`
}

// VSphereNetwork mirrors vsphere_network (table_network.go, same
// plugin/version). Covers plain port groups/networks as seen by
// vSphere; it does NOT distinguish distributed vs standard switch
// port groups as separate fields -- that distinction, if your install
// needs it, would come from the `type` value or a separate join,
// which this v1 doesn't attempt.
type VSphereNetwork struct {
	Name       string `json:"name"`
	MoRef      string `json:"moref"`
	Type       string `json:"type"`
	IPPoolName string `json:"ip_pool_name"`
	IPPoolID   int32  `json:"ip_pool_id"`
	Accessible bool   `json:"accessible"`

	CapturedAt time.Time `json:"captured_at"`
}

// ProxmoxNode mirrors the becash143/proxmox plugin's proxmox_node
// table (source-verified against table_proxmox_node.go, same repo/tag
// as proxmox_vm below). cpu is a 0-1 usage ratio (DOUBLE), not a
// count -- max_cpu is the actual core count.
type ProxmoxNode struct {
	Node        string  `json:"node"`
	Status      string  `json:"status"`
	CPUUsage    float64 `json:"cpu"`      // 0.0-1.0 ratio
	MaxCPU      int     `json:"max_cpu"`  // core count
	MemBytes    int64   `json:"mem"`      // current usage
	MaxMemBytes int64   `json:"max_mem"`  // capacity
	DiskBytes   int64   `json:"disk"`     // current usage
	MaxDisk     int64   `json:"max_disk"` // capacity
	UptimeSec   int64   `json:"uptime"`
	Type        string  `json:"type"`

	CapturedAt time.Time `json:"captured_at"`
}

// ProxmoxStorage mirrors proxmox_storage (table_proxmox_storage.go).
// This is per-node storage status, not a single cluster-wide row --
// a shared storage backend (e.g. an NFS datastore mounted on every
// node) will appear once per node it's reported from, all with the
// same `storage` id and is_shared=true; dedupe on `storage` if you
// only want the distinct backends.
type ProxmoxStorage struct {
	Storage    string `json:"storage"`
	Node       string `json:"node"`
	Type       string `json:"type"` // dir, nfs, zfs, lvm, etc.
	Content    string `json:"content"`
	IsActive   bool   `json:"is_active"`
	UsedBytes  int64  `json:"used"`
	AvailBytes int64  `json:"avail"`
	TotalBytes int64  `json:"total"`
	IsShared   bool   `json:"is_shared"`

	CapturedAt time.Time `json:"captured_at"`
}

// ProxmoxNetwork mirrors proxmox_network (table_proxmox_network.go)
// -- per-node network interface config (bridges, bonds, VLANs), the
// Proxmox-side counterpart needed to eventually map vSphere port
// groups to Proxmox bridges (see README roadmap).
type ProxmoxNetwork struct {
	Node        string `json:"node"`
	Iface       string `json:"iface"`
	Type        string `json:"type"` // bridge, bond, vlan, etc.
	IsActive    bool   `json:"is_active"`
	IsAutostart bool   `json:"is_autostart"`
	Address     string `json:"address"`
	Netmask     string `json:"netmask"`
	Gateway     string `json:"gateway"`
	Method      string `json:"method"` // static, dhcp, manual

	CapturedAt time.Time `json:"captured_at"`
}

// InventorySnapshot is a single point-in-time discovery result,
// persisted to the state store so planning and drift-checking can
// work off a stable dataset instead of re-querying live every time.
//
// The non-VM fields are optional additions: cmdDiscover only
// populates them when their corresponding --*-sql flag resolves to a
// file that exists, so an existing vmmigrate-state.json written before
// these fields existed just decodes them as empty/nil, and a
// vmmigrate build that only cares about VMs can still ignore them.
type InventorySnapshot struct {
	TakenAt time.Time   `json:"taken_at"`
	VSphere []VSphereVM `json:"vsphere"`
	Proxmox []ProxmoxVM `json:"proxmox"`

	VSphereHosts      []VSphereHost      `json:"vsphere_hosts,omitempty"`
	VSphereDatastores []VSphereDatastore `json:"vsphere_datastores,omitempty"`
	VSphereNetworks   []VSphereNetwork   `json:"vsphere_networks,omitempty"`

	ProxmoxNodes    []ProxmoxNode    `json:"proxmox_nodes,omitempty"`
	ProxmoxStorage  []ProxmoxStorage `json:"proxmox_storage,omitempty"`
	ProxmoxNetworks []ProxmoxNetwork `json:"proxmox_networks,omitempty"`
}
