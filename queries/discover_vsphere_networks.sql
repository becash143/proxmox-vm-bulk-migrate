-- Discovery query against theapsgroup/vsphere's vsphere_network
-- table. Column names verified against table_network.go in
-- theapsgroup/steampipe-plugin-vsphere @ v0.2.1 (same sourcing note
-- as the other discover_vsphere_*.sql files).
--
-- This is the vSphere half of the roadmap's "Network/SDN mapping
-- table (vSphere port group -> Proxmox bridge/VLAN)" item -- pair
-- this with discover_proxmox_networks.sql once you're ready to build
-- that mapping. On its own it doesn't tell you which VM uses which
-- network; that join would come from vsphere_vm, which this plugin
-- version doesn't expose a network/portgroup column for today (see
-- README's "verify for your setup" notes).
select
  name,
  moref,
  type,
  ip_pool_name,
  ip_pool_id,
  accessible
from vsphere_network;
