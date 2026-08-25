package steampipe

import (
	"testing"

	"vm-migrate/internal/model"
)

func TestQueryParsesJSONOutput(t *testing.T) {
	c := &Client{Binary: "testdata/fake_steampipe.sh"}

	var vms []model.VSphereVM
	if err := c.Query("select * from vsphere_vm", &vms); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(vms) != 1 {
		t.Fatalf("expected 1 VM, got %d", len(vms))
	}
	if vms[0].Name != "web-01" || vms[0].NumCPU != 4 {
		t.Errorf("unexpected VM data: %+v", vms[0])
	}
}
