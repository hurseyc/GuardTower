---
title: Monitoring with Prometheus
section: Admin
order: 29
summary: Scrape fleet health, sync state, upgrades and redundancy from /metrics.
---

# Monitoring with Prometheus

GuardTower can expose Prometheus metrics at `/metrics`. The endpoint is **off** until you set a token, and scrapers must send that token:

```bash
GT_METRICS_TOKEN=$(openssl rand -hex 24)   # put it in /etc/default/guardtower or your compose file
```

```yaml
# prometheus.yml
scrape_configs:
  - job_name: guardtower
    scheme: http
    static_configs:
      - targets: ["guardtower.lan:8080"]
    authorization:
      type: Bearer
      credentials: "<your GT_METRICS_TOKEN>"
```

## Metrics

Every per-instance metric has `instance` and `group` labels.

| Metric | Meaning |
|---|---|
| `guardtower_instance_up` | 1 if the instance answered its last status check |
| `guardtower_instance_latency_milliseconds` | API latency of the last check |
| `guardtower_instance_protection_enabled` | 1 if filtering is on |
| `guardtower_instance_info{version}` | AdGuard Home version |
| `guardtower_instance_queries_current_hour`, `guardtower_instance_blocked_current_hour` | Queries and blocks since the start of the previous hour |
| `guardtower_collector_last_run_timestamp_seconds`, `guardtower_collector_rows` | Query-log collection health |
| `guardtower_config_pending_changes`, `guardtower_config_drifted_sections` | Settings that differ from the desired state, and hand edits |
| `guardtower_upgrade_available` | 1 if a newer AdGuard Home can be installed |
| `guardtower_group_redundancy{group,score}` | 1 for the group's current [redundancy score](../operation/resilience.md) |
| `guardtower_rollout_active`, `guardtower_observe_mode` | Whether a rollout is running and observe mode is on |
| `guardtower_build_info{version,commit}` | GuardTower build |

Family DNS data is never exported.

## Example alerts

```yaml
groups:
  - name: guardtower
    rules:
      - alert: AdGuardHomeDown
        expr: guardtower_instance_up == 0
        for: 2m
      - alert: DNSGroupWithoutRedundancy
        expr: guardtower_group_redundancy{score=~"spof|down"} == 1
        for: 10m
      - alert: AdGuardHomeChangedOutsideGuardTower
        expr: guardtower_config_drifted_sections > 0
```
