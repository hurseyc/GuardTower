---
title: Try the demo
section: Getting started
order: 3
summary: Explore every screen with a simulated five-instance fleet, without touching your own DNS.
---

# Try the demo

Demo mode runs GuardTower against a **simulated fleet**: five AdGuard Home look-alikes inside the GuardTower process, with a week of generated traffic. Nothing on your network is contacted, and nothing is kept when GuardTower stops.

```bash
docker run --rm -p 8080:8080 -e GT_DEMO=true ghcr.io/hurseyc/guardtower:latest
```

or, with a downloaded binary: `guardtower serve --demo`.

Open `http://<host>:8080` and sign in as **demo** / **guardtower-demo** (the login page has a button that fills these in).

## What's in it

- **Two groups.** *Main network* (agh-1 to agh-3) and *Kids VLAN* (agh-4, agh-5) on three hosts. Both Kids VLAN instances run on the same NAS, so the [resilience](../operation/resilience.md) check flags a single point of failure.
- **Settings layers.** Base holds the shared DNS, filter lists and access settings, and Kids VLAN turns on parental control. The group also has **Safe Search** waiting to be applied: open [Review & apply](../operation/changes.md) to roll it out canary first. Observe mode is off in the demo, so changes are applied to the simulated instances.
- **Traffic and history.** About 280,000 queries over seven days, with a daily rhythm, ad and tracker blocks, and late-night lookups from the kids' devices. New queries keep arriving every second.
- **Family DNS.** People *Sam* (iPad, laptop) and *Alex* (phone) with adult, gambling and bypass attempts, plus an unassigned device in the inbox. Apply the [Family profile](../operation/family.md) to Kids VLAN and watch blocked attempts replace allowed ones.
- **Upgrades.** agh-2 runs an older AdGuard Home with the built-in updater, so it can be [upgraded](../operation/upgrades.md) from the Upgrades page.

The simulation only answers the parts of the AdGuard Home API that GuardTower uses. It is not a DNS server you can point devices at.
