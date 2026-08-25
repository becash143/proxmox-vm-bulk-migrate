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
plugin and [`becash143/proxmox`](https://hub.steampipe.io/plugins/becash143/proxmox)),
keeps its own durable plan on disk, and drives migration through the
documented Proxmox VE REST API `import-from` mechanism — the same one
the GUI wizard uses internally.

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
- `steampipe`, plus the `theapsgroup/vsphere` and `becash143/proxmox`
  plugins, installed and connected — see the dedicated section below
- A Proxmox API token (`Datacenter > Permissions > API Tokens`) with
  rights to create VMs and read tasks on the target node(s)
- An ESXi/vCenter host already registered as Proxmox storage
  (`Datacenter > Storage > Add > ESXi`) — this tool imports through
  that storage target, it does not register it for you

## Installing Steampipe and the plugins

This tool doesn't install or configure Steampipe for you — get these
three things working *first*, independently of `vmmigrate`, before
you build or run this repo at all.

### 1. Install Steampipe itself

From [steampipe.io/downloads](https://steampipe.io/downloads), the
current install commands are:

```
# Linux / WSL2
sudo /bin/sh -c "$(curl -fsSL https://steampipe.io/install/steampipe.sh)"

# macOS
brew install turbot/tap/steampipe
```

Confirm it worked:

```
steampipe -v
```

### 2. Install and configure `theapsgroup/vsphere`

```
steampipe plugin install theapsgroup/vsphere
```

This creates `~/.steampipe/config/vsphere.spc`. Edit it to add a
connection (or use the `VSPHERE_SERVER` / `VSPHERE_USER` /
`VSPHERE_PASSWORD` env vars instead — both are documented in the
plugin's own repo). Per the plugin's published README
([theapsgroup/steampipe-plugin-vsphere](https://github.com/theapsgroup/steampipe-plugin-vsphere)),
the connection block looks like:

```hcl
connection "vsphere" {
  plugin                = "theapsgroup/vsphere"
  vsphere_server         = "vcenter.example.local"
  user                   = "svc-vspherero@vsphere.local"
  password               = "your-password"
  allow_unverified_ssl   = true   # common for internal vCenter certs
}
```

Full column/table reference:
[hub.steampipe.io/plugins/theapsgroup/vsphere](https://hub.steampipe.io/plugins/theapsgroup/vsphere).

### 3. Install and configure `becash143/proxmox`

```
steampipe plugin install becash143/proxmox
```

This creates `~/.steampipe/config/proxmox.spc`. The exact connection
arguments (API URL, token ID/secret, TLS options, etc.) are
documented on the plugin's own pages — check both before configuring:

- Hub overview: [hub.steampipe.io/plugins/becash143/proxmox](https://hub.steampipe.io/plugins/becash143/proxmox)
- Source + README: [github.com/becash143/steampipe-plugin-proxmox](https://github.com/becash143/steampipe-plugin-proxmox)

(We deliberately aren't reproducing a connection-block example for
this one the way we did for vsphere above — we could confirm this
plugin's real *table/column* schema against its published docs
(covered below and already reflected in `queries/discover_proxmox.sql`
and `model.ProxmoxVM`), but not the exact connection-argument names,
so rather than guess at those and risk sending you down a debugging
path with a wrong field name, we're pointing you at the source.)

### 4. Verify both, independently of `vmmigrate`

Before touching this tool at all, confirm both plugins actually
return data:

```
steampipe query "select * from vsphere_vm limit 1"
steampipe query "select * from proxmox_vm limit 1"
```

If either of those fails or returns nothing, that's a Steampipe/
plugin-configuration problem to resolve first — `vmmigrate discover`
will hit the exact same failure, just with an extra layer of
indirection that makes it harder to debug.

`queries/discover_proxmox.sql` and `model.ProxmoxVM` are written
against `becash143/proxmox`'s real, published `proxmox_vm` table
schema (`vm_id`, `name`, `node`, `status`, `cpus`, `max_mem`) — this
is confirmed against
[the plugin's table docs](https://hub.steampipe.io/plugins/becash143/proxmox/tables/proxmox_vm),
not a guess.

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

## Discovered resources

`vmmigrate discover` always captures VMs (`vsphere_vm` / `proxmox_vm`
-- required, `discover` fails if either query fails). It also captures
six more resource types, each optional: if the SQL file isn't present,
that resource is just skipped (0 rows), rather than failing the whole
command.

| Resource | Source table | SQL file | Why it's here |
|---|---|---|---|
| vSphere hosts | `vsphere_host` | `queries/discover_vsphere_hosts.sql` | ESXi host CPU/RAM headroom + status, for target sizing |
| vSphere datastores | `vsphere_datastore` | `queries/discover_vsphere_datastores.sql` | Source-side storage capacity/free space |
| vSphere networks | `vsphere_network` | `queries/discover_vsphere_networks.sql` | Port groups/networks -- half of the port-group-to-bridge mapping in the roadmap |
| Proxmox nodes | `proxmox_node` | `queries/discover_proxmox_nodes.sql` | Target node CPU/RAM/disk headroom + status |
| Proxmox storage | `proxmox_storage` | `queries/discover_proxmox_storage.sql` | Target storage capacity, per node (dedupe on `storage` for shared backends) |
| Proxmox networks | `proxmox_network` | `queries/discover_proxmox_networks.sql` | Bridges/bonds/VLANs per node -- the other half of the network mapping |

All six are new as of this change and verified the same way the
existing `discover_vsphere.sql` / `discover_proxmox.sql` were: against
the plugins' own Go table definitions (`table_host.go`,
`table_datastore.go`, `table_network.go` in
`theapsgroup/steampipe-plugin-vsphere` @ v0.2.1; `table_proxmox_node.go`,
`table_proxmox_storage.go`, `table_proxmox_network.go` in
`becash143/steampipe-plugin-proxmox` @ v1.0.2) rather than the Hub
docs pages, which don't render a static column list for these tables.
Both plugins expose more tables than this tool uses today --
`vsphere_vm`/`host`/`datastore`/`network` is the vSphere plugin's
complete table set (4 total), but `becash143/proxmox` also has
`proxmox_container`, `proxmox_pool`, `proxmox_task`,
`proxmox_cluster_resource`, and `proxmox_user` that nothing here
queries yet -- `proxmox_pool` in particular could replace/complement
the vSphere-tag-based `app_group` wave grouping if you'd rather group
by Proxmox resource pool.

None of these six feed into `check`, `plan`, or `drift` yet -- they're
captured and stored in the snapshot (`vsphere_hosts`, `vsphere_datastores`,
`vsphere_networks`, `proxmox_nodes`, `proxmox_storage`, `proxmox_networks`
in `InventorySnapshot`) so the data's there, but wiring e.g. a
"does the target node have enough free RAM for this wave" readiness
check, or the actual port-group-to-bridge mapping, is still open work.

## Workflow

```
# 1. Snapshot both sides -- VMs plus hosts/datastores/networks/nodes/storage
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
- [`becash143/proxmox`](https://hub.steampipe.io/plugins/becash143/proxmox)
  plugin's `proxmox_vm` table columns (`name`, `vm_id`, `node`,
  `status`, `cpus`, `max_mem`), per the plugin's own published docs —
  `queries/discover_proxmox.sql` and `model.ProxmoxVM` now match this
  exactly, this is no longer a guess.

**You need to confirm/adjust for your setup:**
- `max_mem` from `proxmox_vm` is bytes (it passes through Proxmox's
  own API value), while `memory_size` from `vsphere_vm` is natively
  MB — nothing in this codebase compares the two directly today
  (`ProxmoxVM.MaxMemBytes` is captured but not yet consumed
  anywhere), but if you wire it into a capacity check or comparison
  later, convert first.
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
                        (VMs are required; hosts/datastores/networks/nodes/storage are
                        optional -- see "Discovered resources" above)
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
  bridge/VLAN) with its own readiness control -- raw inventory for
  both sides is now captured (`discover_vsphere_networks.sql` /
  `discover_proxmox_networks.sql`), but the actual mapping and
  readiness control aren't built
- Capacity-aware wave/target checks using the new host/node/storage
  discovery (`discover_vsphere_hosts.sql`, `discover_proxmox_nodes.sql`,
  `discover_*_storage/datastores.sql`) -- captured but not yet
  consumed by `check` or `plan`
- Multi-disk / multi-NIC import spec generation
- Rollback command (delete the Proxmox VM, unmark mapping) for failed
  waves
- Hosted multi-user version (swap the JSON state file for Postgres)
