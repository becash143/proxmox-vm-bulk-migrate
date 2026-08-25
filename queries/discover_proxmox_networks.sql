-- Discovery query against becash143/proxmox's proxmox_network table.
-- Column names verified against table_proxmox_network.go in
-- github.com/becash143/steampipe-plugin-proxmox (tag v1.0.2, same
-- release as the other discover_proxmox_*.sql files).
--
-- This is the Proxmox half of the roadmap's "Network/SDN mapping
-- table (vSphere port group -> Proxmox bridge/VLAN)" item -- pair
-- with discover_vsphere_networks.sql. Per-node interface config
-- (bridges, bonds, VLANs); building the actual port-group-to-bridge
-- mapping is still a manual/future step, this just gives you both
-- sides' raw inventory to build it from.
select
  node,
  iface,
  type,
  is_active,
  is_autostart,
  address,
  netmask,
  gateway,
  method
from proxmox_network;
