# vmmigrate

> **Testing this for someone?** See [TESTING.md](TESTING.md) for a
> step-by-step checklist ordered safe-first (read-only steps before
> any real migration).

A bulk migration planner and orchestrator that fills the gap between
Proxmox VE's native ESXi Import Wizard (excellent, but one VM at a
time, in the GUI) and an actual fleet migration: discovery, pre-flight
readiness checks, wave planning, concurrent bulk import, and
post-migration drift detection.

It reads inventory from **Steampipe** (the `theapsgroup/vsphere`
plugin and your own Proxmox plugin), keeps its own durable plan on
disk, and drives migration through the documented Proxmox VE REST API
`import-from` mechanism — the same one the GUI wizard uses internally.

## Why this architecture

Steampipe tables are **read-only, live facts** — they don't persist
between runs and can't hold a decision like "this VM is in wave 2."
So this tool is deliberately split in two:

- **Steampipe = discovery/fact layer.** `vmmigrate discover` shells
  out to `steampipe query --output json` and snapshots both sides.
- **vmmigrate's own state file = planning/history layer.** Waves,
  VM-to-target mappings, migration status, and drift history live in
  a single JSON file (`vmmigrate-state.json` by default), written
  atomically so a crash mid-write can't corrupt it.

No database server, no external Go dependencies — this builds and
runs with just the Go standard library, so it stays portable and easy
to audit.

## Requirements

- Go 1.22+ (go.mod pins 1.22.2; CI builds against 1.22 — on an older
  1.21.x toolchain, `go build` will try to auto-fetch 1.22 over the
  network, which fails in offline/locked-down environments, so just
  use 1.22+ directly)
- `steampipe` installed (see steampipe.io/downloads) and configured
  with:
  - `theapsgroup/vsphere` plugin, connected to your vCenter/ESXi —
    `steampipe plugin install theapsgroup/vsphere`, then add a
    connection in `~/.steampipe/config/vsphere.spc`
  - your Proxmox Steampipe plugin, connected to your PVE cluster —
    same pattern, its own `.spc` connection file
  - This tool assumes you already have both plugins installed and
    query-able via `steampipe query "select * from vsphere_vm"`
    (or your Proxmox table) before you touch `vmmigrate` at all —
    get that working first, independent of this tool, if it isn't
    already.
- A Proxmox API token (`Datacenter > Permissions > API Tokens`) with
  rights to create VMs and read tasks on the target node(s)
- An ESXi/vCenter host already registered as Proxmox storage
  (`Datacenter > Storage > Add > ESXi`) — this tool imports through
  that storage target, it does not register it for you

## Build

```
go build -o vmmigrate ./cmd/vmmigrate
```

**Run it from the repo root** (or copy the `queries/` directory
alongside wherever you put the binary). `discover`'s SQL file flags
default to `queries/discover_vsphere.sql` / `queries/discover_proxmox.sql`,
resolved relative to your current directory, not the binary's
location — build it, move the binary to e.g. `/usr/local/bin`, and
`vmmigrate discover` will fail with a `no such file or directory`
error that looks like a Steampipe problem but isn't. Either stay in
the repo root, or pass `--vsphere-sql`/`--proxmox-sql` explicitly.
The state file (`vmmigrate-state.json` by default) is also resolved
relative to your current directory — run every `vmmigrate` command
from the same working directory, or pass `--state` explicitly each
time.

## Workflow

```
# 1. Snapshot both sides
./vmmigrate discover

# 2. Run pre-flight readiness checks (powered-on VMs, Windows/VirtIO
#    prep, EFI firmware mapping, naming issues)
./vmmigrate check --alarms-only

# 3. See a suggested wave grouping (by app_group tag/attribute --
#    wire this up in queries/discover_vsphere.sql once you've decided
#    which vSphere custom attribute or tag to key off of)
./vmmigrate plan

# 4. Assign each VM to a wave with its migration target details
./vmmigrate assign \
  --wave frontend \
  --vm vm-101 \
  --node pve1 --vmid 301 \
  --esxi-storage esxi-source \
  --esxi-path "ha-datacenter/web-01/web-01.vmdk"

# 5. Dry-run first, always
./vmmigrate migrate --wave frontend --dry-run

# 6. Then for real, with bounded concurrency
export PVE_URL=https://pve1.example.local:8006
export PVE_TOKEN_ID="root@pam!vm-migrate"
export PVE_TOKEN_VALUE="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
./vmmigrate migrate --wave frontend --concurrency 3 --stop-on-error

# 7. After VMs boot and settle, check for config drift against the
#    pre-migration baseline (CPU, memory, firmware/BIOS mismatches)
./vmmigrate drift --wave frontend

# 8. Anytime: see where everything stands
./vmmigrate status
```

