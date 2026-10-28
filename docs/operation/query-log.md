---
title: Query log
section: Operation
order: 40
summary: Search every DNS query from every instance, live.
routes: [/logs]
---

# Query log

GuardTower copies each instance's query log into its own database every 10 seconds, so you can search **all instances at once**, keep history for longer than AdGuard Home does, and still see queries from an instance that has since gone offline.

## Searching

| Filter | How it matches |
|---|---|
| **Domain** | The domain **and all its subdomains**: `google.com` finds `google.com`, `www.google.com` and `mail.google.com`, but not `notgoogle.com`. This is the fastest search. |
| **match anywhere** | Tick it (or include a `*`) to find the text anywhere in a domain, e.g. `tracker` finds `tracker.example.net` and `mytracker.io`. |
| **Client** | An exact client IP or ClientID, or part of a client's name. |
| **Outcome** | Allowed, allowlisted, blocked, Safe Browsing, parental, Safe Search or rewritten. **Any block** combines every kind of block. |
| **Time range** | Limit results to the last 15 minutes up to 7 days. |
| **Group / instance** | Limit results to one settings group or one instance. |

Filters are kept in the page address, so you can bookmark or share a search.

## Reading a row

Click a row to see details: the exact time, AdGuard Home's internal reason, the **filtering rule** that matched and the filter list it came from, any blocked service, the upstream that answered, and the answer itself. From there, one click shows every query for that domain or from that client.

**Outcomes**

| Outcome | Meaning |
|---|---|
| Allowed | Resolved normally |
| Allowlisted | Matched an allow rule (`@@||example.com^`) |
| Blocked | Blocked by a filter list, custom rule or blocked service |
| Safe Browsing | Blocked by AdGuard's malware/phishing service |
| Parental | Blocked by AdGuard's adult-content (parental control) service |
| Safe Search | Rewritten to the safe-search version of a search engine |
| Rewritten | Answered by a DNS rewrite or `/etc/hosts` entry |

## Live updates

With **Live** on, the newest results refresh automatically as queries arrive, and older pages you have already loaded stay put. Turn Live off to freeze the view while you investigate.

## Where the data comes from

- Each instance must have **query logging enabled** in AdGuard Home (*Settings → General settings → Logs configuration*). With logging disabled, GuardTower has nothing to collect.
- If AdGuard Home **anonymises client IPs**, GuardTower stores the anonymised addresses.
- On first contact with an instance, GuardTower imports up to its 20,000 most recent queries, then collects continuously.
- If an instance is unreachable, collection pauses with increasing back-off and catches up when it returns, provided AdGuard Home still holds those queries.
- Collection health per instance is shown under **Admin → [Settings](../admin/settings.md)**.

How long queries are kept is set under [Settings](../admin/settings.md#data-retention).
