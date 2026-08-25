package readiness

import (
	"encoding/json"
	"os"
	"testing"

	"vm-migrate/internal/model"
)

func loadFixture(t *testing.T) []model.VSphereVM {
	t.Helper()
	b, err := os.ReadFile("../../testdata/vsphere_vms.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var vms []model.VSphereVM
	if err := json.Unmarshal(b, &vms); err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	return vms
}

func TestPoweredOnCheck(t *testing.T) {
	vms := loadFixture(t)
	findings := Run(vms)

	found := false
	for _, f := range findings {
		if f.VM == "web-01" && f.Control == "powered_on_during_assessment" {
			found = true
			if f.Status != Alarm {
				t.Errorf("expected web-01 powered-on check to alarm, got %s", f.Status)
			}
		}
	}
	if !found {
		t.Fatal("did not find powered_on_during_assessment finding for web-01")
	}
}

func TestWindowsVirtIOCheck(t *testing.T) {
	vms := loadFixture(t)
	findings := Run(vms)

	for _, f := range findings {
		if f.VM == "db-01" && f.Control == "windows_virtio_prep_needed" {
			if f.Status != Alarm {
				t.Errorf("expected db-01 (Windows guest) to alarm on VirtIO prep, got %s", f.Status)
			}
			return
		}
	}
	t.Fatal("did not find windows_virtio_prep_needed finding for db-01")
}

func TestNamingCheckFlagsPlaceholder(t *testing.T) {
	vms := loadFixture(t)
	findings := Run(vms)

	for _, f := range findings {
		if f.VM == "New Virtual Machine" && f.Control == "unnamed_or_duplicate_identity" {
			if f.Status != Alarm {
				t.Errorf("expected placeholder-named VM to alarm, got %s", f.Status)
			}
			return
		}
	}
	t.Fatal("did not find naming finding for placeholder VM")
}

func TestSummarizeCounts(t *testing.T) {
	vms := loadFixture(t)
	findings := Run(vms)
	counts := Summarize(findings)

	total := counts[OK] + counts[Alarm] + counts[Info]
	want := len(vms) * len(AllControls)
	if total != want {
		t.Errorf("expected %d total findings (%d VMs x %d controls), got %d", want, len(vms), len(AllControls), total)
	}
}
