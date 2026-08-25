# Testing checklist

You're the first person to run this against a real cluster — everything
below has only been tested against mocks. Go in that order: it's
designed so the risky step (an actual migration) is last, and you can
stop after any step and still have given useful feedback.

## 0. Before you start

- Point this at **non-production VMs only** — a couple of throwaway
  test VMs on the ESXi side is ideal.
- You'll need Steampipe installed and configured with:
  - the `theapsgroup/vsphere` plugin, pointed at your vCenter/ESXi
  - your Proxmox Steampipe plugin, pointed at your PVE cluster
- You'll need a Proxmox API token (`Datacenter > Permissions > API
  Tokens`) with rights to create VMs on the target node.

## 1. Build it

```
go build -o vmmigrate ./cmd/vmmigrate
```

If this fails, that's the first useful bug report — send me the error.

## 2. Discover (read-only, safe)

```
./vmmigrate discover
```

**This is very likely to fail on the first try** — `queries/discover_proxmox.sql`
assumes a table/column layout I guessed at (`proxmox_vm` with `vmid,
name, node, status, cores, maxmem`). If your plugin's schema is
different, edit that file to match, then re-run.

**What to send back:** the exact error if it fails, or the success
line (`Discovered N vSphere VM(s) and M Proxmox VM(s)...`) if it works.

## 3. Check (read-only, safe)

```
./vmmigrate check
```

**What to send back:** the full output. I want to know if the alarms
make sense for your actual VMs (e.g. does it correctly flag a
powered-on VM, a Windows VM, etc.) or if anything looks wrong/missing.

## 4. Plan (read-only, safe)

```
./vmmigrate plan
```

Everything will probably land in "ungrouped" unless you've tagged VMs
with a custom attribute — that's expected for now, see the README.

## 5. Assign + dry-run migrate (still safe — no API calls)

```
./vmmigrate assign \
  --wave test \
  --vm <a moref from your discover output> \
  --node <your pve node> \
  --vmid <a free vmid> \
  --esxi-storage <the storage ID you registered for ESXi> \
  --esxi-path "ha-datacenter/<vm folder>/<vm>.vmdk"

./vmmigrate migrate --wave test --dry-run
```

**What to send back:** does the dry-run output look right for your
environment?

## 6. The real test: one actual migration

Only do this once steps 1–5 look right, and only against a VM you're
fully OK with re-creating if something goes wrong.

```
export PVE_URL=https://your-pve-host:8006
export PVE_TOKEN_ID="root@pam!vm-migrate"
export PVE_TOKEN_VALUE="your-token-secret"

./vmmigrate migrate --wave test --concurrency 1
```

**What to send back, whether it works or not:**
- The full console output
- If it fails: which step failed (VM creation? task polling? something
  else), and the exact error text
- If it succeeds: does the resulting Proxmox VM look right? Right CPU/
  memory, boots correctly, disk attached?

## 7. Drift check (after the VM boots)

```
./vmmigrate drift --wave test
```

**What to send back:** any drift findings it reports, and whether they
match reality (e.g. if it flags a firmware mismatch, was there
actually one?).

---

Anything that breaks is useful — the whole point of you testing this
is to find the gap between "works against my mocks" and "works against
a real cluster." Don't feel like you need to get all the way to step 6
for this to be a worthwhile test.
