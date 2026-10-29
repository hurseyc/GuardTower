---
title: The overview dashboard
section: Operation
order: 10
summary: Reading the fleet overview, status indicators and live updates.
routes: [/]
---

# The overview dashboard

The **Overview** page is GuardTower's home screen. It updates live: instance status is polled every 15 seconds and pushed to your browser as it changes.

## Summary cards

| Card | Meaning |
|---|---|
| **Instances online** | Instances that answered their last status check, out of all enabled instances. |
| **Protection** | Whether filtering is enabled everywhere. Lists any instance where it is paused or off. |
| **Groups** | Number of settings groups and how many instances are ungrouped. |
| **AdGuard Home versions** | Distinct versions in the fleet. More than one is highlighted, because mixed versions usually mean an upgrade is pending. |

## Instance cards

Instances are shown by group. Each card shows:

- a **status dot**: green and pulsing when online, red when offline;
- the AdGuard Home **version**;
- **protected** / **protection off**, or how long ago an offline instance was last seen (hover for the error);
- its **host**, **tags** and **API latency**;
- a **canary** badge if the instance receives changes first.

Click a card to open the [instance detail](instance-detail.md) page.

## Notifications

When an instance goes offline or comes back, a notification appears in the bottom-right corner. The sidebar footer always shows a one-glance health bar for the whole fleet.

## Light, dark or Auto

The theme button in the header (sun, moon or monitor icon) switches between **Light**, **Dark** and **Auto**. Auto follows your operating system and changes with it, for example when your computer switches to dark mode in the evening. The choice is remembered per browser, and you can also pick it from your user menu or by typing *theme* in the command palette (<kbd>⌘K</kbd>).

## Keyboard shortcuts

| Keys | Action |
|---|---|
| <kbd>⌘K</kbd> / <kbd>Ctrl K</kbd> | Search pages, instances and documentation, or switch the theme |
| <kbd>?</kbd> | Open help for the current page |
