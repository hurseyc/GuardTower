import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { Activity, Boxes, Plus, Server, ShieldCheck, ShieldOff } from 'lucide-react'
import { api, type Overview } from '@/lib/api'
import { useSession } from '@/lib/session'
import { colorFor, timeAgo } from '@/lib/utils'
import { PageHeader } from '@/components/AppShell'
import { Badge, Button, Card, CardHeader, EmptyState, Skeleton, StatusDot, Tip } from '@/components/ui'

export function StatCard({ label, value, sub, icon, tone = 'neutral' }: { label: string; value: React.ReactNode; sub?: React.ReactNode; icon?: React.ReactNode; tone?: 'neutral' | 'ok' | 'warn' | 'danger' }) {
  const toneCls = { neutral: 'text-fg', ok: 'text-ok', warn: 'text-warn', danger: 'text-danger' }[tone]
  return (
    <Card className="p-4">
      <div className="flex items-center justify-between text-xs text-muted">
        {label}
        {icon && <span className="text-faint">{icon}</span>}
      </div>
      <div className={`mt-2 text-2xl font-semibold tabular-nums tracking-tight ${toneCls}`}>{value}</div>
      {sub && <div className="mt-1 text-xs text-faint">{sub}</div>}
    </Card>
  )
}

export function OverviewPage() {
  const { can } = useSession()
  const { data, isLoading } = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview'), refetchInterval: 30_000 })

  if (isLoading || !data) {
    return (
      <div className="space-y-4 p-6">
        <Skeleton className="h-8 w-48" />
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-24" />
          ))}
        </div>
      </div>
    )
  }

  const groupById = new Map(data.groups.map((g) => [g.id, g]))
  const hostById = new Map(data.hosts.map((h) => [h.id, h]))
  const offline = data.summary.instances - data.summary.online
  const unprotected = data.instances.filter((i) => i.status?.online && !i.status.protection_enabled)
  const versions = new Set(data.instances.map((i) => i.status?.version).filter(Boolean))

  if (data.instances.length === 0) {
    return (
      <>
        <PageHeader title="Overview" description="Your AdGuard Home fleet at a glance." />
        <div className="p-6">
          <Card>
            <EmptyState
              icon={<ShieldCheck className="h-6 w-6" />}
              title="Add your first AdGuard Home instance"
              description="GuardTower connects to each instance's web API. Start by adding one, then organise instances into groups that share settings."
              action={
                can('admin') ? (
                  <Link to="/instances?add=1">
                    <Button variant="primary">
                      <Plus className="h-4 w-4" /> Add instance
                    </Button>
                  </Link>
                ) : undefined
              }
            />
          </Card>
        </div>
      </>
    )
  }

  // Group instances for display: grouped first, then ungrouped.
  const sections: { key: string; title: string; color: string; ids: number[] }[] = data.groups
    .filter((g) => g.instance_ids.length > 0)
    .map((g) => ({ key: `g${g.id}`, title: g.name, color: colorFor(g.name, g.color), ids: g.instance_ids }))
  const ungrouped = data.instances.filter((i) => !i.group_id).map((i) => i.id)
  if (ungrouped.length) sections.push({ key: 'none', title: 'Ungrouped', color: 'var(--fg-subtle)', ids: ungrouped })
  const instById = new Map(data.instances.map((i) => [i.id, i]))

  return (
    <>
      <PageHeader title="Overview" description="Live health of every AdGuard Home instance GuardTower manages." />
      <div className="space-y-6 p-6">
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <StatCard
            label="Instances online"
            value={`${data.summary.online}/${data.summary.instances}`}
            sub={offline > 0 ? `${offline} offline` : 'All reachable'}
            tone={offline > 0 ? 'danger' : 'ok'}
            icon={<Activity className="h-4 w-4" />}
          />
          <StatCard
            label="Protection"
            value={unprotected.length === 0 ? 'On' : `${unprotected.length} off`}
            sub={unprotected.length ? unprotected.map((i) => i.name).join(', ') : 'Filtering enabled everywhere'}
            tone={unprotected.length ? 'warn' : 'ok'}
            icon={unprotected.length ? <ShieldOff className="h-4 w-4" /> : <ShieldCheck className="h-4 w-4" />}
          />
          <StatCard label="Groups" value={data.summary.groups} sub={`${ungrouped.length} ungrouped instance${ungrouped.length === 1 ? '' : 's'}`} icon={<Boxes className="h-4 w-4" />} />
          <StatCard
            label="AdGuard Home versions"
            value={versions.size || '—'}
            sub={versions.size > 1 ? 'Mixed versions in the fleet' : [...versions][0] ?? 'Unknown'}
            tone={versions.size > 1 ? 'warn' : 'neutral'}
            icon={<Server className="h-4 w-4" />}
          />
        </div>

        {sections.map((sec) => (
          <Card key={sec.key}>
            <CardHeader
              title={
                <span className="flex items-center gap-2">
                  <span className="h-2.5 w-2.5 rounded-full" style={{ background: sec.color }} />
                  {sec.title}
                  <span className="font-normal text-faint">· {sec.ids.length}</span>
                </span>
              }
            />
            <div className="grid gap-3 px-5 pb-5 sm:grid-cols-2 xl:grid-cols-3">
              {sec.ids.map((id) => {
                const i = instById.get(id)
                if (!i) return null
                const host = i.host_id ? hostById.get(i.host_id) : undefined
                return (
                  <Link
                    key={i.id}
                    to={`/instances/${i.id}`}
                    className="group rounded-xl border border-border bg-bg p-4 transition-all hover:-translate-y-0.5 hover:border-border-strong hover:shadow-card"
                  >
                    <div className="flex items-center gap-2">
                      <StatusDot online={i.status?.online} />
                      <span className="truncate font-medium">{i.name}</span>
                      {i.is_canary && <Badge tone="info">canary</Badge>}
                      <span className="ml-auto font-mono text-xs text-faint">{i.status?.version || '—'}</span>
                    </div>
                    <div className="mt-1 truncate font-mono text-xs text-faint">{i.url}</div>
                    <div className="mt-3 flex flex-wrap items-center gap-1.5">
                      {i.status?.online ? (
                        i.status.protection_enabled ? (
                          <Badge tone="ok">protected</Badge>
                        ) : (
                          <Badge tone="warn">protection off</Badge>
                        )
                      ) : (
                        <Tip content={i.status?.last_error}>
                          <Badge tone="danger">offline · {timeAgo(i.status?.last_seen_at)}</Badge>
                        </Tip>
                      )}
                      {host && <Badge>{host.name}</Badge>}
                      {i.tags.map((t) => (
                        <Badge key={t}>#{t}</Badge>
                      ))}
                      {i.status?.online && <span className="ml-auto text-xs tabular-nums text-faint">{i.status.latency_ms} ms</span>}
                    </div>
                  </Link>
                )
              })}
            </div>
          </Card>
        ))}
        {groupById.size === 0 && (
          <p className="text-sm text-muted">
            Tip: create <Link to="/groups" className="text-accent hover:underline">groups</Link> to give sets of instances different settings.
          </p>
        )}
      </div>
    </>
  )
}