## What's real vs. what you need to verify in your environment

**Verified against current documentation/forum-confirmed behavior:**
- Proxmox VE 8.2+ ESXi Import Wizard mechanism and the underlying
  `import-from=<storage>:<path>` disk syntax used by
  `POST /nodes/{node}/qemu` (this is literally how the GUI wizard
  works internally, per Proxmox staff confirmation).
- `steampipe query <sql> --output json` as a stable JSON integration
  surface.
- `theapsgroup/vsphere` plugin's `vsphere_vm` table columns
  (`name`, `moref`, `num_cpu`, `memory_size`, `power`, `guest_full_name`).

**You need to confirm/adjust for your setup:**
- Your Proxmox plugin's actual table/column names in
  `queries/discover_proxmox.sql` — this repo assumes a
  `proxmox_vm` table with `vmid, name, node, status, cores, maxmem`;
  swap these to match your plugin's real schema.
- Whether your vSphere plugin install exposes `firmware` — if not,
  the firmware readiness check reports `info` (not a false `ok`)
  rather than silently skipping.
- The exact `import-from` parameter set for VMs with multiple disks
  or non-SCSI controllers — `ImportSpec` in
  `internal/proxmoxapi/client.go` covers the single-scsi-disk case;
  extend `CreateFromESXiImport` for multi-disk VMs against your PVE
  version's live API viewer (`https://<host>:8006/pve-docs/api-viewer/`)
  before relying on it for anything business-critical.
- `app_group` tagging strategy — v1 groups VMs by a vSphere custom
  attribute/tag you choose and populate; it does not infer
  dependencies from network flow data. That's the honest, cheap
  version discussed as the v1 scope — a flow-based dependency
  clusterer is a legitimate v2, not a v1 requirement.

## Project layout

```
cmd/vmmigrate/          CLI entrypoint (discover, check, plan, assign, migrate, drift, status)
internal/model/         Shared VM fact structs (VSphereVM, ProxmoxVM)
internal/steampipe/     Exec wrapper around `steampipe query --output json`
internal/state/         Durable JSON-file store: waves, mappings, drift history
internal/readiness/     Pre-flight checks (powered-on, Windows/VirtIO, EFI firmware, naming)
internal/planner/       Wave grouping by app_group tag
internal/proxmoxapi/    Minimal Proxmox REST client (create-via-import, task polling, config fetch)
internal/orchestrator/  Concurrent bulk migration engine
internal/drift/         Post-migration baseline-vs-live config comparison
queries/                Discovery SQL, kept out of Go code so it's editable without a rebuild
testdata/                Fixture data used by unit tests (no live steampipe/Proxmox required to test)
```

## Testing

`internal/planner`, `internal/readiness`, `internal/state`, and
`internal/steampipe` have unit tests that run without any live
steampipe/vSphere/Proxmox connection — the Steampipe wrapper is
tested against a fake shell script standing in for the real binary,
and readiness/planner logic runs against static JSON fixtures.

```
go test ./...
```

`internal/drift`, `internal/orchestrator`, and `internal/model`
currently have no test files (`go test ./...` will report
`[no test files]` for these) — same as the CLI (`cmd/vmmigrate`) and
the Proxmox HTTP client (`internal/proxmoxapi`), which are untested
for the documented reason that they need a live Proxmox API to
exercise meaningfully. Treat the orchestrator and drift packages in
particular as read-the-code-before-trusting-it until tests land for
them.

## Roadmap (not built yet, scoped honestly)

- Flow-log-based dependency clustering instead of manual tagging
- Network/SDN mapping table (vSphere port group → Proxmox
  bridge/VLAN) with its own readiness control
- Multi-disk / multi-NIC import spec generation
- Rollback command (delete the Proxmox VM, unmark mapping) for failed
  waves
- Hosted multi-user version (swap the JSON state file for Postgres)
