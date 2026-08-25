// Package planner turns a raw VM list into wave assignments. v1
// deliberately uses the cheap, honest approach discussed up front:
// group by an operator-supplied tag (vSphere custom attribute /
// naming convention) rather than inferring dependencies from flow
// data, which is a real but much larger project to do well. This
// still beats migrating blind, and it ships now.
package planner

import (
	"fmt"
	"sort"

	"vm-migrate/internal/model"
)

// GroupByAppGroup buckets VMs by their AppGroup field (populated from
// a vSphere custom attribute/tag at discovery time -- wire that up in
// discover_vsphere.sql once you decide which attribute to use).
// VMs with no group land in a catch-all "ungrouped" wave so nothing
// silently falls through planning.
func GroupByAppGroup(vms []model.VSphereVM) map[string][]model.VSphereVM {
	groups := map[string][]model.VSphereVM{}
	for _, vm := range vms {
		key := vm.AppGroup
		if key == "" {
			key = "ungrouped"
		}
		groups[key] = append(groups[key], vm)
	}
	return groups
}

// SuggestWaveOrder returns group names sorted by size (ascending),
// on the simple, explainable heuristic that you rehearse the process
// on your smallest, lowest-risk group first and save the biggest,
// most business-critical group for last once the process is proven.
// This is a starting default, not a policy -- operators should
// override manually for anything with a hard compliance or
// dependency constraint.
func SuggestWaveOrder(groups map[string][]model.VSphereVM) []string {
	names := make([]string, 0, len(groups))
	for k := range groups {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool {
		return len(groups[names[i]]) < len(groups[names[j]])
	})
	return names
}

// Summary renders a quick human-readable plan preview.
func Summary(groups map[string][]model.VSphereVM, order []string) string {
	out := ""
	for i, name := range order {
		out += fmt.Sprintf("Wave %d: %-20s %d VM(s)\n", i+1, name, len(groups[name]))
	}
	return out
}
