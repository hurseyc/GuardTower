---
title: Family DNS
section: Operation
order: 50
summary: See which people and devices try to reach adult and other blocked sites, and when, with masking, bypass signals and alerts.
routes: [/family]
---

# Family DNS

**Family DNS** turns the fleet's query logs into something a parent can act on. Instead of a single "% blocked" number, it shows **who** tried to reach adult (and other notable) sites, **from which device**, **when**, and whether the attempt was blocked.

The section is only visible to **admins** and users with **family access** (set per user under [Users](../admin/users.md)).

## How attempts are counted

When a page loads, a device often looks up the same site dozens of times (images, trackers, retries). GuardTower groups those lookups into **attempts**: one device, one site, one category, with gaps of 5 minutes or less merged. "Sam: 3 attempts" means three separate times Sam's device went to such a site, not three hundred DNS queries. The number of lookups is still shown on each attempt.

## Categories

Every query is checked against, in order:

1. **AdGuard Home's verdict**: a *parental control* block is **Adult**; a *Safe Browsing* block is **Malware**; a forced safe search is counted as **Safe search**.
2. **Blocked services**: e.g. TikTok is **Social media**, Roblox is **Gaming**.
3. **The filter list that blocked it**. Well-known adult lists (HaGeZi NSFW, OISD NSFW, StevenBlack porn and others with *nsfw*, *porn* or *adult* in their name or URL) count as **Adult**; gambling lists as **Gambling**, and so on. General ad-blocking lists count as **Ads & tracking**, which never appears here. Change any list's category under **Family DNS → Settings**.
4. **Built-in site lists** for the best-known adult, gambling, VPN/private-DNS, social and gaming sites. These also catch lookups that were **not blocked**, which is how GuardTower can tell you about gaps in your filtering.

| Category | Examples |
|---|---|
| Adult | Parental-control blocks, NSFW lists |
| Gambling | Betting and casino sites |
| VPN & private DNS | Encrypted DNS resolvers (DoH/DoT), VPN providers, Tor |
| Social media | TikTok, Instagram, Snapchat, Discord |
| Gaming | Roblox, Fortnite, Steam |
| Malware | Safe Browsing blocks, threat lists |

## Overview

- **Adult-site attempts**, **Not blocked** (adult sites that resolved: your filtering has a gap), **During bedtime** and **Safe search enforced**, for the last 7, 30 or 90 days.
- **Bypass & coverage signals** (see below).
- One card per person with their adult attempts, attempts during bedtime, the time of the last attempt and the other categories. Devices nobody owns are grouped under **Unassigned devices**.

## A person's page

- **When attempts happen**: a heatmap by weekday and hour.
- **Devices**: attempts per device.
- **Trend**: attempts per day.
- **Most attempted sites** and the **Attempts** table: time, device, category, site, number of lookups, and whether it was blocked. A moon marks attempts during bedtime.

Use the category chips to show gambling, social media or other categories alongside (or instead of) adult sites.

### Masking

Adult and gambling domains are **masked** by default (`p***hub.com`) so the screen is safe to look at with others around. Click the eye icon to reveal one. Every reveal is recorded in the [audit log](../admin/audit-log.md) with who revealed it; the log itself only stores the masked name. Turn masking off under **Settings** if you prefer.

## People & devices

Add each person (with an optional emoji), then assign their devices in **Devices seen this week**. A device is whatever AdGuard Home records as the client: an IP address, or a ClientID if the device uses encrypted DNS with one. Devices with attempts that nobody owns are listed first.

> **Tip:** give each child's devices a fixed IP (a DHCP reservation on your router) or a ClientID, so attempts always land on the right person. Assigning a device also applies to its past attempts.

## Bypass & coverage signals

| Signal | Meaning |
|---|---|
| **VPN & private DNS** | A device looked up a VPN provider, Tor, or an encrypted-DNS resolver such as `dns.google`. Browsers and apps can use these to get around your DNS filtering. |
| **Not blocked** | A device reached adult sites that weren't blocked. Turn on parental control or add an adult list for its group (see *Family profile*). |
| **Went quiet** | An assigned device that normally uses your DNS servers hasn't for over a day while others have. It may simply be off or away, or it may be using a VPN or private DNS. |
| **Unprotected** | A person's devices use an instance where parental control or safe search is off. |

## Family profile

Admins can turn on family protection for a whole group under **Family DNS → Settings → Family profile**. It adds to the group's settings layers:

- **Parental control** (AdGuard's adult-content service),
- **Safe search** on Google, Bing, DuckDuckGo, Ecosia, Yandex, Pixabay and YouTube,
- the **HaGeZi NSFW** block list,
- rules that block well-known **private-DNS resolvers, VPNs and Tor**, and make Firefox switch off its own DNS-over-HTTPS.

If the group's filtering isn't managed by GuardTower yet, its current lists and custom rules are first copied from one of its online instances, so the profile only *adds* to them. Nothing changes on your instances yet: you are taken to [Review & apply](changes.md) to preview and roll the change out like any other.

## Alerts and the weekly digest

With at least one [notification target](../admin/notifications.md), GuardTower can push an alert when a new attempt starts, for example *"Sam (iPad) tried an adult site: 23:14 during bedtime: p\*\*\*hub.com (blocked)"*. Alerts are limited to one per device and category every 30 minutes, respect quiet hours and stay masked unless masking is off. People who have GuardTower open see the same alert in the app.

The weekly digest (Sundays at 18:00 by default) summarises each person's attempts, bedtime attempts and any bypass signals.

All of this is set under **Family DNS → Settings**; see [Settings](../admin/settings.md#family-dns) for every option.

## Privacy

Everything stays on your GuardTower server. Attempts are kept for a year by default (**Keep family history**), independently of the raw query log, so you can see long-term trends without keeping every query.
