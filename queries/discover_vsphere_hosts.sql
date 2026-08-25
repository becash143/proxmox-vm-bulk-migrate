-- Discovery query against theapsgroup/vsphere's vsphere_host table.
-- Column names verified against table_host.go in
-- theapsgroup/steampipe-plugin-vsphere @ v0.2.1 (the Steampipe Hub
-- docs page for this table doesn't render a static column list, so
-- these are read off the plugin's Go source, not the hub page).
--
-- Used for target-capacity sizing (how much CPU/RAM headroom exists
-- per ESXi host today) and for readiness checks that key off host
-- status (e.g. don't plan migrations off a 'red' host).
select
  name,
  moref,
  vendor,
  model,
  cpu,
  cpu_cores,
  cpu_threads,
  cpu_mhz,
  num_nics,
  num_hbas,
  memory,
  status,
  cpu_usage,
  memory_usage,
  uptime,
  product
from vsphere_host;
