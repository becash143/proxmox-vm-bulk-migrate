-- Discovery query against theapsgroup/vsphere's vsphere_datastore
-- table. Column names verified against table_datastore.go in
-- theapsgroup/steampipe-plugin-vsphere @ v0.2.1 (same sourcing note
-- as discover_vsphere_hosts.sql -- read off the Go source, since the
-- hub docs page doesn't render a static schema).
--
-- Used to size the source-side storage per VM's datastore before
-- committing to a wave, and to sanity-check the ESXi storage you've
-- registered in Proxmox (see README "An ESXi/vCenter host already
-- registered as Proxmox storage") actually has room.
select
  name,
  moref,
  capacity,
  uncommitted,
  free,
  accessible,
  type
from vsphere_datastore;
