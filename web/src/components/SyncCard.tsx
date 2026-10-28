import { useState } from 'react'
import { Link } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, CheckCircle2, ChevronDown, ChevronRight, RefreshCw } from 'lucide-react'
import { api, type Change, type ConfigSection, type SectionState } from '@/lib/api'
import { fmtValue } from '@/lib/configMerge'
import { cn } from '@/lib/utils'
import { Badge, Button, Card, CardHeader, Skeleton } from './ui'

const scopeTone = { base: 'neutral', group: 'accent', instance: 'warn' } as const

function describe(c: Change, sec?: ConfigSection): string {
  const f = sec?.fields.find((x) => x.name === c.field)
  switch (c.op) {
    case 'set':
      return f ? `${fmtValue(f, c.from)} → ${fmtValue(f, c.to)}` : 'change value'
    case 'add':
      return `add ${c.item}`
    case 'remove':
      return `remove ${c.item}`
    case 'update':
      return `update ${c.item}`
  }
}

/** Desired-vs-live state of every section on one instance, with where each value comes from. */
export function SyncCard({ instanceId }: { instanceId: number }) {
  const { data: schema } = useQuery({ queryKey: ['config-schema'], queryFn: () => api.get<ConfigSection[]>('/config/schema'), staleTime: Infinity })
  const { data, isFetching, refetch } = useQuery({
    queryKey: ['instance-config', instanceId],
    queryFn: () => api.get<SectionState[]>(`/instances/${instanceId}/config`),
    refetchInterval: 60_000,
  })
  const [open, setOpen] = useState<string | null>(null)
  const managed = data?.filter((s) => s.managed) ?? []
  const total = managed.reduce((n, s) => n + s.changes.length, 0)
  const errors = managed.filter((s) => s.live_error)

  return (
    <Card className="xl:col-span-3">
      <CardHeader
        title="Settings sync"
        description="How this instance compares with the settings GuardTower manages for it (Base, its group and overrides)."
        actions={
          <Button size="sm" variant="ghost" onClick={() => void refetch()} aria-label="Re-check">
            <RefreshCw className={cn('h-3.5 w-3.5', isFetching && 'animate-spin')} /> Re-check
          </Button>
        }
      />
      <div className="px-5 pb-5">
        {!data ? (
          <Skeleton className="h-20" />
        ) : managed.length === 0 ? (
          <p className="text-sm text-muted">
            No settings are managed yet. Go to <Link to="/config" className="text-accent hover:underline">Configuration</Link> and import a baseline to start.
          </p>
        ) : (
          <>
            <div className={cn('mb-3 flex items-center gap-2 rounded-lg p-3 text-sm', total === 0 && errors.length === 0 ? 'bg-accent-soft' : 'bg-warn-soft')}>
              {total === 0 && errors.length === 0 ? <CheckCircle2 className="h-4 w-4 text-ok" /> : <AlertTriangle className="h-4 w-4 text-warn" />}
              {total === 0 && errors.length === 0
                ? 'In sync: this instance matches its desired settings.'
                : `${total} difference${total === 1 ? '' : 's'} from the desired settings${errors.length ? `; ${errors.length} section(s) couldn't be read` : ''}. Apply changes to bring it in line.`}
            </div>
            <ul className="divide-y divide-border rounded-lg border border-border">
              {managed.map((s) => {
                const sec = schema?.find((x) => x.key === s.section)
                const expanded = open === s.section
                return (
                  <li key={s.section}>
                    <button onClick={() => setOpen(expanded ? null : s.section)} className="flex w-full items-center gap-2 px-3 py-2.5 text-left text-sm hover:bg-hover/50">
                      {expanded ? <ChevronDown className="h-4 w-4 text-faint" /> : <ChevronRight className="h-4 w-4 text-faint" />}
                      <span className="flex-1">{sec?.label ?? s.section}</span>
                      {s.unsupported?.length ? <Badge title={`Not available in this AdGuard Home version: ${s.unsupported.join(', ')}`}>{s.unsupported.length} unsupported</Badge> : null}
                      {s.live_error ? <Badge tone="danger">unreadable</Badge> : s.changes.length ? <Badge tone="warn">{s.changes.length} to change</Badge> : <Badge tone="ok">in sync</Badge>}
                    </button>
                    {expanded && (
                      <div className="space-y-3 bg-subtle/50 px-10 py-3 text-xs">
                        {s.live_error && <p className="text-danger">{s.live_error}</p>}
                        {s.changes.length > 0 && (
                          <div>
                            <div className="mb-1 font-medium text-muted">Pending changes</div>
                            <ul className="space-y-1">
                              {s.changes.map((c, i) => (
                                <li key={i} className="flex gap-2">
                                  <Badge tone={c.op === 'remove' ? 'danger' : c.op === 'add' ? 'ok' : 'info'}>{c.op}</Badge>
                                  <span className="text-muted">{c.label}:</span>
                                  <span className="break-all font-mono">{describe(c, sec)}</span>
                                </li>
                              ))}
                            </ul>
                          </div>
                        )}
                        <div>
                          <div className="mb-1 font-medium text-muted">Desired values and where they come from</div>
                          <ul className="space-y-1">
                            {sec?.fields
                              .filter((f) => s.desired && f.name in s.desired)
                              .map((f) => {
                                const o = s.origins?.[f.name]
                                return (
                                  <li key={f.name} className="flex flex-wrap items-center gap-2">
                                    <span className="text-muted">{f.label}</span>
                                    <span className="font-mono">{fmtValue(f, s.desired?.[f.name])}</span>
                                    {o && (
                                      <Badge tone={scopeTone[o.scope as keyof typeof scopeTone] ?? 'neutral'}>
                                        {o.scope === 'base' ? 'Base' : o.name}
                                      </Badge>
                                    )}
                                  </li>
                                )
                              })}
                          </ul>
                        </div>
                      </div>
                    )}
                  </li>
                )
              })}
            </ul>
          </>
        )}
      </div>
    </Card>
  )
}
