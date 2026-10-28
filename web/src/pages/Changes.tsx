import { useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { AlertTriangle, CheckCircle2, CircleDashed, Eye, Loader2, Rocket, RotateCcw, ShieldCheck, SkipForward, XCircle } from 'lucide-react'
import { api, type ConfigSection, type Overview, type Plan, type Rollout, type RolloutStep, type RolloutTarget } from '@/lib/api'
import { describe } from '@/lib/configMerge'
import { onServerEvent } from '@/lib/events'
import { useSession } from '@/lib/session'
import { cn, fmtDateTime, timeAgo } from '@/lib/utils'
import { PageHeader } from '@/components/AppShell'
import { Badge, Button, Card, CardHeader, ConfirmDialog, EmptyState, Select, Skeleton, Table, Td, Th } from '@/components/ui'

const rolloutTone = { running: 'info', succeeded: 'ok', failed: 'danger', rolled_back: 'warn', cancelled: 'neutral' } as const
const rolloutLabel = { running: 'Running', succeeded: 'Succeeded', failed: 'Failed', rolled_back: 'Rolled back', cancelled: 'Cancelled' }

export function RolloutBadge({ status }: { status: Rollout['status'] }) {
  return <Badge tone={rolloutTone[status]}>{rolloutLabel[status]}</Badge>
}

function parseTarget(v: string | null): RolloutTarget {
  if (!v || v === 'fleet') return { kind: 'fleet' }
  const [k, rest] = v.split(':')
  if (k === 'tag') return { kind: 'tag', tag: rest }
  return { kind: k as RolloutTarget['kind'], id: Number(rest) }
}
function targetParam(t: RolloutTarget): string {
  if (t.kind === 'fleet') return 'fleet'
  if (t.kind === 'tag') return `tag:${t.tag}`
  return `${t.kind}:${t.id}`
}

/** Live-refresh rollout queries on server events. */
function useRolloutEvents() {
  const qc = useQueryClient()
  useEffect(
    () =>
      onServerEvent((type) => {
        if (type !== 'rollout.update') return
        void qc.invalidateQueries({ queryKey: ['rollouts'] })
        void qc.invalidateQueries({ queryKey: ['rollout'] })
        void qc.invalidateQueries({ queryKey: ['sync'] })
        void qc.invalidateQueries({ queryKey: ['instance-config'] })
      }),
    [qc],
  )
}

export function ChangesPage() {
  const { can } = useSession()
  const qc = useQueryClient()
  const [params, setParams] = useSearchParams()
  const target = parseTarget(params.get('target'))
  const rolloutId = Number(params.get('rollout') ?? 0)
  const [section, setSection] = useState(params.get('section') ?? '')
  const [plan, setPlan] = useState<Plan | null>(null)
  const [confirm, setConfirm] = useState(false)
  const [observeConfirm, setObserveConfirm] = useState(false)
  useRolloutEvents()

  const { data: overview } = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview') })
  const { data: schema } = useQuery({ queryKey: ['config-schema'], queryFn: () => api.get<ConfigSection[]>('/config/schema'), staleTime: Infinity })
  const { data: list } = useQuery({
    queryKey: ['rollouts'],
    queryFn: () => api.get<{ rollouts: Rollout[]; active_id: number; observe: boolean }>('/rollouts'),
    refetchInterval: 15_000,
  })
  const tags = useMemo(() => [...new Set((overview?.instances ?? []).flatMap((i) => i.tags))].sort(), [overview])

  const setParam = (k: string, v: string) => {
    const n = new URLSearchParams(params)
    if (v) n.set(k, v)
    else n.delete(k)
    setParams(n, { replace: true })
  }

  const preview = useMutation({
    mutationFn: () => api.post<Plan>('/rollouts/plan', { target, sections: section ? [section] : [] }),
    onSuccess: setPlan,
    onError: (e) => toast.error((e as Error).message),
  })
  const apply = useMutation({
    mutationFn: () => api.post<{ id: number }>('/rollouts', { target, sections: section ? [section] : [] }),
    onSuccess: (r) => {
      setConfirm(false)
      setPlan(null)
      setParam('rollout', String(r.id))
      void qc.invalidateQueries({ queryKey: ['rollouts'] })
      toast.success('Rollout started', { description: 'Canary first, then one instance at a time.' })
    },
    onError: (e) => {
      setConfirm(false)
      toast.error((e as Error).message)
    },
  })
  const disableObserve = useMutation({
    mutationFn: () => api.put('/settings', { 'mode.observe': 'false' }),
    onSuccess: () => {
      setObserveConfirm(false)
      void qc.invalidateQueries({ queryKey: ['rollouts'] })
      if (plan) preview.mutate()
      toast.success('Observe mode is off', { description: 'GuardTower may now apply changes you approve.' })
    },
    onError: (e) => toast.error((e as Error).message),
  })

  // Re-plan automatically when the target changes.
  useEffect(() => setPlan(null), [params, section])

  const observe = list?.observe ?? true
  const busy = !!list?.active_id
  const affected = plan?.items.filter((i) => i.changes.length > 0) ?? []

  return (
    <>
      <PageHeader title="Review & apply" description="Preview exactly what will change, then roll it out safely: canary first, one instance at a time, with automatic rollback if DNS breaks.">
        {observe && (
          <div className="mt-4 flex flex-wrap items-center gap-3 rounded-xl border border-border bg-subtle p-3 text-sm">
            <Eye className="h-4 w-4 text-info" />
            <span className="flex-1">
              <strong>Observe mode is on.</strong> GuardTower reads your instances but won't change them. You can still preview changes.
            </span>
            {can('admin') && (
              <Button size="sm" variant="outline" onClick={() => setObserveConfirm(true)}>
                Allow applying changes
              </Button>
            )}
          </div>
        )}
      </PageHeader>
      <div className="space-y-6 p-6">
        {rolloutId > 0 && <RolloutDetail id={rolloutId} onClose={() => setParam('rollout', '')} />}

        <Card>
          <CardHeader title="1. Choose what to update" description="Only managed settings that differ are touched." />
          <div className="flex flex-wrap items-end gap-3 px-5 pb-5">
            <label className="space-y-1.5">
              <span className="text-xs font-medium text-muted">Instances</span>
              <Select value={targetParam(target)} onChange={(e) => setParam('target', e.target.value)} className="w-64">
                <option value="fleet">Whole fleet</option>
                {overview?.groups.length ? (
                  <optgroup label="Groups">
                    {overview.groups.map((g) => (
                      <option key={g.id} value={`group:${g.id}`}>
                        {g.name}
                      </option>
                    ))}
                  </optgroup>
                ) : null}
                {tags.length ? (
                  <optgroup label="Tags">
                    {tags.map((t) => (
                      <option key={t} value={`tag:${t}`}>
                        #{t}
                      </option>
                    ))}
                  </optgroup>
                ) : null}
                <optgroup label="Single instance">
                  {overview?.instances.map((i) => (
                    <option key={i.id} value={`instance:${i.id}`}>
                      {i.name}
                    </option>
                  ))}
                </optgroup>
              </Select>
            </label>
            <label className="space-y-1.5">
              <span className="text-xs font-medium text-muted">Settings</span>
              <Select value={section} onChange={(e) => setSection(e.target.value)} className="w-64">
                <option value="">All managed sections</option>
                {schema?.map((s) => (
                  <option key={s.key} value={s.key}>
                    {s.label}
                  </option>
                ))}
              </Select>
            </label>
            <Button variant="primary" onClick={() => preview.mutate()} loading={preview.isPending}>
              Preview changes
            </Button>
          </div>
        </Card>

        {plan && (
          <Card>
            <CardHeader
              title={`2. Review: ${plan.total} change${plan.total === 1 ? '' : 's'} on ${affected.length} instance${affected.length === 1 ? '' : 's'}`}
              description={`${plan.target}. Instances are updated in this order.`}
              actions={
                can('operator') &&
                plan.total > 0 && (
                  <Button variant="primary" disabled={observe || busy} onClick={() => setConfirm(true)} title={observe ? 'Observe mode is on' : busy ? 'A rollout is running' : undefined}>
                    <Rocket className="h-4 w-4" /> Apply {plan.total} change{plan.total === 1 ? '' : 's'}
                  </Button>
                )
              }
            />
            <div className="space-y-3 px-5 pb-5">
              {plan.warnings.map((w) => (
                <div key={w} className="flex items-start gap-2 rounded-lg bg-warn-soft p-3 text-sm">
                  <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warn" /> {w}
                </div>
              ))}
              {plan.total === 0 && (
                <EmptyState icon={<ShieldCheck className="h-6 w-6" />} title="Everything is already in sync" description="These instances already match their desired settings. Edit settings under Configuration first." />
              )}
              {plan.items.map((it, idx) => (
                <div key={it.instance_id} className={cn('rounded-xl border border-border', it.changes.length === 0 && 'opacity-60')}>
                  <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2.5">
                    <span className="flex h-6 w-6 items-center justify-center rounded-full bg-subtle text-xs font-semibold tabular-nums">{idx + 1}</span>
                    <Link to={`/instances/${it.instance_id}`} className="font-medium hover:underline">
                      {it.name}
                    </Link>
                    {it.canary && <Badge tone="info">canary</Badge>}
                    {!it.online && <Badge tone="danger">offline: will be skipped</Badge>}
                    <span className="ml-auto text-xs text-muted">{it.changes.length ? `${it.changes.length} change${it.changes.length === 1 ? '' : 's'}` : 'in sync'}</span>
                  </div>
                  {(it.changes.length > 0 || it.error) && (
                    <ul className="space-y-1 px-4 py-3 text-xs">
                      {it.error && <li className="text-danger">{it.error}</li>}
                      {it.changes.map((c, i) => (
                        <li key={i} className="flex flex-wrap items-center gap-2">
                          <Badge tone={c.op === 'remove' ? 'danger' : c.op === 'add' ? 'ok' : 'info'}>{c.op}</Badge>
                          <span className="text-muted">
                            {schema?.find((s) => s.key === c.section)?.label} · {c.label}:
                          </span>
                          <span className="break-all font-mono">{describe(c, schema?.find((s) => s.key === c.section))}</span>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              ))}
            </div>
          </Card>
        )}

        <Card>
          <CardHeader title="Rollout history" description="Every apply and restore, with what happened on each instance." />
          {!list ? (
            <Skeleton className="mx-5 mb-5 h-24" />
          ) : list.rollouts.length === 0 ? (
            <EmptyState icon={<Rocket className="h-6 w-6" />} title="No rollouts yet" description="When you apply changes, each rollout is recorded here." />
          ) : (
            <Table>
              <thead>
                <tr>
                  <Th>#</Th>
                  <Th>Target</Th>
                  <Th>Status</Th>
                  <Th>Started</Th>
                  <Th>By</Th>
                  <Th>Result</Th>
                </tr>
              </thead>
              <tbody>
                {list.rollouts.map((r) => (
                  <tr key={r.id} className="cursor-pointer hover:bg-hover/50" onClick={() => setParam('rollout', String(r.id))}>
                    <Td className="font-mono text-xs text-faint">#{r.id}</Td>
                    <Td className="text-sm">
                      {r.kind === 'restore' && <RotateCcw className="mr-1 inline h-3.5 w-3.5 text-muted" />}
                      {r.target}
                    </Td>
                    <Td>
                      <RolloutBadge status={r.status} />
                    </Td>
                    <Td className="text-xs text-muted" title={fmtDateTime(r.created_at)}>
                      {timeAgo(r.created_at)}
                    </Td>
                    <Td className="text-sm">{r.created_by}</Td>
                    <Td className="max-w-md truncate text-xs text-muted" title={r.error}>
                      {r.error || (r.status === 'succeeded' ? 'All instances updated and verified' : '')}
                    </Td>
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
        title={`Apply ${plan?.total ?? 0} changes?`}
        description={
          <div className="space-y-2">
            <p>GuardTower will update {affected.length} instance{affected.length === 1 ? '' : 's'} one at a time, starting with the canary. For each instance it will:</p>
            <ol className="list-decimal space-y-1 pl-5">
              <li>take a snapshot of its current settings,</li>
              <li>apply the changes,</li>
              <li>check that the instance still answers DNS and that the settings took effect,</li>
              <li>if anything fails, restore the snapshot automatically and stop.</li>
            </ol>
          </div>
        }
        confirmLabel="Start rollout"
        loading={apply.isPending}
        onConfirm={() => apply.mutate()}
      />
      <ConfirmDialog
        open={observeConfirm}
        onOpenChange={setObserveConfirm}
        title="Allow GuardTower to change instances?"
        description="Turning off observe mode lets admins and operators apply changes to AdGuard Home. Every change still needs an explicit review and apply, and is snapshotted first. You can turn observe mode back on under Admin → Settings."
        confirmLabel="Turn off observe mode"
        loading={disableObserve.isPending}
        onConfirm={() => disableObserve.mutate()}
      />
    </>
  )
}

const stepIcon: Record<RolloutStep['status'], React.ReactNode> = {
  pending: <CircleDashed className="h-5 w-5 text-faint" />,
  applying: <Loader2 className="h-5 w-5 animate-spin text-info" />,
  verifying: <Loader2 className="h-5 w-5 animate-spin text-info" />,
  succeeded: <CheckCircle2 className="h-5 w-5 text-ok" />,
  failed: <XCircle className="h-5 w-5 text-danger" />,
  rolled_back: <RotateCcw className="h-5 w-5 text-warn" />,
  skipped: <SkipForward className="h-5 w-5 text-faint" />,
}
const stepLabel: Record<RolloutStep['status'], string> = {
  pending: 'Waiting',
  applying: 'Applying',
  verifying: 'Verifying',
  succeeded: 'Done',
  failed: 'Failed',
  rolled_back: 'Rolled back',
  skipped: 'Skipped',
}

export function RolloutDetail({ id, onClose }: { id: number; onClose?: () => void }) {
  useRolloutEvents()
  const { data: r } = useQuery({
    queryKey: ['rollout', id],
    queryFn: () => api.get<Rollout>(`/rollouts/${id}`),
    refetchInterval: (q) => (q.state.data?.status === 'running' ? 2000 : false),
  })
  if (!r) return <Skeleton className="h-40" />
  return (
    <Card className={cn(r.status === 'running' && 'ring-2 ring-ring')}>
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            Rollout #{r.id} <RolloutBadge status={r.status} />
          </span>
        }
        description={`${r.target} · started ${timeAgo(r.created_at)} by ${r.created_by}${r.sections?.length ? ` · ${r.sections.join(', ')}` : ''}`}
        actions={
          onClose && (
            <Button size="sm" variant="ghost" onClick={onClose}>
              Close
            </Button>
          )
        }
      />
      {r.error && <div className="mx-5 mb-3 rounded-lg bg-warn-soft p-3 text-sm">{r.error}</div>}
      <ol className="px-5 pb-5">
        {r.steps?.map((s, i) => (
          <li key={s.id} className="relative flex gap-3 pb-4 last:pb-0">
            {i < (r.steps?.length ?? 0) - 1 && <span className="absolute left-[9px] top-6 h-[calc(100%-1rem)] w-px bg-border" />}
            <span className="relative z-10 bg-elevated">{stepIcon[s.status]}</span>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2 text-sm">
                <Link to={`/instances/${s.instance_id}`} className="font-medium hover:underline">
                  {s.instance_name}
                </Link>
                {s.canary && <Badge tone="info">canary</Badge>}
                <span className="text-xs text-muted">{stepLabel[s.status]}</span>
                {s.changes.length > 0 && <span className="text-xs text-faint">· {s.changes.length} change{s.changes.length === 1 ? '' : 's'}</span>}
                {s.snapshot_id ? <span className="text-xs text-faint">· snapshot #{s.snapshot_id}</span> : null}
              </div>
              {s.detail && <p className={cn('mt-0.5 text-xs', s.status === 'failed' || s.status === 'rolled_back' ? 'text-warn' : 'text-muted')}>{s.detail}</p>}
            </div>
          </li>
        ))}
      </ol>
    </Card>
  )
}
