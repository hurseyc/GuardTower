---
title: Hosts, sites & failure domains
section: Operation
order: 30
summary: Describe where instances physically run so single points of failure can be detected.
routes: [/hosts]
---

# Hosts, sites & failure domains

Two DNS servers only give you redundancy if they can't fail together. GuardTower models where each instance runs so it can point out when they would.

## The model

```
 Site (Home, Office)
  └─ Power / UPS (optional)
      └─ Host (pve-01, synology)
          └─ Instance (agh-1, agh-2)
```

| Level | Example | Fails together when… |
|---|---|---|
| **Site** | Home, Cabin | The internet or power at that location goes down |
| **Power** | `UPS-rack` | A UPS or circuit trips |
| **Host** | `pve-01`, `synology` | The machine reboots, crashes or is updated |

## Adding hosts

1. Go to **Hosts & sites → New host**.
2. Enter a name, the type (Proxmox node, NAS, Docker host…) and optionally an address, a site and a power/UPS label.
3. Edit each instance and choose its host under **Runs on host**.

Hosts with the same **Power / UPS** value are treated as one power failure domain.

## Example

With Main network = `agh-1` (pve-01), `agh-2` (pve-02), `agh-3` (synology), and Kids VLAN = `agh-4` and `agh-5`, both on synology:

- **Main network** is redundant: its instances are on three different hosts.
- **Kids VLAN** has a **single point of failure**. If the NAS reboots, both of its DNS servers go down together. Move one of them to a Proxmox node to fix this.

GuardTower shows this on the Overview, Groups, Instances and Hosts pages and on the [topology map](topology.md). See [Single points of failure](resilience.md) for every check it makes.
