package planner

import (
	"testing"

	"vm-migrate/internal/model"
)

func TestGroupByAppGroupAndOrder(t *testing.T) {
	vms := []model.VSphereVM{
		{Name: "web-01", AppGroup: "frontend"},
		{Name: "web-02", AppGroup: "frontend"},
		{Name: "web-03", AppGroup: "frontend"},
		{Name: "db-01", AppGroup: "database"},
		{Name: "misc-01"}, // no tag -> ungrouped
	}

	groups := GroupByAppGroup(vms)
	if len(groups["frontend"]) != 3 {
		t.Errorf("expected 3 frontend VMs, got %d", len(groups["frontend"]))
	}
	if len(groups["database"]) != 1 {
		t.Errorf("expected 1 database VM, got %d", len(groups["database"]))
	}
	if len(groups["ungrouped"]) != 1 {
		t.Errorf("expected 1 ungrouped VM, got %d", len(groups["ungrouped"]))
	}

	order := SuggestWaveOrder(groups)
	// smallest groups first: database(1) and ungrouped(1) before frontend(3)
	if len(groups[order[len(order)-1]]) < len(groups[order[0]]) {
		t.Errorf("expected ascending size order, got order %v", order)
	}
	if order[len(order)-1] != "frontend" {
		t.Errorf("expected largest group 'frontend' last, got order %v", order)
	}
}
