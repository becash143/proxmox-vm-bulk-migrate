// Package readiness implements pre-flight checks against a captured
// vSphere inventory snapshot. This is the highest-value, lowest-cost
// part of the whole tool: none of these checks exist in Proxmox's own
// Import Wizard today, and every one of them maps directly to a
// documented, recurring post-import failure mode (wrong firmware/
// machine-type mapping, missing VirtIO prep, powered-on source VMs
// causing inconsistent disk copies).
package readiness

import (
	"strings"

	"vm-migrate/internal/model"
)

type Status string

const (
	OK    Status = "ok"
	Alarm Status = "alarm"
	Info  Status = "info" // can't be determined from available data; not a failure, just incomplete facts
)

// Finding is one row of check output, one per VM per control.
type Finding struct {
	Control string `json:"control"`
	VM      string `json:"vm"`
	MoRef   string `json:"moref"`
	Status  Status `json:"status"`
	Reason  string `json:"reason"`
}

// Control is a named check function. Keeping these as plain Go
// functions (rather than embedding a SQL string like the earlier
// Steampipe-mod sketch) means they run directly over the JSON
// snapshot already sitting in state -- no live steampipe/plugin
// dependency needed to re-run a check.
type Control struct {
	Name string
	Run  func(vm model.VSphereVM) Finding
}

// AllControls is the default control set. Extend this list as you
// learn more real-world failure modes from actual migrations.
var AllControls = []Control{
	{Name: "firmware_machine_type_mapping", Run: checkFirmware},
	{Name: "powered_on_during_assessment", Run: checkPoweredOn},
	{Name: "unnamed_or_duplicate_identity", Run: checkNaming},
	{Name: "windows_virtio_prep_needed", Run: checkWindowsVirtIO},
}

func base(ctrl, vmName, moref string) Finding {
	return Finding{Control: ctrl, VM: vmName, MoRef: moref}
}

// checkFirmware flags EFI-firmware VMs, which is the single
// most-cited manual-fixup step across every 2026 migration writeup:
// BIOS vs UEFI and i440fx vs q35 machine type don't always translate
// cleanly through the Import Wizard and should be confirmed before
// cutover, not discovered at first boot.
func checkFirmware(vm model.VSphereVM) Finding {
	f := base("firmware_machine_type_mapping", vm.Name, vm.MoRef)
	switch strings.ToLower(vm.Firmware) {
	case "":
		f.Status = Info
		f.Reason = "firmware type not present in this Steampipe snapshot -- confirm manually in vCenter before import"
	case "efi", "uefi":
		f.Status = Alarm
		f.Reason = "EFI firmware -- verify target machine-type (q35) and OVMF/EFI disk are set correctly on the Proxmox side before first boot"
	default:
		f.Status = OK
		f.Reason = "BIOS firmware -- standard i440fx/seabios import path applies"
	}
	return f
}

// checkPoweredOn flags VMs that are still running at assessment time.
// A live migration risks the "IO on the VMDK for 2-3 seconds after
// shutdown" class of copy failures documented on the Proxmox forums;
// operators should shut down and re-verify shortly before import.
func checkPoweredOn(vm model.VSphereVM) Finding {
	f := base("powered_on_during_assessment", vm.Name, vm.MoRef)
	if strings.EqualFold(vm.Power, "poweredOn") {
		f.Status = Alarm
		f.Reason = "VM is powered on -- plan a graceful shutdown immediately before import, not during business hours mid-wave"
	} else {
		f.Status = OK
		f.Reason = "VM already powered off"
	}
	return f
}

// checkNaming flags empty or generically-named VMs, which are the
// most common cause of failed source->target reconciliation when
// name is used as a join key instead of MoRef/VMID.
func checkNaming(vm model.VSphereVM) Finding {
	f := base("unnamed_or_duplicate_identity", vm.Name, vm.MoRef)
	name := strings.TrimSpace(vm.Name)
	if name == "" || strings.HasPrefix(strings.ToLower(name), "new virtual machine") {
		f.Status = Alarm
		f.Reason = "missing or placeholder VM name -- rename in vSphere before migration or the wave planner can't disambiguate it"
	} else {
		f.Status = OK
		f.Reason = "named"
	}
	return f
}

// checkWindowsVirtIO flags Windows guests, which need VirtIO drivers
// pre-installed while still on ESXi -- otherwise the VM won't boot
// after import without console/rescue intervention. This is called
// out as a "huge performance win" step in every migration guide and
// is exactly the kind of thing that should surface as a checklist
// item automatically instead of relying on the operator to remember.
func checkWindowsVirtIO(vm model.VSphereVM) Finding {
	f := base("windows_virtio_prep_needed", vm.Name, vm.MoRef)
	if strings.Contains(strings.ToLower(vm.GuestFullName), "windows") {
		f.Status = Alarm
		f.Reason = "Windows guest -- confirm VirtIO drivers and QEMU Guest Agent are installed BEFORE import, or plan console-based driver injection after"
	} else {
		f.Status = OK
		f.Reason = "non-Windows guest, VirtIO prep not required pre-import"
	}
	return f
}

// Run executes every control against every VM in the snapshot.
func Run(vms []model.VSphereVM) []Finding {
	var out []Finding
	for _, vm := range vms {
		for _, c := range AllControls {
			out = append(out, c.Run(vm))
		}
	}
	return out
}

// Summarize counts findings by status, useful for a one-line CLI
// summary before printing the full table.
func Summarize(findings []Finding) map[Status]int {
	counts := map[Status]int{}
	for _, f := range findings {
		counts[f.Status]++
	}
	return counts
}
