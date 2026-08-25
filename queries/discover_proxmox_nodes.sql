-- Discovery query against becash143/proxmox's proxmox_node table.
-- Column names verified against table_proxmox_node.go in
-- github.com/becash143/steampipe-plugin-proxmox (tag v1.0.2, the same
-- release discover_proxmox.sql's proxmox_vm columns are verified
-- against).
--
-- This is the Proxmox-side counterpart to discover_vsphere_hosts.sql
-- -- current CPU/mem/disk headroom per target node, for capacity
-- checks before assigning a wave to a node. Note `cpu` is a 0-1 usage
-- ratio, not a core count (`max_cpu` is the core count).
select
  node,
  status,
  cpu,
  max_cpu,
  mem,
  max_mem,
  disk,
  max_disk,
  uptime,
  type
from proxmox_node;
