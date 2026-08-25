// vmmigrate is a CLI that fills the bulk-orchestration gap left by
// Proxmox's (excellent, but single-VM) ESXi Import Wizard. It reads
// live inventory from both the vSphere and Proxmox Steampipe plugins,
// keeps its own durable plan (waves, mappings, drift) on disk, runs
// pre-flight readiness checks, and drives bulk import through the
// Proxmox API with concurrency control.
//
// Usage:
//
//	vmmigrate discover                          # snapshot vSphere + Proxmox inventory via steampipe
//	vmmigrate check                              # run readiness controls against the last snapshot
//	vmmigrate plan                               # suggest wave grouping by app_group tag
//	vmmigrate assign --wave <name> --vm <moref> --node <n> --vmid <id> --esxi-storage <s> --esxi-path <p>
//	vmmigrate migrate --wave <name> [--dry-run] [--concurrency 2] [--target-storage local-lvm] [--bridge vmbr0]
//	vmmigrate drift --wave <name>
//	vmmigrate status
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"vm-migrate/internal/drift"
	"vm-migrate/internal/model"
	"vm-migrate/internal/orchestrator"
	"vm-migrate/internal/planner"
	"vm-migrate/internal/proxmoxapi"
	"vm-migrate/internal/readiness"
	"vm-migrate/internal/state"
	"vm-migrate/internal/steampipe"
)

const defaultStatePath = "vmmigrate-state.json"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "discover":
		cmdDiscover(os.Args[2:])
	case "check":
		cmdCheck(os.Args[2:])
	case "plan":
		cmdPlan(os.Args[2:])
	case "assign":
		cmdAssign(os.Args[2:])
	case "migrate":
		cmdMigrate(os.Args[2:])
	case "drift":
		cmdDrift(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`vmmigrate - VMware -> Proxmox bulk migration planner & orchestrator

Commands:
  discover   Snapshot vSphere + Proxmox inventory via steampipe
  check      Run readiness controls against the last snapshot
  plan       Suggest wave grouping by app_group tag
  assign     Assign a VM into a wave with its migration target details
  migrate    Bulk-migrate a wave via the Proxmox import API
  drift      Compare post-migration Proxmox config against baseline
  status     Print current wave/mapping state

Run 'vmmigrate <command> -h' for command-specific flags.`)
}

