---
title: Analytics
section: Operation
order: 35
summary: Traffic, blocking and client charts across the fleet.
routes: [/analytics]
---

# Analytics

The **Analytics** page charts DNS traffic across your fleet. Use the controls at the top to choose a time range (1 hour to 30 days) and to narrow it to one **group** or one **instance**. The address bar keeps your choices, so views can be bookmarked.

## Headline numbers

| Card | Meaning |
|---|---|
| **DNS queries** | Total queries in the range |
| **Blocked** | Share of queries blocked by filter lists, blocked services, Safe Browsing or parental control |
| **Avg. processing** | Average time AdGuard Home took to answer, including upstream time for uncached answers |
| **Served from cache** | Share answered from AdGuard Home's cache |
| **Active clients** | Distinct client addresses seen |

## Charts

- **Queries over time**: all queries and blocked queries per interval. Hover for exact numbers.
- **Traffic by instance**: how load is spread across instances. A very uneven split usually means clients are pointed at one server, so the others aren't really sharing the load. Each instance keeps the same colour on every chart.
- **Blocking rate by instance**: the blocked percentage per instance. Instances in a stricter group (for example a kids' network) should show a higher rate.
- **Top queried / blocked domains** and **Top clients**: click any entry to open the [query log](query-log.md) filtered to it.
- **Outcome**: how queries were handled. Click an outcome to see those queries.
- **When queries happen**: total queries by weekday and hour, which makes unusual activity (such as traffic at 3 a.m.) easy to spot.

## Long ranges

For ranges of a day or more, totals and charts come from **hourly statistics**, which are kept much longer than raw queries (see [Settings](../admin/settings.md#data-retention)). Top lists for those ranges are built from each hour's top 100 entries per instance, so very rare domains can drop out of long-range top lists; the query log always has the full detail.
