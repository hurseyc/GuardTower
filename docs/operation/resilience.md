---
title: Single points of failure
section: Operation
order: 31
summary: How GuardTower decides whether a group's DNS servers would fail together, and how to fix it.
---

# Single points of failure

Two DNS servers only protect you if they can't fail at the same time. GuardTower compares where each group's instances run (see [Hosts & sites](hosts.md)) and how they are doing right now, and gives every group a **redundancy score**.

| Score | Meaning |
|---|---|
| **Redundant** | The group survives any one host failing. |
| **At risk** | It survives a host failing, but something else is shared (power), or only one server is answering right now. |
| **Single point of failure** | One failure takes the group's DNS down: it has one instance, or all of them run on the same host. |
| **Down** | None of the group's servers is answering. |
| **Unknown** | Hosts aren't recorded, so GuardTower can't tell. |

## Findings

| Finding | When | How to fix it |
|---|---|---|
| Single DNS server | The group has one enabled instance. | Add a second instance on another host. |
| All on one host | Every instance runs on the same host. | Move one instance to another host. |
| Shared power | Different hosts, but they all have the same *Power / UPS* value. | Put one host on another UPS or circuit. |
| Same site | You recorded several sites and all of the group's hosts are at one of them (informational). | Run one instance at another site if you need site-level redundancy. |
| No host recorded | Some instances have no host. | Set *Runs on host* on each instance. |
| No backup right now | Only one of the group's servers is online. | Bring the others back. |
| Down | No server in the group answers. | Check the instances and hosts. |

Disabled instances don't count as redundancy.

## Where you see it

GuardTower keeps these warnings quiet but visible:

- **Overview**: a *Redundancy* tile and, when something is wrong, a *Single points of failure* card.
- **Groups**: each card shows its score and findings.
- **Instances**: a small amber shield next to the group of an affected instance (hover for details).
- **Hosts & sites**: a host that alone serves a group is tagged *only host of …*.
- **[Topology](topology.md)**: dashed amber outlines around such hosts.
- **Adding or editing an instance**: if the group and host you pick would create a new single point of failure (for example a second Kids VLAN server on the same NAS), the dialog warns you *before* you save.
- **Upgrades** never take the last online server of a group down. See [Upgrading AdGuard Home](upgrades.md).
