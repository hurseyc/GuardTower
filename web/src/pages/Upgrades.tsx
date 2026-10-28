import { useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { AlertTriangle, ArrowUpCircle, CalendarClock, CheckCircle2, ExternalLink, Eye, Pin, RefreshCw, ShieldCheck } from 'lucide-react'
import {
  api,
  type Overview,
  type Rollout,
  type RolloutTarget,
  type UpgradeInstanceState,
  type UpgradeOverview,
  type UpgradePlan,
  type UpgradePolicy,
} from '@/lib/api'
import { useSession } from '@/lib/sessionContext'
import { cn, colorFor, fmtDateTime, timeAgo } from '@/lib/utils'
import { PageHeader } from '@/components/AppShell'
import { Markdown } from '@/components/Markdown'
import { TargetSelect } from '@/components/TargetSelect'
import { targetParam } from '@/lib/target'
import { parseTarget } from '@/lib/target'
import { Badge, Button, Card, CardHeader, ConfirmDialog, EmptyState, Input, Select, Skeleton, Table, Td, Th, Tip } from '@/components/ui'
import { RolloutBadge, RolloutDetail } from './Changes'
import { useRolloutEvents } from '@/lib/rollouts'
import { useOnChange } from '@/lib/useOnChange'
import { driverLabel } from '@/lib/labels'

function StatusCell({ s }: { s: UpgradeInstanceState }) {
  if (!s.online) return <Badge tone="danger">offline</Badge>
  if (s.update_available)
    return (
      <Badge tone="warn">
        <ArrowUpCircle className="mr-1 inline h-3 w-3" />
        {s.target} available
      </Badge>
    )
  if (s.blocked) {
    return (
      <Tip content={s.blocked}>
        <Badge>can't upgrade</Badge>
      </Tip>
    )
  }
  return (
    <Badge tone="ok">
      <CheckCircle2 className="mr-1 inline h-3 w-3" />
      up to date
    </Badge>
  )
}

export function UpgradesPage() {
  const { can } = useSession()
  const qc = useQueryClient()
  const [params, setParams] = useSearchParams()
  const target = parseTarget(params.get('target'))
  const rolloutId = Number(params.get('rollout') ?? 0)
  const [version, setVersion] = useState('')
  const [plan, setPlan] = useState<UpgradePlan | null>(null)
  const [confirm, setConfirm] = useState(false)
  useRolloutEvents()

  const { data } = useQuery({ queryKey: ['upgrades'], queryFn: () => api.get<UpgradeOverview>('/upgrades'), refetchInterval: 30_000 })
  const { data: overview } = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview') })
  const { data: list } = useQuery({
    queryKey: ['rollouts'],
    queryFn: () => api.get<{ rollouts: Rollout[]; active_id: number; observe: boolean }>('/rollouts'),
    refetchInterval: 15_000,
  })
  const setParam = (k: string, v: string) => {
    const n = new URLSearchParams(params)
    if (v) n.set(k, v)
    else n.delete(k)
    setParams(n, { replace: true })
  }

  const check = useMutation({
    mutationFn: () => api.post<UpgradeOverview>('/upgrades/check', {}),
    onSuccess: (d) => {
      qc.setQueryData(['upgrades'], d)
      if (d.check_error) toast.error('Release check failed', { description: d.check_error })
      else toast.success('Checked for AdGuard Home updates')
    },
    onError: (e) => toast.error((e as Error).message),
  })
  const preview = useMutation({
    mutationFn: () => api.post<UpgradePlan>('/upgrades/plan', { target, version }),
    onSuccess: setPlan,
    onError: (e) => toast.error((e as Error).message),
  })
  const start = useMutation({
    mutationFn: () => api.post<{ id: number }>('/upgrades', { target, version }),
    onSuccess: (r) => {
      setConfirm(false)
      setPlan(null)
      setParam('rollout', String(r.id))
      void qc.invalidateQueries({ queryKey: ['rollouts'] })
      toast.success('Upgrade started', { description: 'One instance at a time, canary first.' })
    },
    onError: (e) => {
      setConfirm(false)
      toast.error((e as Error).message)
    },
  })
  useOnChange(`${params.toString()}|${version}`, () => setPlan(null))

  const pickInstance = (id: number) => {
    setParam('target', targetParam({ kind: 'instance', id } as RolloutTarget))
  }
  const upgrades = (list?.rollouts ?? []).filter((r) => r.kind === 'upgrade').slice(0, 10)
  const outdated = data?.instances.filter((i) => i.update_available).length ?? 0
  const busy = !!list?.active_id
  const observe = data?.observe ?? true
  const groupName = (id?: number) => overview?.groups.find((g) => g.id === id)

  return (
    <>
      <PageHeader
        title="AdGuard Home upgrades"
        description="Keep every instance current: GuardTower upgrades one instance at a time, checks it answers DNS on the new version, and rolls back automatically where it can."
        actions={
          can('operator') && (
            <Button onClick={() => check.mutate()} loading={check.isPending}>
              <RefreshCw className="h-4 w-4" /> Check for updates
            </Button>
          )
        }
      >
        {observe && (
          <div className="mt-4 flex flex-wrap items-center gap-3 rounded-xl border border-border bg-subtle p-3 text-sm">
            <Eye className="h-4 w-4 text-info" />
            <span className="flex-1">
              <strong>Observe mode is on.</strong> You can preview upgrades; starting one needs observe mode off (on <Link to="/changes" className="text-accent hover:underline">Review & apply</Link> or under Settings).
            </span>
          </div>
        )}
      </PageHeader>
      <div className="space-y-6 p-6">
        {rolloutId > 0 && <RolloutDetail id={rolloutId} onClose={() => setParam('rollout', '')} />}

        <div className="grid gap-6 xl:grid-cols-3">
          <Card className="xl:col-span-1">
            <CardHeader
              title="Latest release"
              description={
                data?.checks_enabled === false
                  ? 'Release checks are off (Settings).'
                  : data?.checked_at
                    ? `Checked ${timeAgo(data.checked_at)}`
                    : 'Checked every 6 hours from GitHub.'
              }
            />
            <div className="space-y-3 px-5 pb-5">
              {!data ? (
                <Skeleton className="h-24" />
              ) : data.latest ? (
                <>
                  <div className="flex items-baseline gap-3">
                    <span className="font-mono text-2xl font-semibold">{data.latest.tag}</span>
                    {data.latest.published_at > 0 && (
                      <span className="text-xs text-muted" title={fmtDateTime(data.latest.published_at)}>
                        released {timeAgo(data.latest.published_at)}
                      </span>
                    )}
                  </div>
                  <p className="text-sm text-muted">
                    {outdated === 0 ? 'Every instance that can be upgraded is up to date.' : `${outdated} instance${outdated === 1 ? '' : 's'} can be upgraded.`}
                  </p>
                  {data.latest.notes && (
                    <details className="rounded-lg border border-border bg-subtle/50 p-3 text-sm">
                      <summary className="cursor-pointer font-medium">Release notes</summary>
                      <div className="mt-2 max-h-72 overflow-auto text-xs">
                        <Markdown source={data.latest.notes} baseSlug="" />
                      </div>
                    </details>
                  )}
                  {data.latest.url && (
                    <a href={data.latest.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-sm text-accent hover:underline">
                      View on GitHub <ExternalLink className="h-3.5 w-3.5" />
                    </a>
                  )}
                </>
              ) : (
                <div className="space-y-2 text-sm text-muted">
                  <p>No release information yet. You can still type a version to upgrade to, and instances whose built-in updater offers a new version can be upgraded.</p>
                  {data.check_error && (
                    <p className="line-clamp-3 rounded-lg bg-warn-soft p-2 text-xs" title={data.check_error}>
                      Last check failed: {data.check_error}
                    </p>
                  )}
                </div>
              )}
            </div>
          </Card>

          <Card className="xl:col-span-2">
            <CardHeader title="Instances" description="Current version, how GuardTower upgrades each one, and what's available." />
            {!data ? (
              <Skeleton className="mx-5 mb-5 h-32" />
            ) : data.instances.length === 0 ? (
              <EmptyState icon={<ShieldCheck className="h-6 w-6" />} title="No instances yet" description="Add AdGuard Home instances first." />
            ) : (
              <Table>
                <thead>
                  <tr>
                    <Th>Instance</Th>
                    <Th>Version</Th>
                    <Th>Upgrade method</Th>
                    <Th>Status</Th>
                    <Th>
                      <span className="sr-only">Actions</span>
                    </Th>
                  </tr>
                </thead>
                <tbody>
                  {data.instances.map((s) => {
                    const g = groupName(s.group_id)
                    return (
                      <tr key={s.instance_id}>
                        <Td>
                          <Link to={`/instances/${s.instance_id}`} className="font-medium hover:underline">
                            {s.name}
                          </Link>
                          {g && (
                            <span className="ml-2 inline-flex items-center gap-1 text-xs text-muted">
                              <span className="inline-block h-2 w-2 rounded-full" style={{ background: colorFor(g.name, g.color) }} />
                              {g.name}
                            </span>
                          )}
                        </Td>
                        <Td className="font-mono text-sm">
                          {s.version || '—'}
                          {s.pinned && (
                            <Tip content={`Pinned to ${s.pinned}`}>
                              <Pin className="ml-1 inline h-3 w-3 text-muted" />
                            </Tip>
                          )}
                        </Td>
                        <Td className="text-sm">
                          {s.driver ? (
                            driverLabel[s.driver]
                          ) : (
                            <Tip content={s.blocked ?? ''}>
                              <span className="text-muted underline decoration-dotted">{driverLabel['']}</span>
                            </Tip>
                          )}
                          {s.configured === 'auto' && s.driver && <span className="ml-1 text-xs text-faint">(auto)</span>}
                        </Td>
                        <Td>
                          <StatusCell s={s} />
                        </Td>
                        <Td className="text-right">
                          {s.update_available && can('operator') && (
                            <Button size="sm" variant="ghost" onClick={() => pickInstance(s.instance_id)}>
                              Plan
                            </Button>
                          )}
                        </Td>
                      </tr>
                    )
                  })}
                </tbody>
              </Table>
            )}
          </Card>
        </div>

        <Card>
          <CardHeader title="Plan an upgrade" description="Pick which instances and which version. Up-to-date instances are left alone." />
          <div className="flex flex-wrap items-end gap-3 px-5 pb-5">
            <label className="space-y-1.5">
              <span className="text-xs font-medium text-muted">Instances</span>
              <TargetSelect value={target} onChange={(t) => setParam('target', targetParam(t))} overview={overview} />
            </label>
            <label className="space-y-1.5">
              <span className="text-xs font-medium text-muted">Version</span>
              <Input
                list="agh-releases"
                value={version}
                onChange={(e) => setVersion(e.target.value.trim())}
                placeholder={data?.latest ? `Latest (${data.latest.tag})` : 'Latest, or e.g. v0.107.79'}
                className="w-56 font-mono"
              />
              <datalist id="agh-releases">
                {data?.releases.map((r) => (
                  <option key={r.tag} value={r.tag} />
                ))}
              </datalist>
            </label>
            <Button variant="primary" onClick={() => preview.mutate()} loading={preview.isPending}>
              Preview upgrade
            </Button>
          </div>
          {plan && (
            <div className="space-y-3 border-t border-border px-5 py-5">
              <div className="flex flex-wrap items-center gap-3">
                <h3 className="flex-1 text-sm font-semibold">
                  {plan.total === 0 ? 'Nothing to upgrade' : `${plan.total} instance${plan.total === 1 ? '' : 's'} to upgrade`} · {plan.target}
                </h3>
                {can('operator') && plan.total > 0 && (
                  <Button variant="primary" disabled={observe || busy} onClick={() => setConfirm(true)} title={observe ? 'Observe mode is on' : busy ? 'A rollout is running' : undefined}>
                    <ArrowUpCircle className="h-4 w-4" /> Start upgrade
                  </Button>
                )}
              </div>
              {plan.warnings.map((w) => (
                <div key={w} className="flex items-start gap-2 rounded-lg bg-warn-soft p-3 text-sm">
                  <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warn" /> {w}
                </div>
              ))}
              {plan.items.length === 0 && <p className="text-sm text-muted">Every selected instance is already up to date.</p>}
              <ol className="divide-y divide-border rounded-xl border border-border">
                {plan.items.map((it, i) => (
                  <li key={it.instance_id} className={cn('flex flex-wrap items-center gap-2 px-4 py-2.5 text-sm', it.skip && 'opacity-60')}>
                    <span className="flex h-6 w-6 items-center justify-center rounded-full bg-subtle text-xs font-semibold tabular-nums">{i + 1}</span>
                    <span className="font-medium">{it.name}</span>
                    {it.canary && <Badge tone="info">canary</Badge>}
                    <span className="font-mono text-xs text-muted">
                      {it.from || '?'} → {it.to || '—'}
                    </span>
                    {it.driver && <Badge>{driverLabel[it.driver] ?? it.driver}</Badge>}
                    {it.skip && <span className="ml-auto text-xs text-warn">skipped: {it.skip}</span>}
                  </li>
                ))}
              </ol>
            </div>
          )}
        </Card>

        <PoliciesCard policies={data?.policies ?? []} overview={overview} editable={can('admin')} />

        <Card>
          <CardHeader title="Recent upgrades" description="Every upgrade is also listed under Review & apply." />
          {upgrades.length === 0 ? (
            <p className="px-5 pb-5 text-sm text-muted">No upgrades yet.</p>
          ) : (
            <Table>
              <thead>
                <tr>
                  <Th>#</Th>
                  <Th>Upgrade</Th>
                  <Th>Status</Th>
                  <Th>Started</Th>
                  <Th>By</Th>
                </tr>
              </thead>
              <tbody>
                {upgrades.map((r) => (
                  <tr key={r.id} className="cursor-pointer hover:bg-hover/50" onClick={() => setParam('rollout', String(r.id))}>
                    <Td className="font-mono text-xs text-faint">#{r.id}</Td>
                    <Td className="text-sm">{r.target}</Td>
                    <Td>
                      <RolloutBadge status={r.status} />
                    </Td>
                    <Td className="text-xs text-muted" title={fmtDateTime(r.created_at)}>
                      {timeAgo(r.created_at)}
                    </Td>
                    <Td className="text-sm">{r.created_by}</Td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Card>
      </div>
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={`Upgrade ${plan?.total ?? 0} instance${plan?.total === 1 ? '' : 's'}?`}
        description={
          <div className="space-y-2">
            <p>GuardTower upgrades one instance at a time, canary first, and never takes down the last online member of a group. For each instance it:</p>
            <ol className="list-decimal space-y-1 pl-5">
              <li>installs the new version with the instance's upgrade method,</li>
              <li>waits for AdGuard Home to report the new version and answer a DNS query,</li>
              <li>if that fails, puts the previous version back (Docker and SSH) and stops.</li>
            </ol>
            <p>Each instance is unavailable for a few seconds while it restarts.</p>
          </div>
        }
        confirmLabel="Start upgrade"
        loading={start.isPending}
        onConfirm={() => start.mutate()}
      />
    </>
  )
}

const dayNames = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

function PolicyRow({ p, name, color, editable }: { p: UpgradePolicy; name: string; color: string; editable: boolean }) {
  const qc = useQueryClient()
  const [draft, setDraft] = useState(p)
  const dirty = JSON.stringify(draft) !== JSON.stringify(p)
  const save = useMutation({
    mutationFn: () => api.put(`/groups/${p.group_id}/upgrade-policy`, draft),
    onSuccess: () => {
      toast.success(`Upgrade policy saved for ${name}`)
      void qc.invalidateQueries({ queryKey: ['upgrades'] })
    },
    onError: (e) => toast.error((e as Error).message),
  })
  const toggleDay = (d: number) =>
    setDraft((x) => ({ ...x, window: { ...x.window, days: x.window.days.includes(d) ? x.window.days.filter((y) => y !== d) : [...x.window.days, d].sort() } }))
  return (
    <tr>
      <Td>
        <span className="inline-flex items-center gap-2 font-medium">
          <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ background: color }} />
          {name}
        </span>
      </Td>
      <Td>
        <Select aria-label={`Upgrade policy for ${name}`} value={draft.policy} disabled={!editable} onChange={(e) => setDraft({ ...draft, policy: e.target.value as UpgradePolicy['policy'] })} className="w-48">
          <option value="manual">Manual (notify only)</option>
          <option value="auto">Automatic in window</option>
        </Select>
      </Td>
      <Td>
        <div className={cn('flex flex-wrap items-center gap-2 xl:flex-nowrap', draft.policy === 'manual' && 'opacity-50')}>
          <div className="flex gap-0.5" role="group" aria-label="Days">
            {dayNames.map((d, i) => (
              <button
                key={d}
                type="button"
                disabled={!editable}
                aria-pressed={draft.window.days.includes(i)}
                onClick={() => toggleDay(i)}
                className={cn('rounded px-1.5 py-0.5 text-xs', draft.window.days.includes(i) ? 'bg-accent-soft font-medium text-fg' : 'text-faint hover:bg-hover')}
              >
                {d}
              </button>
            ))}
          </div>
          <Input aria-label="Window start" type="time" disabled={!editable} value={draft.window.start} onChange={(e) => setDraft({ ...draft, window: { ...draft.window, start: e.target.value } })} className="w-32" />
          <span className="text-xs text-muted">to</span>
          <Input aria-label="Window end" type="time" disabled={!editable} value={draft.window.end} onChange={(e) => setDraft({ ...draft, window: { ...draft.window, end: e.target.value } })} className="w-32" />
        </div>
      </Td>
      <Td className="text-xs text-muted">{p.last_auto ? timeAgo(p.last_auto) : '—'}</Td>
      <Td className="text-right">
        {editable && (
          <Button size="sm" variant={dirty ? 'primary' : 'ghost'} disabled={!dirty} loading={save.isPending} onClick={() => save.mutate()}>
            Save
          </Button>
        )}
      </Td>
    </tr>
  )
}

function PoliciesCard({ policies, overview, editable }: { policies: UpgradePolicy[]; overview?: Overview; editable: boolean }) {
  return (
    <Card>
      <CardHeader
        title={
          <span className="inline-flex items-center gap-2">
            <CalendarClock className="h-4 w-4 text-muted" /> Automatic upgrades
          </span>
        }
        description="Per group: upgrade to the latest release by hand, or automatically inside a weekly maintenance window (server time). Observe mode pauses automatic upgrades."
      />
      {policies.length === 0 ? (
        <p className="px-5 pb-5 text-sm text-muted">Create groups to set upgrade policies. Ungrouped instances are always upgraded by hand.</p>
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>Group</Th>
              <Th>Policy</Th>
              <Th>Maintenance window</Th>
              <Th>Last automatic run</Th>
              <Th>
                <span className="sr-only">Actions</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {policies.map((p) => {
              const g = overview?.groups.find((x) => x.id === p.group_id)
              return <PolicyRow key={`${p.group_id}:${JSON.stringify(p)}`} p={p} name={g?.name ?? `Group ${p.group_id}`} color={colorFor(g?.name ?? '', g?.color)} editable={editable} />
            })}
          </tbody>
        </Table>
      )}
    </Card>
  )
}
