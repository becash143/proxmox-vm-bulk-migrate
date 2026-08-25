// Package orchestrator is the bulk-migration engine: for every VM
// mapped into a wave, it calls the Proxmox import API, waits for the
// task, and records the outcome in state. This is the piece that
// doesn't exist anywhere today -- the native wizard is one-VM-at-a-
// time in the GUI; this is the "migrate these 40 VMs, 3 at a time,
// stop on first failure or keep going" capability.
package orchestrator

import (
	"fmt"
	"sync"
	"time"

	"vm-migrate/internal/proxmoxapi"
	"vm-migrate/internal/state"
)

type Options struct {
	Concurrency int           // how many imports run in parallel
	DryRun      bool          // log what would happen, call nothing
	TaskTimeout time.Duration // per-VM import timeout
	StopOnError bool          // abort remaining VMs in the wave on first failure
}

func DefaultOptions() Options {
	return Options{Concurrency: 2, TaskTimeout: 30 * time.Minute, StopOnError: false}
}

// Result is one VM's outcome, returned so the caller can print a
// summary and decide next steps (retry, roll back, proceed to drift
// check).
type Result struct {
	SourceMoRef string
	SourceName  string
	Success     bool
	Err         error
}

// RunWave migrates every planned mapping in a wave. mappings must
// already have TargetNode/TargetVMID/ESXiStorage/ESXiVMPath populated
// (the planner or a manual `vmmigrate assign` step is responsible for
// that -- this package only executes).
func RunWave(client *proxmoxapi.Client, st *state.Store, mappings []*state.Mapping, targetStorage, bridge string, opts Options) []Result {
	if opts.Concurrency < 1 {
		opts.Concurrency = 1
	}

	sem := make(chan struct{}, opts.Concurrency)
	results := make([]Result, len(mappings))
	var wg sync.WaitGroup
	var aborted bool
	var mu sync.Mutex

	for i, m := range mappings {
		i, m := i, m
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			mu.Lock()
			stop := aborted
			mu.Unlock()
			if stop {
				results[i] = Result{SourceMoRef: m.SourceMoRef, SourceName: m.SourceName, Success: false, Err: fmt.Errorf("skipped: wave aborted after earlier failure")}
				return
			}

			res := migrateOne(client, st, m, targetStorage, bridge, opts)
			results[i] = res

			if !res.Success && opts.StopOnError {
				mu.Lock()
				aborted = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return results
}

func migrateOne(client *proxmoxapi.Client, st *state.Store, m *state.Mapping, targetStorage, bridge string, opts Options) Result {
	name := m.SourceName
	moref := m.SourceMoRef

	if opts.DryRun {
		fmt.Printf("[dry-run] would import %s (moref=%s) -> node=%s vmid=%d\n", name, moref, m.TargetNode, m.TargetVMID)
		return Result{SourceMoRef: moref, SourceName: name, Success: true}
	}

	m.Status = state.StatusInProgress
	st.SetMapping(m)
	_ = st.Save()

	spec := proxmoxapi.ImportSpec{
		Node:          m.TargetNode,
		VMID:          m.TargetVMID,
		Name:          name,
		Cores:         cpuOrDefault(m),
		MemoryMB:      memOrDefault(m),
		TargetStorage: targetStorage,
		ESXiStorageID: m.ESXiStorage,
		ESXiDiskPath:  m.ESXiVMPath,
		Bridge:        bridge,
	}

	upid, err := client.CreateFromESXiImport(spec)
	if err != nil {
		return fail(st, m, fmt.Errorf("create VM: %w", err))
	}
	m.UPID = upid
	st.SetMapping(m)
	_ = st.Save()

	ts, err := client.WaitForTask(m.TargetNode, upid, opts.TaskTimeout)
	if err != nil {
		return fail(st, m, fmt.Errorf("waiting for import task: %w", err))
	}
	if ts.ExitStatus != "OK" {
		return fail(st, m, fmt.Errorf("import task finished with exit status %q", ts.ExitStatus))
	}

	m.Status = state.StatusMigrated
	m.LastError = ""
	st.SetMapping(m)
	_ = st.Save()

	return Result{SourceMoRef: moref, SourceName: name, Success: true}
}

func fail(st *state.Store, m *state.Mapping, err error) Result {
	m.Status = state.StatusFailed
	m.LastError = err.Error()
	st.SetMapping(m)
	_ = st.Save()
	return Result{SourceMoRef: m.SourceMoRef, SourceName: m.SourceName, Success: false, Err: err}
}

// cpuOrDefault/memOrDefault pull sizing from the captured baseline if
// present, otherwise fall back to conservative defaults rather than
// failing outright -- an operator can always resize post-import.
func cpuOrDefault(m *state.Mapping) int {
	if m.Baseline != nil && m.Baseline.NumCPU > 0 {
		return m.Baseline.NumCPU
	}
	return 2
}

func memOrDefault(m *state.Mapping) int {
	if m.Baseline != nil && m.Baseline.MemoryMB > 0 {
		return int(m.Baseline.MemoryMB)
	}
	return 4096
}
