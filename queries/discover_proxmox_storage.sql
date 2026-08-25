-- Discovery query against becash143/proxmox's proxmox_storage table.
-- Column names verified against table_proxmox_storage.go in
-- github.com/becash143/steampipe-plugin-proxmox (tag v1.0.2, same
-- release as discover_proxmox.sql / discover_proxmox_nodes.sql).
--
-- This is per-node storage status, not one row per distinct backend:
-- a shared storage target (is_shared = true) mounted on every node
-- shows up once per node, all under the same `storage` id. Dedupe on
-- `storage` if you only want the distinct backends -- e.g. to check
-- the ESXi import target itself (see README) has room, group by
-- `storage` first.
select
  storage,
  node,
  type,
  content,
  is_active,
  used,
  avail,
  total,
  is_shared
from proxmox_storage;
