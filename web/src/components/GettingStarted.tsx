import { useState } from 'react'
import { Link } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { CheckCircle2, Circle, X } from 'lucide-react'
import { api, type Overview, type Rollout, type SyncSummary } from '@/lib/api'
import { Card, CardHeader } from './ui'

const dismissKey = 'gt.getting-started.dismissed'

function readDismissed() {
  try {
    return localStorage.getItem(dismissKey) === '1'
  } catch {
    return false
  }
}

/** Admin checklist on Overview until the fleet is set up; each step links to where it's done. */
export function GettingStarted({ overview }: { overview: Overview }) {
  const [dismissed, setDismissed] = useState(readDismissed)
  const { data: sync } = useQuery({ queryKey: ['sync'], queryFn: () => api.get<SyncSummary[]>('/sync'), refetchInterval: 60_000, enabled: !dismissed })
  const { data: list } = useQuery({
    queryKey: ['rollouts'],
    queryFn: () => api.get<{ rollouts: Rollout[]; active_id: number; observe: boolean }>('/rollouts'),
    enabled: !dismissed,
  })
  if (dismissed || !sync || !list) return null

  const ins = overview.instances
  const steps = [
    { done: ins.length > 0, title: 'Add your AdGuard Home instances', hint: 'Connect each server by its web address.', to: '/instances?add=1', doc: 'setup/adding-instances' },
    { done: ins.length > 0 && ins.every((i) => i.group_id), title: 'Put instances in groups', hint: 'Instances in a group share settings, for example "Main network" and "Kids VLAN".', to: '/groups', doc: 'operation/groups' },
    { done: ins.length > 0 && ins.every((i) => i.host_id), title: 'Say where each one runs', hint: 'Hosts (or a Proxmox connection) let GuardTower spot single points of failure.', to: '/hosts', doc: 'operation/hosts' },
    { done: sync.some((s) => Object.keys(s.sections).length > 0), title: 'Manage settings centrally', hint: 'Import a baseline from your best-configured instance into Base.', to: '/config', doc: 'operation/configuration' },
    { done: !list.observe && list.rollouts.some((r) => r.kind === 'apply' && r.status === 'succeeded'), title: 'Apply your first change', hint: list.observe ? 'Turn observe mode off when you are ready, then roll out canary first.' : 'Review the pending changes and roll them out, canary first.', to: '/changes', doc: 'operation/changes' },
  ]
  const left = steps.filter((s) => !s.done).length
  if (left === 0) return null
  const dismiss = () => {
    try {
      localStorage.setItem(dismissKey, '1')
    } catch {
      /* hide for this visit only */
    }
    setDismissed(true)
  }
  return (
    <Card>
      <CardHeader
        title="Getting started"
        description={`${steps.length - left} of ${steps.length} done. GuardTower only watches until you choose to apply changes.`}
        actions={
          <button type="button" onClick={dismiss} className="rounded p-1 text-muted hover:bg-hover" aria-label="Hide the getting-started checklist">
            <X className="h-4 w-4" />
          </button>
        }
      />
      <ol className="grid grid-cols-1 items-start gap-2 px-5 pb-5 md:grid-cols-5">
        {steps.map((s, i) => (
          <li key={s.title} className="rounded-lg border border-border p-3">
            <div className="flex items-center gap-2 text-sm font-medium">
              {s.done ? <CheckCircle2 className="h-4 w-4 shrink-0 text-ok" aria-hidden /> : <Circle className="h-4 w-4 shrink-0 text-faint" aria-hidden />}
              <span className="sr-only">{s.done ? 'Done:' : 'To do:'}</span>
              <span className={s.done ? 'text-muted line-through' : ''}>
                {i + 1}. {s.title}
              </span>
            </div>
            {!s.done && (
              <>
                <p className="mt-1 text-xs text-muted">{s.hint}</p>
                <p className="mt-2 flex gap-3 text-xs">
                  <Link to={s.to} className="font-medium text-accent">
                    Go
                  </Link>
                  <Link to={`/docs/${s.doc}`} className="text-muted">
                    How
                  </Link>
                </p>
              </>
            )}
          </li>
        ))}
      </ol>
    </Card>
  )
}
