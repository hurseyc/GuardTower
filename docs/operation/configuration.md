---
title: Configuration & settings layers
section: Operation
order: 25
summary: Manage AdGuard Home settings centrally with Base, group and instance layers.
routes: [/config]
---

# Configuration & settings layers

The **Configuration** page holds the settings you *want* your AdGuard Home instances to have. GuardTower compares them with what each instance actually has and shows the differences. **Nothing is changed on an instance until you apply changes** on the [Review & apply](changes.md) page.

## The three levels

| Level | Applies to | Use it for |
|---|---|---|
| **Base** | Every instance | What the whole fleet shares: upstream DNS, common block lists, query-log retention |
| **Group** | The group's members | What a set of instances does differently, e.g. a *Kids VLAN* group with parental control and Safe Search |
| **Instance override** | One instance | Rare one-offs, such as a different upstream on a single server |

Each instance's desired settings are worked out by starting with Base, then applying its group, then its own overrides.

## How values combine

Every setting row has a switch:

- **On Base:** *on* means GuardTower **manages** the setting; *off* means it is **not managed** and left as it is on each instance.
- **On a group or instance:** *on* means the setting is **set at this level**; *off* means it is **inherited** (the inherited value is shown).

How a value that is set combines with lower levels depends on its type:

| Type | Example | At a higher level you… |
|---|---|---|
| Single value | Rate limit, Blocking mode, Parental control on/off | **replace** the inherited value |
| Ordered list | Upstream DNS servers | **replace** the whole list |
| Set | Custom filtering rules, Disallowed domains, Blocked services | **add** items and **remove** inherited ones |
| Keyed list | Block lists (by URL), DNS rewrites, Persistent clients (by name) | **add or change** items and **exclude** inherited ones (click an inherited item to exclude it) |

So a *Kids VLAN* group can add an adult-content block list and a few custom rules without repeating everything from Base, and an instance can drop one inherited rewrite.

## Sections

| Section | What it covers |
|---|---|
| DNS | Upstream, bootstrap and fallback servers, upstream mode, caching, rate limiting, blocking mode, DNSSEC, EDNS |
| Filter lists & custom rules | Block lists, allow lists, custom rules, the filtering switch and the list update interval |
| DNS rewrites | `domain → address` answers |
| Persistent clients | Named devices and their identifiers, tags and per-client settings |
| Access control | Allowed and disallowed clients, disallowed domains |
| Blocked services | Services blocked by ID (e.g. `tiktok`, `roblox`) and the pause schedule |
| Safe Browsing, Parental control | AdGuard's malware and adult-content services |
| Safe Search | Safe-search enforcement per search engine |
| Query log / Statistics settings | AdGuard Home's own retention and ignored domains |

TLS certificates, DHCP and the web-interface address are deliberately **not** managed: they are specific to each machine.

## Getting started: import a baseline

1. Open **Configuration** with **Base** selected and click **Import from an instance**.
2. Pick the instance whose settings are closest to what you want everywhere, and choose the sections to import.
3. GuardTower copies that instance's current settings into Base. That instance is now in sync, and every other instance shows its differences.
4. Select a group and set what should differ for it.

You can also import into a **group** (for example from the instance that already has the kids' settings).

## Checking sync

- The **Instances** list has a **Settings** column: *in sync*, *N to change*, or *not managed*.
- An instance's page has a **Settings sync** card listing each managed section, the pending changes, and the **origin** of every desired value (Base, the group's name or the instance).
- GuardTower re-checks every 2 minutes and immediately after you save a change.

If an instance's AdGuard Home version doesn't have a setting (for example, *Use ignored-domain list* on older releases), the section shows **unsupported** for that setting instead of a change that can never be applied.

## Applying changes

When the differences look right, open [Review & apply](changes.md) to preview and roll them out, canary first, with automatic rollback.

## Keeping settings in a file (YAML export & import)

Every layer can be saved as one YAML file, to keep in git, review in a pull request or copy to another GuardTower.

- **Export YAML** (Configuration page) downloads all layers: `base`, then `groups` and `instances` by name. Each section uses the same `{add, remove}` and `{upsert, remove}` form as the editor.
- **Import YAML** (admins) uploads or pastes a file and shows a **preview** first: which layers would be created, updated or deleted. Nothing changes until you confirm.
  - Layers in the file replace the stored layer for the same level and section. Layers the file doesn't mention are kept, unless you tick **Delete layers missing from the file**.
  - Groups and instances are matched by **name** and must already exist. If anything in the file is wrong (an unknown section or group, an invalid value), nothing is imported and every problem is listed.
  - An import only changes *desired* settings, like saving in the editor. Use [Review & apply](changes.md) to roll it out.

```yaml
version: 1
base:
  dns:
    upstream_dns: { add: ["https://dns.quad9.net/dns-query"] }
groups:
  Kids VLAN:
    parental:
      enabled: true
```

The same works from scripts: `GET /api/v1/config/yaml` exports, and `POST /api/v1/config/yaml?dry_run=1` (add `&prune=1` to delete missing layers) previews an import of the request body. Drop `dry_run` to apply. Imports are recorded in the [audit log](../admin/audit-log.md).

## Permissions

Viewers and operators can see and export the configuration. Only admins can change layers, import baselines or import YAML.
