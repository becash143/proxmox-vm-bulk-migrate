-- Discovery query against your Proxmox Steampipe plugin.
-- Adjust table/column names to match what your plugin actually
-- exposes (this tool doesn't assume a specific plugin name since
-- yours is custom) -- the Go side only cares that the JSON keys
-- returned match the `json` tags on model.ProxmoxVM: vmid, name,
-- node, status, cores, maxmem.
select
  vmid,
  name,
  node,
  status,
  cores,
  maxmem
from proxmox_vm;
