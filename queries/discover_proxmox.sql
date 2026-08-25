-- Discovery query against the becash143/proxmox Steampipe plugin
-- (hub.steampipe.io/plugins/becash143/proxmox), confirmed published
-- and installable via `steampipe plugin install becash143/proxmox`.
-- Column names verified against the plugin's proxmox_vm table docs:
-- hub.steampipe.io/plugins/becash143/proxmox/tables/proxmox_vm --
-- name, vm_id, node, status, cpus, max_mem. The Go side only cares
-- that the JSON keys returned match the `json` tags on
-- model.ProxmoxVM, which now mirror this schema exactly.
select
  vm_id,
  name,
  node,
  status,
  cpus,
  max_mem
from proxmox_vm;
