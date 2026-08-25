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

// InventorySnapshot is a single point-in-time discovery result,
// persisted to the state store so planning and drift-checking can
// work off a stable dataset instead of re-querying live every time.
type InventorySnapshot struct {
	TakenAt time.Time   `json:"taken_at"`
	VSphere []VSphereVM `json:"vsphere"`
	Proxmox []ProxmoxVM `json:"proxmox"`
}
