// Package drift compares the pre-migration vSphere baseline captured
// in each state.Mapping against the VM's actual live config in
// Proxmox after import, surfacing exactly the class of silent
// mismatch (CPU count, memory, firmware/machine-type) that every
// 2026 migration writeup calls out as a manual, easy-to-miss step.
package drift

import (
	"fmt"

	"vm-migrate/internal/proxmoxapi"
	"vm-migrate/internal/state"
)

// Check compares one mapping's baseline against live Proxmox config
// and returns any mismatches found. Safe to call repeatedly; it does
// not mutate the baseline.
func Check(client *proxmoxapi.Client, m *state.Mapping) ([]state.DriftFinding, error) {
	if m.Baseline == nil {
		return nil, fmt.Errorf("no baseline captured for %s -- drift check requires a snapshot taken at plan time", m.SourceName)
	}
	if m.Status != state.StatusMigrated && m.Status != state.StatusValidated {
		return nil, fmt.Errorf("%s has not completed migration (status=%s)", m.SourceName, m.Status)
	}

	cfg, err := client.GetVMConfig(m.TargetNode, m.TargetVMID)
	if err != nil {
		return nil, fmt.Errorf("fetching live config for vmid %d: %w", m.TargetVMID, err)
	}

	var findings []state.DriftFinding

	if cores, ok := cfg["cores"]; ok {
		coresStr := fmt.Sprintf("%v", cores)
		baselineStr := fmt.Sprintf("%d", m.Baseline.NumCPU)
		if coresStr != baselineStr {
			findings = append(findings, mkFinding(m, "cpu_cores", baselineStr, coresStr))
		}
	}

	if mem, ok := cfg["memory"]; ok {
		memStr := fmt.Sprintf("%v", mem)
		baselineStr := fmt.Sprintf("%d", m.Baseline.MemoryMB)
		if memStr != baselineStr {
			findings = append(findings, mkFinding(m, "memory_mb", baselineStr, memStr))
		}
	}

	if bios, ok := cfg["bios"]; ok {
		biosStr := fmt.Sprintf("%v", bios)
		wantEFI := m.Baseline.Firmware == "efi" || m.Baseline.Firmware == "uefi"
		gotEFI := biosStr == "ovmf"
		if wantEFI != gotEFI {
			findings = append(findings, mkFinding(m, "firmware", m.Baseline.Firmware, biosStr))
		}
	}

	return findings, nil
}

func mkFinding(m *state.Mapping, field, source, target string) state.DriftFinding {
	return state.DriftFinding{
		SourceMoRef: m.SourceMoRef,
		Field:       field,
		Source:      source,
		Target:      target,
	}
}
