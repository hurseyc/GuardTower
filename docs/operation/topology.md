---
title: Topology map
section: Operation
order: 32
summary: See where every DNS server runs, which group it serves, and what would fail together.
routes: [/topology]
---

# Topology map

The **Topology** page draws your fleet so you can see at a glance what depends on what.

## Views

- **Physical** shows sites (if you recorded any), then hosts (with their address), then the instances running on each host. Instances without a host appear under *No host recorded*. Lines from **DNS clients** show how much traffic each instance answers: the thicker the line, the more queries per second over the last hour.
- **By group** shows each settings group with its redundancy score and lines to its instances. Each instance shows the host it runs on.

Each instance card shows its status (green = online, red = offline), name, address, AdGuard Home version and queries per second. The coloured left edge is its group's colour.

## Spotting single points of failure

- A host drawn with a **dashed amber outline** is the *only* host of a group. If it restarts, that group has no DNS. The label says which group.
- Instances with an amber border are named in a [resilience finding](resilience.md).
- In the *By group* view, dashed amber lines connect a group to instances that share one host.

## Focusing a group

Click a group chip above the map, or a group node in the *By group* view, to focus it. Everything else fades and the group's findings are listed above the map. Click it again, or **All groups**, to clear the focus. The focus and view are kept in the page address, so you can bookmark or share them.

Click any instance to open its page. Use the controls in the corner to zoom or fit the map, and the minimap to pan.
