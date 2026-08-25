-- Discovery query against theapsgroup/vsphere.
-- Column names verified against hub.steampipe.io/plugins/theapsgroup/vsphere/tables/vsphere_vm
-- firmware / disk_controller are not native columns on every plugin
-- version; if your install doesn't expose them, drop them here and
-- the readiness checks that depend on them will just report "unknown"
-- (see internal/readiness) rather than failing.
select
  name,
  moref,
  num_cpu,
  memory_size,
  power,
  guest_full_name,
  host_moref
from vsphere_vm;