func openStore(path string) *state.Store {
	st, err := state.Open(path)
	if err != nil {
		fatalf("opening state: %v", err)
	}
	return st
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

// ---- discover ----

func cmdDiscover(args []string) {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "path to state file")
	steampipeBin := fs.String("steampipe-bin", "steampipe", "path to steampipe binary")
	vsphereSQLFile := fs.String("vsphere-sql", "queries/discover_vsphere.sql", "path to vSphere discovery SQL")
	proxmoxSQLFile := fs.String("proxmox-sql", "queries/discover_proxmox.sql", "path to Proxmox discovery SQL")
	vsphereHostsSQLFile := fs.String("vsphere-hosts-sql", "queries/discover_vsphere_hosts.sql", "path to vSphere host discovery SQL (skipped if the file doesn't exist)")
	vsphereDatastoresSQLFile := fs.String("vsphere-datastores-sql", "queries/discover_vsphere_datastores.sql", "path to vSphere datastore discovery SQL (skipped if the file doesn't exist)")
	vsphereNetworksSQLFile := fs.String("vsphere-networks-sql", "queries/discover_vsphere_networks.sql", "path to vSphere network discovery SQL (skipped if the file doesn't exist)")
	proxmoxNodesSQLFile := fs.String("proxmox-nodes-sql", "queries/discover_proxmox_nodes.sql", "path to Proxmox node discovery SQL (skipped if the file doesn't exist)")
	proxmoxStorageSQLFile := fs.String("proxmox-storage-sql", "queries/discover_proxmox_storage.sql", "path to Proxmox storage discovery SQL (skipped if the file doesn't exist)")
	proxmoxNetworksSQLFile := fs.String("proxmox-networks-sql", "queries/discover_proxmox_networks.sql", "path to Proxmox network discovery SQL (skipped if the file doesn't exist)")
	fs.Parse(args)

	sp := steampipe.New()
	sp.Binary = *steampipeBin

	var vms []model.VSphereVM
	if err := sp.QueryFile(*vsphereSQLFile, &vms); err != nil {
		fatalf("querying vSphere via steampipe: %v", err)
	}
	var pvms []model.ProxmoxVM
	if err := sp.QueryFile(*proxmoxSQLFile, &pvms); err != nil {
		fatalf("querying Proxmox via steampipe: %v", err)
	}

	// The VM queries above are mandatory -- this tool is nothing
	// without them. The resource queries below (hosts, datastores,
	// networks, nodes, storage) are additive: skip silently if the
	// SQL file isn't present (e.g. an older checkout or a deliberately
	// trimmed queries/ dir) rather than failing discover entirely, and
	// still fail loudly if the file exists but the query itself
	// errors (bad SQL, plugin not connected, etc.) since that's a
	// real problem worth surfacing.
	now := time.Now().UTC()

	var hosts []model.VSphereHost
	queryOptional(sp, *vsphereHostsSQLFile, &hosts, "vSphere hosts")
	var datastores []model.VSphereDatastore
	queryOptional(sp, *vsphereDatastoresSQLFile, &datastores, "vSphere datastores")
	var networks []model.VSphereNetwork
	queryOptional(sp, *vsphereNetworksSQLFile, &networks, "vSphere networks")

	var nodes []model.ProxmoxNode
	queryOptional(sp, *proxmoxNodesSQLFile, &nodes, "Proxmox nodes")
	var storage []model.ProxmoxStorage
	queryOptional(sp, *proxmoxStorageSQLFile, &storage, "Proxmox storage")
	var pnetworks []model.ProxmoxNetwork
	queryOptional(sp, *proxmoxNetworksSQLFile, &pnetworks, "Proxmox networks")

	for i := range vms {
		vms[i].CapturedAt = now
	}
	for i := range pvms {
		pvms[i].CapturedAt = now
	}
	for i := range hosts {
		hosts[i].CapturedAt = now
	}
	for i := range datastores {
		datastores[i].CapturedAt = now
	}
	for i := range networks {
		networks[i].CapturedAt = now
	}
	for i := range nodes {
		nodes[i].CapturedAt = now
	}
	for i := range storage {
		storage[i].CapturedAt = now
	}
	for i := range pnetworks {
		pnetworks[i].CapturedAt = now
	}

	st := openStore(*statePath)
	st.Inventory = &model.InventorySnapshot{
		TakenAt: now,
		VSphere: vms,
		Proxmox: pvms,

		VSphereHosts:      hosts,
		VSphereDatastores: datastores,
		VSphereNetworks:   networks,

		ProxmoxNodes:    nodes,
		ProxmoxStorage:  storage,
		ProxmoxNetworks: pnetworks,
	}
	if err := st.Save(); err != nil {
		fatalf("saving state: %v", err)
	}

	fmt.Printf("Discovered %d vSphere VM(s) and %d Proxmox VM(s) at %s\n", len(vms), len(pvms), now.Format(time.RFC3339))
	fmt.Printf("Also captured: %d vSphere host(s), %d vSphere datastore(s), %d vSphere network(s), %d Proxmox node(s), %d Proxmox storage row(s), %d Proxmox network interface(s)\n",
		len(hosts), len(datastores), len(networks), len(nodes), len(storage), len(pnetworks))
}

