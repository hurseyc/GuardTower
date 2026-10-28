import { useMemo } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { Activity, Ban, Clock, Database, Users } from 'lucide-react'
import { api, type Overview, type StatsSummary } from '@/lib/api'
import { fmtCompact, fmtNum, fmtPct } from '@/lib/utils'
import { statusMeta } from '@/lib/status'
import { PageHeader } from '@/components/AppShell'
import { Card, CardHeader, EmptyState, Select, Skeleton, Tabs, TabsList, TabsTrigger } from '@/components/ui'
import { BarList, HourHeatmap, TimeSeriesChart, useChartTheme, seriesColor } from '@/components/charts'
import { StatCard } from './Overview'

export const ranges = [
  { value: '1h', label: '1 hour' },
  { value: '6h', label: '6 hours' },
  { value: '24h', label: '24 hours' },
  { value: '7d', label: '7 days' },
  { value: '30d', label: '30 days' },
]

/** Shared filter state kept in the URL so views are linkable. */
export function useFleetFilter(defaultRange = '24h') {
  const [params, setParams] = useSearchParams()
  const range = params.get('range') ?? defaultRange
  const group = params.get('group') ?? ''
  const instance = params.get('instance') ?? ''
  const set = (k: string, v: string) => {
    const n = new URLSearchParams(params)
    if (v) n.set(k, v)
    else n.delete(k)
    setParams(n, { replace: true })
  }
  const qs = new URLSearchParams()
  if (group) qs.set('group', group)
  if (instance) qs.set('instance', instance)
  return { range, group, instance, set, scopeQuery: qs.toString() }
}

