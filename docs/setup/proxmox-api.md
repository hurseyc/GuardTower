---
title: Proxmox API token
section: Setup
order: 22
summary: Let GuardTower read which Proxmox node runs each AdGuard Home container or VM, with a read-only token.
---

# Proxmox API token

When GuardTower can read your Proxmox VE cluster, it works out which **node** runs each AdGuard Home LXC or VM and keeps the instance's host up to date, including after you migrate a guest. Single-point-of-failure warnings then follow your real layout without manual edits.

The token is **read-only**. GuardTower lists guests, their nodes and their IP addresses, and never changes anything in Proxmox.

## 1. Create a user and token

In the Proxmox web UI (**Datacenter → Permissions**), or in a shell on any node:

```bash
pveum user add guardtower@pve --comment "GuardTower (read-only)"
pveum acl modify / --users guardtower@pve --roles PVEAuditor
pveum user token add guardtower@pve readonly --privsep 0
```

The last command prints the token **secret** once. Copy it.

- `PVEAuditor` can read guests, nodes and container network settings, which is all GuardTower needs.
- For **VMs**, addresses come from the QEMU guest agent: install `qemu-guest-agent` in the VM and enable *QEMU Guest Agent* in its options. Reading the agent needs an extra privilege that PVEAuditor may not include (`VM.Monitor` on Proxmox VE 8, `VM.GuestAgent.Audit` on 9). Without it, GuardTower matches VMs by **name** instead (see below).

## 2. Connect it in GuardTower

**Hosts & sites → Proxmox VE → Connect Proxmox**:

| Field | Example |
|---|---|
| URL | `https://192.168.10.11:8006` (any node of the cluster) |
| API token ID | `guardtower@pve!readonly` |
| Token secret | the secret from step 1 |
| Accept self-signed certificate | on, unless your nodes have trusted certificates |

**Test connection** shows the Proxmox version and how many guests are visible. **Save & sync** places your instances straight away. After that GuardTower syncs every 5 minutes, and **Sync now** runs it on demand.

## How instances are matched

For each instance, GuardTower looks for a running guest:

1. whose IP address is the instance's address (from its URL or DNS address; hostnames are resolved first), or
2. whose name is the instance's name, or the first part of its hostname (`agh-1.lan` matches a guest called `agh-1`).

A matched instance is placed on the GuardTower host **named after its node**. The host is created (as a *Proxmox node*, with the node's IP) if it doesn't exist yet. The instance's runtime is set to *LXC* or *VM*. Instances that don't match any guest, such as Docker containers on a NAS, keep the host you set by hand.

Every move is recorded in the [audit log](../admin/audit-log.md) as `proxmox.placement`, and signed-in users get a short notice.

## Troubleshooting

- **"rejected the API token"**: check the token ID format `user@realm!name` and the secret.
- **"isn't allowed to read …"**: give the user the `PVEAuditor` role on `/`, and create the token with privilege separation off (`--privsep 0`). Otherwise give the token itself the role.
- **A VM isn't matched**: install the guest agent, or name the VM like the instance.