// queryOptional runs sql from sqlFile into dest, skipping quietly if
// sqlFile doesn't exist (this resource just wasn't shipped/enabled)
// but failing loudly (via fatalf, same as the mandatory VM queries)
// if the file exists and the query itself fails -- a present-but-
// broken query is a real problem, not an absent optional extra.
func queryOptional(sp *steampipe.Client, sqlFile string, dest interface{}, label string) {
	if _, err := os.Stat(sqlFile); err != nil {
		return
	}
	if err := sp.QueryFile(sqlFile, dest); err != nil {
		fatalf("querying %s via steampipe: %v", label, err)
	}
}

// ---- check ----

func cmdCheck(args []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "path to state file")
	onlyAlarms := fs.Bool("alarms-only", false, "only print alarm-status findings")
	fs.Parse(args)

	st := openStore(*statePath)
	if st.Inventory == nil {
		fatalf("no inventory snapshot found -- run 'vmmigrate discover' first")
	}

	findings := readiness.Run(st.Inventory.VSphere)
	counts := readiness.Summarize(findings)
	fmt.Printf("Readiness check: %d ok, %d alarm, %d info (%d VM(s), %d control(s) each)\n\n",
		counts[readiness.OK], counts[readiness.Alarm], counts[readiness.Info], len(st.Inventory.VSphere), len(readiness.AllControls))

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "STATUS\tVM\tCONTROL\tREASON")
	for _, f := range findings {
		if *onlyAlarms && f.Status != readiness.Alarm {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", f.Status, f.VM, f.Control, f.Reason)
	}
	w.Flush()
}

// ---- plan ----

func cmdPlan(args []string) {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "path to state file")
	fs.Parse(args)

	st := openStore(*statePath)
	if st.Inventory == nil {
		fatalf("no inventory snapshot found -- run 'vmmigrate discover' first")
	}

	groups := planner.GroupByAppGroup(st.Inventory.VSphere)
	order := planner.SuggestWaveOrder(groups)
	fmt.Print(planner.Summary(groups, order))
	fmt.Println("\nNote: grouping is based on the app_group field (populate this from a vSphere")
	fmt.Println("custom attribute or tag in discover_vsphere.sql). VMs with no tag land in")
	fmt.Println("'ungrouped' -- assign those manually with 'vmmigrate assign' before migrating.")

	// Persist suggested waves as records so `assign` has something to attach to.
	for i, name := range order {
		st.UpsertWave(state.Wave{
			ID:        fmt.Sprintf("wave-%d-%s", i+1, name),
			Name:      name,
			CreatedAt: time.Now().UTC(),
		})
	}
	if err := st.Save(); err != nil {
		fatalf("saving state: %v", err)
	}
}

// ---- assign ----

func cmdAssign(args []string) {
	fs := flag.NewFlagSet("assign", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "path to state file")
	wave := fs.String("wave", "", "wave name (from 'plan' output)")
	moref := fs.String("vm", "", "source VM moref (from 'discover'/'check' output)")
	node := fs.String("node", "", "target Proxmox node")
	vmid := fs.Int("vmid", 0, "target Proxmox VMID")
	esxiStorage := fs.String("esxi-storage", "", "Proxmox storage ID registered against the ESXi host")
	esxiPath := fs.String("esxi-path", "", "VM disk path under that ESXi storage, e.g. ha-datacenter/MyVM/MyVM.vmdk")
	fs.Parse(args)

	if *wave == "" || *moref == "" || *node == "" || *vmid == 0 || *esxiStorage == "" || *esxiPath == "" {
		fatalf("all of --wave --vm --node --vmid --esxi-storage --esxi-path are required")
	}

	st := openStore(*statePath)
	if st.Inventory == nil {
		fatalf("no inventory snapshot found -- run 'vmmigrate discover' first")
	}

	var baseline *model.VSphereVM
	var sourceName string
	for i := range st.Inventory.VSphere {
		if st.Inventory.VSphere[i].MoRef == *moref {
			baseline = &st.Inventory.VSphere[i]
			sourceName = baseline.Name
			break
		}
	}
	if baseline == nil {
		fatalf("VM with moref %s not found in last discovery snapshot", *moref)
	}

	w, ok := st.WaveByName(*wave)
	if !ok {
		nw := state.Wave{ID: fmt.Sprintf("wave-manual-%s", *wave), Name: *wave, CreatedAt: time.Now().UTC()}
		st.UpsertWave(nw)
		w = &nw
	}

	st.SetMapping(&state.Mapping{
		SourceMoRef: *moref,
		SourceName:  sourceName,
		WaveID:      w.ID,
		TargetNode:  *node,
		TargetVMID:  *vmid,
		ESXiStorage: *esxiStorage,
		ESXiVMPath:  *esxiPath,
		Status:      state.StatusPlanned,
		Baseline:    baseline,
	})
	if err := st.Save(); err != nil {
		fatalf("saving state: %v", err)
	}
	fmt.Printf("Assigned %s (%s) to wave %q -> node=%s vmid=%d\n", sourceName, *moref, *wave, *node, *vmid)
}

