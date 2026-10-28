---
title: Groups & settings layers
section: Operation
order: 20
summary: How groups, base settings and per-instance overrides fit together.
routes: [/groups]
---

# Groups & settings layers

A **group** is a set of instances that should share the same AdGuard Home settings. Typical examples:

| Group | Members | Purpose |
|---|---|---|
| Main network | 3 instances | Adults and infrastructure, standard ad blocking |
| Kids VLAN | 2 instances | Strict family filtering, safe search, bedtime schedules |

## The three layers

Every instance's effective configuration is built from three layers, applied in order:

```
 Base settings  ──▶  Group settings  ──▶  Instance overrides  =  Effective config
 (whole fleet)       (e.g. Kids VLAN)     (rare exceptions)
```

1. **Base** holds what every instance should share: upstream DNS servers, common block lists, query-log retention.
2. **Group** adds to or replaces base values for its members, such as an extra adult-content list or enforced safe search.
3. **Instance overrides** are for genuine one-offs, such as a different upstream on one server. Keep them rare.

Lists that have a natural key (filter lists by URL, rewrites by domain, clients by name) are **merged**: a group can add a filter list without repeating the base ones, and can explicitly *remove* an inherited item.

## Rules

- An instance belongs to **at most one** settings group. This means two groups can never disagree about one instance.
- An instance with no group receives only the base settings.
- **Tags** are separate from groups. Use them for filtering, dashboards and batching upgrades (for example `nas` or `lxc`); they never decide settings.

## Managing groups

- **New group**: give it a name, an optional description and a colour, which is used throughout the UI.
- **Manage members**: tick the instances that belong to the group. Ticking an instance that is in another group moves it.
- **Delete group**: members become ungrouped. Nothing is pushed to AdGuard Home until you apply a plan.

## Redundancy

For DNS to keep working when a server fails, each group should have at least two instances **on different hosts**. Record where instances run under [Hosts & sites](hosts.md).