export function FleetFilterBar({ overview, range, group, instance, set, showRange = true }: { overview?: Overview; range?: string; group: string; instance: string; set: (k: string, v: string) => void; showRange?: boolean }) {
  return (
    <div className="mt-4 flex flex-wrap items-center gap-2">
      {showRange && range && (
        <Tabs value={range} onValueChange={(v) => set('range', v)}>
          <TabsList>
            {ranges.map((r) => (
              <TabsTrigger key={r.value} value={r.value}>
                {r.label}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      )}
      <Select value={group} onChange={(e) => set('group', e.target.value)} className="w-44" aria-label="Group">
        <option value="">All groups</option>
        {overview?.groups.map((g) => (
          <option key={g.id} value={g.id}>
            {g.name}
          </option>
        ))}
      </Select>
      <Select value={instance} onChange={(e) => set('instance', e.target.value)} className="w-44" aria-label="Instance">
        <option value="">All instances</option>
        {overview?.instances
          .filter((i) => !group || String(i.group_id) === group)
          .map((i) => (
            <option key={i.id} value={i.id}>
              {i.name}
            </option>
          ))}
      </Select>
    </div>
  )
}

export function AnalyticsPage() {
  const nav = useNavigate()
  const { range, group, instance, set, scopeQuery } = useFleetFilter()
  const theme = useChartTheme()
  const { data: overview } = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview') })
  const { data, isLoading } = useQuery({
    queryKey: ['stats', range, group, instance],
    queryFn: () => api.get<StatsSummary>(`/stats/summary?range=${range}&top=10${scopeQuery ? '&' + scopeQuery : ''}`),
    refetchInterval: 30_000,
    placeholderData: (prev) => prev,
  })

  // Stable color per instance: its position in the full instance list (by id), never its rank.
  const instColor = useMemo(() => {
    const ids = (overview?.instances ?? []).map((i) => i.id).sort((a, b) => a - b)
    return (id: number) => seriesColor(theme, Math.max(0, ids.indexOf(id)))
  }, [overview, theme])
  const instName = (id: number) => overview?.instances.find((i) => i.id === id)?.name ?? `#${id}`

  const toLog = (k: string, v: string) => {
    const qs = new URLSearchParams(scopeQuery)
    qs.set(k, v)
    nav(`/logs?${qs.toString()}`)
  }

  const series = useMemo(
    () =>
      data
        ? [
            { name: 'All queries', colorIndex: 0, area: true, data: data.series.map((p) => [p.t, p.queries] as [number, number]) },
            { name: 'Blocked', colorIndex: 1, data: data.series.map((p) => [p.t, p.blocked] as [number, number]) },
          ]
        : [],
    [data],
  )

  const t = data?.totals
  const empty = data && t?.queries === 0
  return (
    <>
      <PageHeader title="Analytics" description="Traffic and blocking across the fleet, from GuardTower's stored query history.">
        <FleetFilterBar overview={overview} range={range} group={group} instance={instance} set={set} />
      </PageHeader>
      <div className="space-y-6 p-6">
        {isLoading || !data ? (
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
            {[0, 1, 2, 3, 4].map((i) => (
              <Skeleton key={i} className="h-24" />
            ))}
          </div>
        ) : (
          <>
            <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
              <StatCard label="DNS queries" value={fmtCompact(t!.queries)} sub={fmtNum(t!.queries)} icon={<Activity className="h-4 w-4" />} />
              <StatCard label="Blocked" value={fmtPct(t!.blocked_pct)} sub={`${fmtNum(t!.blocked)} queries`} icon={<Ban className="h-4 w-4" />} />
              <StatCard label="Avg. processing" value={`${t!.avg_elapsed_ms.toFixed(1)} ms`} sub="Time AdGuard Home took to answer" icon={<Clock className="h-4 w-4" />} />
              <StatCard label="Served from cache" value={fmtPct(t!.cached_pct)} sub={`${fmtNum(t!.cached)} queries`} icon={<Database className="h-4 w-4" />} />
              <StatCard label="Active clients" value={fmtNum(t!.clients)} sub="Distinct client addresses" icon={<Users className="h-4 w-4" />} />
            </div>
            {empty ? (
              <Card>
                <EmptyState
                  icon={<Activity className="h-6 w-6" />}
                  title="No queries in this range yet"
                  description="GuardTower collects query logs from your instances every few seconds. If you just added instances, data appears shortly. Make sure query logging is enabled in AdGuard Home."
                />
              </Card>
            ) : (
              <>
                <Card>
                  <CardHeader title="Queries over time" description={`Per ${data.bucket_ms >= 3600_000 ? data.bucket_ms / 3600_000 + ' h' : data.bucket_ms / 60_000 + ' min'}`} />
                  <div className="px-3 pb-3">
                    <TimeSeriesChart series={series} bucketMs={data.bucket_ms} />
                  </div>
                </Card>
                <div className="grid gap-6 xl:grid-cols-2">
                  <Card>
                    <CardHeader title="Traffic by instance" description="Share of all queries handled by each instance" />
                    <div className="px-3 pb-4">
                      <BarList
                        items={data.instances.map((i) => ({ key: String(i.instance_id), label: instName(i.instance_id), value: i.queries, sub: fmtPct((i.queries * 100) / Math.max(1, t!.queries)) }))}
                        colorFor={(k) => instColor(Number(k))}
                        onSelect={(k) => set('instance', k)}
                      />
                    </div>
                  </Card>
                  <Card>
                    <CardHeader title="Blocking rate by instance" description="Percentage of each instance's queries that were blocked" />
                    <div className="px-3 pb-4">
                      <BarList
                        items={[...data.instances]
                          .map((i) => ({ key: String(i.instance_id), label: instName(i.instance_id), value: i.queries ? (i.blocked * 100) / i.queries : 0, sub: `${fmtNum(i.blocked)} blocked` }))
                          .sort((a, b) => b.value - a.value)}
                        colorFor={(k) => instColor(Number(k))}
                        max={100}
                        format={(n) => fmtPct(n)}
                        onSelect={(k) => set('instance', k)}
                      />
                    </div>
                  </Card>
                </div>
                <div className="grid gap-6 xl:grid-cols-3">
                  <Card>
                    <CardHeader title="Top queried domains" />
                    <div className="px-3 pb-4">
                      <BarList items={data.top_domains.map((d) => ({ key: d.key, value: d.count }))} onSelect={(k) => toLog('domain', k)} />
                    </div>
                  </Card>
                  <Card>
                    <CardHeader title="Top blocked domains" />
                    <div className="px-3 pb-4">
                      <BarList items={data.top_blocked.map((d) => ({ key: d.key, value: d.count }))} colorFor={() => 'var(--series-2)'} onSelect={(k) => toLog('domain', k)} />
                    </div>
                  </Card>
                  <Card>
                    <CardHeader title="Top clients" />
                    <div className="px-3 pb-4">
                      <BarList
                        items={data.top_clients.map((d) => ({ key: d.key, label: d.label ? `${d.label} (${d.key})` : d.key, value: d.count }))}
                        colorFor={() => 'var(--series-3)'}
                        onSelect={(k) => toLog('client', k)}
                      />
                    </div>
                  </Card>
                </div>
                <div className="grid gap-6 xl:grid-cols-3">
                  <Card>
                    <CardHeader title="Outcome" description="What happened to each query" />
                    <div className="space-y-2 px-5 pb-5">
                      {data.statuses.map((s) => {
                        const meta = statusMeta[s.key as keyof typeof statusMeta]
                        const Icon = meta?.icon
                        return (
                          <button key={s.key} onClick={() => toLog('status', s.key)} className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-sm hover:bg-hover">
                            {Icon && <Icon className="h-4 w-4 text-muted" />}
                            <span>{meta?.label ?? s.key}</span>
                            <span className="ml-auto tabular-nums text-muted">{fmtNum(s.count)}</span>
                            <span className="w-14 text-right text-xs tabular-nums text-faint">{fmtPct((s.count * 100) / Math.max(1, t!.queries))}</span>
                          </button>
                        )
                      })}
                    </div>
                  </Card>
                  <Card className="xl:col-span-2">
                    <CardHeader title="When queries happen" description="Total queries by day of week and hour (server time)" />
                    <div className="px-3 pb-3">
                      <HourHeatmap grid={data.heatmap} />
                    </div>
                  </Card>
                </div>
                {t!.from_rollups && <p className="text-xs text-faint">Totals and charts for longer ranges come from hourly rollups, which are kept longer than raw query logs.</p>}
              </>
            )}
          </>
        )}
      </div>
    </>
  )
}