// ---- migrate ----

func cmdMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "path to state file")
	waveName := fs.String("wave", "", "wave name to migrate")
	dryRun := fs.Bool("dry-run", false, "log actions without calling the Proxmox API")
	concurrency := fs.Int("concurrency", 2, "number of VMs to import in parallel")
	stopOnError := fs.Bool("stop-on-error", false, "abort remaining VMs in the wave on first failure")
	targetStorage := fs.String("target-storage", "local-lvm", "Proxmox storage for imported disks")
	bridge := fs.String("bridge", "vmbr0", "Proxmox network bridge for the new VM's NIC")
	pveURL := fs.String("pve-url", os.Getenv("PVE_URL"), "Proxmox API base URL, e.g. https://pve.local:8006")
	tokenID := fs.String("pve-token-id", os.Getenv("PVE_TOKEN_ID"), "Proxmox API token ID, e.g. root@pam!vm-migrate")
	tokenValue := fs.String("pve-token-value", os.Getenv("PVE_TOKEN_VALUE"), "Proxmox API token secret")
	insecure := fs.Bool("insecure-skip-verify", true, "skip TLS verification (common for internal PVE hosts with self-signed certs)")
	fs.Parse(args)

	if *waveName == "" {
		fatalf("--wave is required")
	}
	if !*dryRun && (*pveURL == "" || *tokenID == "" || *tokenValue == "") {
		fatalf("--pve-url/--pve-token-id/--pve-token-value (or PVE_URL/PVE_TOKEN_ID/PVE_TOKEN_VALUE env vars) are required unless --dry-run")
	}

	st := openStore(*statePath)
	w, ok := st.WaveByName(*waveName)
	if !ok {
		fatalf("wave %q not found -- run 'vmmigrate plan' or 'vmmigrate assign' first", *waveName)
	}
	mappings := st.MappingsInWave(w.ID)
	if len(mappings) == 0 {
		fatalf("wave %q has no assigned VMs -- run 'vmmigrate assign' for each VM first", *waveName)
	}

	var client *proxmoxapi.Client
	if !*dryRun {
		client = proxmoxapi.New(*pveURL, *tokenID, *tokenValue, *insecure)
	}

	opts := orchestrator.DefaultOptions()
	opts.Concurrency = *concurrency
	opts.DryRun = *dryRun
	opts.StopOnError = *stopOnError

	fmt.Printf("Migrating wave %q: %d VM(s), concurrency=%d, dry-run=%v\n", *waveName, len(mappings), *concurrency, *dryRun)
	results := orchestrator.RunWave(client, st, mappings, *targetStorage, *bridge, opts)

	ok_, fail := 0, 0
	for _, r := range results {
		if r.Success {
			ok_++
			fmt.Printf("  OK    %s\n", r.SourceName)
		} else {
			fail++
			fmt.Printf("  FAIL  %s: %v\n", r.SourceName, r.Err)
		}
	}
	fmt.Printf("\n%d succeeded, %d failed\n", ok_, fail)
}

