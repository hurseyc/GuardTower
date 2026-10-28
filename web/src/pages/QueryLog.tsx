import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, ChevronRight, Pause, Play, ScrollText, Search, X } from 'lucide-react'
import { api, type Overview, type QueryPage, type QueryRow } from '@/lib/api'
import { onServerEvent } from '@/lib/events'
import { allStatuses, statusMeta } from '@/lib/status'
import { cn, colorFor, fmtDateTime, timeAgo } from '@/lib/utils'
import { PageHeader } from '@/components/AppShell'
import { Badge, Button, Card, EmptyState, Input, Select, Skeleton, Switch, Table, Td, Th, Tip } from '@/components/ui'
import { FleetFilterBar } from './Analytics'
import { useFleetFilter } from '@/lib/fleetFilter'
import { useSearchParams } from 'react-router'

export function StatusBadge({ status }: { status: QueryRow['status'] }) {
  const m = statusMeta[status] ?? statusMeta.allowed
  const Icon = m.icon
  return (
    <Badge tone={m.tone}>
      <Icon className="h-3 w-3" />
      {m.label}
    </Badge>
  )
}

export function QueryLogPage() {
  const qc = useQueryClient()
  const [params, setParams] = useSearchParams()
  const { group, instance, set } = useFleetFilter()
  const domain = params.get('domain') ?? ''
  const contains = params.get('contains') ?? ''
  const client = params.get('client') ?? ''
  const status = params.get('status') ?? ''
  const range = params.get('range') ?? ''
  const [search, setSearch] = useState(domain || contains)
  const [anywhere, setAnywhere] = useState(!!contains)
  const [clientInput, setClientInput] = useState(client)
  const [live, setLive] = useState(true)
  const [expanded, setExpanded] = useState<number | null>(null)
  const { data: overview } = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview') })

  const filterQS = useMemo(() => {
    const qs = new URLSearchParams()
    for (const k of ['group', 'instance', 'domain', 'contains', 'client', 'status', 'range']) {
      const v = params.get(k)
      if (v) qs.set(k, v)
    }
    if (status === 'blocked-any') {
      qs.delete('status')
      qs.set('blocked', '1')
    }
    return qs.toString()
  }, [params, status])

  const q = useInfiniteQuery({
    queryKey: ['querylog', filterQS],
    initialPageParam: null as QueryPage['next'] | null,
    queryFn: ({ pageParam }) =>
      api.get<QueryPage>(`/querylog?limit=100${filterQS ? '&' + filterQS : ''}${pageParam ? `&after_ts=${pageParam.ts}&after_id=${pageParam.id}` : ''}`),
    getNextPageParam: (last) => last.next ?? undefined,
  })

  // Live tail: when new rows arrive anywhere, refresh the first page (debounced).
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => {
    if (!live) return
    return onServerEvent((type) => {
      if (type !== 'querylog.rows') return
      clearTimeout(timer.current)
      timer.current = setTimeout(() => {
        // Only the first page refreshes; deeper pages stay where the user scrolled.
        qc.setQueryData(['querylog', filterQS], (old: { pages: QueryPage[]; pageParams: unknown[] } | undefined) =>
          old ? { pages: old.pages.slice(0, 1), pageParams: old.pageParams.slice(0, 1) } : old,
        )
        void qc.invalidateQueries({ queryKey: ['querylog', filterQS] })
      }, 800)
    })
  }, [live, qc, filterQS])

  const setParam = (k: string, v: string) => {
    const n = new URLSearchParams(params)
    if (v) n.set(k, v)
    else n.delete(k)
    setParams(n, { replace: true })
  }
  const applySearch = () => {
    const n = new URLSearchParams(params)
    n.delete('domain')
    n.delete('contains')
    const v = search.trim()
    if (v) n.set(anywhere || v.includes('*') ? 'contains' : 'domain', v.replace(/\*/g, ''))
    if (clientInput.trim()) n.set('client', clientInput.trim())
    else n.delete('client')
    setParams(n, { replace: true })
  }
  const clearAll = () => {
    setSearch('')
    setClientInput('')
    setAnywhere(false)
    setParams(new URLSearchParams(), { replace: true })
  }

  const rows = q.data?.pages.flatMap((p) => p.rows) ?? []
  const instById = new Map((overview?.instances ?? []).map((i) => [i.id, i]))
  const groupById = new Map((overview?.groups ?? []).map((g) => [g.id, g]))
  const hasFilters = !!(domain || contains || client || status || group || instance || range)

  return (
    <>
      <PageHeader
        title="Query log"
        description="Every DNS query from every instance, stored and searchable in one place."
        actions={
          <label className="flex items-center gap-2 rounded-lg border border-border px-3 py-1.5 text-sm">
            {live ? <Play className="h-3.5 w-3.5 text-ok" /> : <Pause className="h-3.5 w-3.5 text-muted" />}
            Live
            <Switch checked={live} onCheckedChange={setLive} />
          </label>
        }
      >
        <form
          className="mt-4 flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            applySearch()
          }}
        >
          <div className="relative w-full max-w-sm">
            <Search className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-faint" />
            <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Domain, e.g. google.com" className="pl-9 font-mono" aria-label="Domain" />
          </div>
          <label className="flex items-center gap-2 text-xs text-muted">
            <input type="checkbox" checked={anywhere} onChange={(e) => setAnywhere(e.target.checked)} className="accent-[var(--accent)]" />
            match anywhere
          </label>
          <Input value={clientInput} onChange={(e) => setClientInput(e.target.value)} placeholder="Client IP or name" className="w-48 font-mono" aria-label="Client" />
          <Select value={status} onChange={(e) => setParam('status', e.target.value)} className="w-44" aria-label="Status">
            <option value="">Any outcome</option>
            <option value="blocked-any">Any block</option>
            {allStatuses.map((s) => (
              <option key={s} value={s}>
                {statusMeta[s].label}
              </option>
            ))}
          </Select>
          <Select value={range} onChange={(e) => setParam('range', e.target.value)} className="w-36" aria-label="Time range">
            <option value="">Any time</option>
            <option value="15m">Last 15 min</option>
            <option value="1h">Last hour</option>
            <option value="24h">Last 24 hours</option>
            <option value="7d">Last 7 days</option>
          </Select>
          <Button type="submit" variant="primary">
            Search
          </Button>
          {hasFilters && (
            <Button type="button" variant="ghost" onClick={clearAll}>
              <X className="h-4 w-4" /> Clear
            </Button>
          )}
        </form>
        <FleetFilterBar overview={overview} group={group} instance={instance} set={set} showRange={false} />
      </PageHeader>
      <div className="p-6">
        <Card>
          {q.isLoading ? (
            <div className="space-y-2 p-4">
              {Array.from({ length: 8 }).map((_, i) => (
                <Skeleton key={i} className="h-9" />
              ))}
            </div>
          ) : rows.length === 0 ? (
            <EmptyState
              icon={<ScrollText className="h-6 w-6" />}
              title={hasFilters ? 'No queries match these filters' : 'No queries collected yet'}
              description={
                hasFilters
                  ? 'Try a wider time range or remove a filter. Domain search matches the domain and all its subdomains.'
                  : 'GuardTower collects query logs from your instances every few seconds. Make sure query logging is enabled in each AdGuard Home instance.'
              }
            />
          ) : (
            <>
              <Table>
                <thead>
                  <tr>
                    <Th className="w-8" />
                    <Th>Time</Th>
                    <Th>Domain</Th>
                    <Th>Outcome</Th>
                    <Th>Client</Th>
                    <Th>Instance</Th>
                    <Th className="text-right">Response</Th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((r) => {
                    const inst = instById.get(r.instance_id)
                    const g = inst?.group_id ? groupById.get(inst.group_id) : undefined
                    const open = expanded === r.id
                    return (
                      <Fragment key={r.id}>
                        <tr className={cn('cursor-pointer hover:bg-hover/50', open && 'bg-hover/40')} onClick={() => setExpanded(open ? null : r.id)}>
                          <Td className="py-2 pr-0 text-faint">{open ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}</Td>
                          <Td className="py-2 whitespace-nowrap text-xs text-muted">
                            <Tip content={fmtDateTime(r.ts)}>
                              <span>{new Date(r.ts).toLocaleTimeString()}</span>
                            </Tip>
                          </Td>
                          <Td className="py-2 max-w-md">
                            <div className="truncate font-mono text-[13px]">{r.domain}</div>
                            <div className="text-[11px] text-faint">{r.qtype}</div>
                          </Td>
                          <Td>
                            <StatusBadge status={r.status} />
                          </Td>
                          <Td className="py-2 text-sm">
                            <div className="truncate">{r.client_name || r.client}</div>
                            {r.client_name && <div className="font-mono text-[11px] text-faint">{r.client}</div>}
                          </Td>
                          <Td className="py-2 text-sm">
                            <span className="inline-flex items-center gap-1.5">
                              {g && <span className="h-2 w-2 rounded-full" style={{ background: colorFor(g.name, g.color) }} title={g.name} />}
                              {inst?.name ?? `#${r.instance_id}`}
                            </span>
                          </Td>
                          <Td className="py-2 whitespace-nowrap text-right text-xs tabular-nums text-muted">
                            {r.elapsed_ms.toFixed(1)} ms{r.cached && <Badge className="ml-1.5">cached</Badge>}
                          </Td>
                        </tr>
                        {open && (
                          <tr className="bg-subtle/60">
                            <td colSpan={7} className="border-b border-border px-12 py-3">
                              <dl className="grid gap-x-8 gap-y-2 text-xs sm:grid-cols-3">
                                <Detail label="When">{fmtDateTime(r.ts)} ({timeAgo(r.ts)})</Detail>
                                <Detail label="AdGuard Home reason" mono>{r.reason || '—'}</Detail>
                                <Detail label="Protocol">{r.proto || 'dns'}</Detail>
                                {r.rule && <Detail label="Matched rule" mono>{r.rule}</Detail>}
                                {!!r.filter_id && <Detail label="Filter list ID" mono>{r.filter_id}</Detail>}
                                {r.service && <Detail label="Blocked service">{r.service}</Detail>}
                                {r.upstream && <Detail label="Upstream" mono>{r.upstream}</Detail>}
                                {r.answer && <Detail label="Answer" mono>{r.answer}</Detail>}
                                {r.client_id && <Detail label="ClientID" mono>{r.client_id}</Detail>}
                              </dl>
                              <div className="mt-3 flex gap-2">
                                <Button size="sm" variant="outline" onClick={() => { setSearch(r.domain); setAnywhere(false); setParam('domain', r.domain) }}>
                                  Show all queries for this domain
                                </Button>
                                <Button size="sm" variant="outline" onClick={() => { setClientInput(r.client); setParam('client', r.client) }}>
                                  Show all from this client
                                </Button>
                              </div>
                            </td>
                          </tr>
                        )}
                      </Fragment>
                    )
                  })}
                </tbody>
              </Table>
              <div className="flex items-center justify-between border-t border-border px-4 py-3 text-xs text-faint">
                <span>
                  Showing {rows.length.toLocaleString()} {rows.length === 1 ? 'query' : 'queries'}
                  {live && ' · updating live'}
                </span>
                {q.hasNextPage && (
                  <Button size="sm" variant="ghost" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                    Load older
                  </Button>
                )}
              </div>
            </>
          )}
        </Card>
      </div>
    </>
  )
}

function Detail({ label, children, mono }: { label: string; children: React.ReactNode; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <dt className="text-faint">{label}</dt>
      <dd className={cn('mt-0.5 break-all text-fg', mono && 'font-mono')}>{children}</dd>
    </div>
  )
}
