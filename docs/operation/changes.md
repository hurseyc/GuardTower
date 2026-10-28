---
title: Review & apply changes
section: Operation
order: 26
summary: Preview, roll out and roll back settings changes safely, with canaries, health checks and snapshots.
routes: [/changes]
---

# Review & apply changes

The **Review & apply** page takes the settings you defined on the [Configuration](configuration.md) page and puts them on your instances. Every change is previewed first, rolled out one instance at a time, checked for health, and undone automatically if something breaks.

## Observe mode

GuardTower starts in **observe mode**. It collects logs and compares settings but **never changes an instance**. While it is on, the page shows a banner, and **Apply** is disabled.

When you're happy with the plan, an admin turns observe mode off from the banner or under [Settings](../admin/settings.md#observe-mode). You can turn it back on at any time, for example during an exam week or while you're away.

## Planning a change

1. Pick a **target**: the whole fleet, one group, every instance with a tag, or a single instance.
2. Optionally limit the change to one **section** (for example only *Filter lists*). Otherwise every managed section is included.
3. Click **Preview**. The plan lists every affected instance in the order it will be updated, with each pending change (`set`, `add`, `remove`, `update`).

The plan also shows **warnings**, for example:

- an instance is **offline** and will be skipped;
- no **canary** instance is affected, so the first instance in the list acts as the canary.

Instances that are already in sync are listed as *no changes* and are not touched.

## How a rollout runs

Click **Apply** and confirm. GuardTower then works through the instances **one at a time**, canaries first. For each instance it:

1. **Snapshots** the instance's current settings (*before apply*).
2. **Applies** the changes through the AdGuard Home API.
3. Waits a moment for AdGuard Home to settle, then runs a **health check**:
   - the API answers and reports that DNS is running, and
   - a real DNS query for a random name under `guardtower-health.example` gets an answer. *NXDOMAIN* is fine; *SERVFAIL*, *REFUSED* or no reply means the instance cannot resolve (for example, unreachable upstreams).
4. **Verifies** that the instance now matches its desired settings.
5. Takes a second snapshot (*after apply*), which becomes the reference for [drift detection](#drift).

If any step fails, GuardTower **restores the instance from its *before* snapshot**, marks the rollout *rolled back*, and **skips every remaining instance**, so one bad change never reaches the rest of a group. If the restore itself fails, the rollout is marked *failed* and the step shows what went wrong. Fix it from the instance's page.

The rollout page updates live. Each step shows its status (*pending*, *running*, *succeeded*, *rolled back*, *skipped*, *failed*) and the detail of what happened. Only one rollout runs at a time.

### Choosing canaries

Mark your least critical instance as **canary** in the instance's settings (for example, a test LXC or the one in the garage). Canary instances are always updated first, so a problem shows up there before it reaches the instances your household depends on.

## Snapshots and restore

Every instance page has a **Snapshots** card listing the snapshots taken before and after each change, plus any you took by hand with **Take snapshot**.

**Restore** puts every managed setting back to the snapshot's values. It runs as a small rollout of its own: GuardTower takes a *before restore* snapshot first, then restores and health-checks, so a restore can be undone too. Restores are refused while observe mode is on.

## Drift

After a successful apply, GuardTower remembers what it put on each instance. If someone later changes that instance directly in AdGuard Home's own UI, GuardTower flags the section as **drifted** (re-checked every 2 minutes):

- the instance page's **Settings sync** card shows a red *drifted* badge and a warning;
- a notification appears in GuardTower.

You then have two choices:

- **Undo the hand edit**: click **Review & apply** on the instance, which puts the managed settings back.
- **Keep the hand edit**: import it into the right layer on the [Configuration](configuration.md#getting-started-import-a-baseline) page, so it becomes the desired setting.

## Permissions

Viewers can see plans and rollout history. Operators and admins can preview, apply, take snapshots and restore. Only admins can switch observe mode.

Every rollout, snapshot and restore is recorded in the [audit log](../admin/audit-log.md).