// ---- drift ----

func cmdDrift(args []string) {
	fs := flag.NewFlagSet("drift", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "path to state file")
	waveName := fs.String("wave", "", "wave name to check")
	pveURL := fs.String("pve-url", os.Getenv("PVE_URL"), "Proxmox API base URL")
	tokenID := fs.String("pve-token-id", os.Getenv("PVE_TOKEN_ID"), "Proxmox API token ID")
	tokenValue := fs.String("pve-token-value", os.Getenv("PVE_TOKEN_VALUE"), "Proxmox API token secret")
	insecure := fs.Bool("insecure-skip-verify", true, "skip TLS verification")
	fs.Parse(args)

	if *waveName == "" || *pveURL == "" || *tokenID == "" || *tokenValue == "" {
		fatalf("--wave, --pve-url, --pve-token-id, --pve-token-value are all required")
	}

	st := openStore(*statePath)
	w, ok := st.WaveByName(*waveName)
	if !ok {
		fatalf("wave %q not found", *waveName)
	}
	client := proxmoxapi.New(*pveURL, *tokenID, *tokenValue, *insecure)

	mappings := st.MappingsInWave(w.ID)
	total := 0
	for _, m := range mappings {
		findings, err := drift.Check(client, m)
		if err != nil {
			fmt.Printf("  SKIP  %s: %v\n", m.SourceName, err)
			continue
		}
		if len(findings) == 0 {
			fmt.Printf("  OK    %s: no drift detected\n", m.SourceName)
			m.Status = state.StatusValidated
			st.SetMapping(m)
			continue
		}
		for _, f := range findings {
			f.CheckedAt = time.Now().UTC()
			st.AddDrift(f)
			fmt.Printf("  DRIFT %s: %s baseline=%s actual=%s\n", m.SourceName, f.Field, f.Source, f.Target)
			total++
		}
	}
	if err := st.Save(); err != nil {
		fatalf("saving state: %v", err)
	}
	fmt.Printf("\n%d drift finding(s) across %d VM(s)\n", total, len(mappings))
}

// ---- status ----

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "path to state file")
	asJSON := fs.Bool("json", false, "print raw JSON instead of a table")
	fs.Parse(args)

	st := openStore(*statePath)

	if *asJSON {
		b, _ := json.MarshalIndent(st, "", "  ")
		fmt.Println(string(b))
		return
	}

	if st.Inventory != nil {
		fmt.Printf("Last discovery: %s (%d vSphere, %d Proxmox VMs)\n",
			st.Inventory.TakenAt.Format(time.RFC3339), len(st.Inventory.VSphere), len(st.Inventory.Proxmox))
		if n := len(st.Inventory.VSphereHosts) + len(st.Inventory.VSphereDatastores) + len(st.Inventory.VSphereNetworks) +
			len(st.Inventory.ProxmoxNodes) + len(st.Inventory.ProxmoxStorage) + len(st.Inventory.ProxmoxNetworks); n > 0 {
			fmt.Printf("  + %d vSphere host(s), %d datastore(s), %d network(s); %d Proxmox node(s), %d storage row(s), %d network interface(s)\n",
				len(st.Inventory.VSphereHosts), len(st.Inventory.VSphereDatastores), len(st.Inventory.VSphereNetworks),
				len(st.Inventory.ProxmoxNodes), len(st.Inventory.ProxmoxStorage), len(st.Inventory.ProxmoxNetworks))
		}
		fmt.Println()
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "WAVE\tVM\tSTATUS\tTARGET\tLAST ERROR")
	for _, waveDef := range st.Waves {
		for _, m := range st.MappingsInWave(waveDef.ID) {
			target := "-"
			if m.TargetNode != "" {
				target = fmt.Sprintf("%s/%d", m.TargetNode, m.TargetVMID)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", waveDef.Name, m.SourceName, m.Status, target, m.LastError)
		}
	}
	w.Flush()
}
