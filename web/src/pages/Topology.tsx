import { useMemo } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { Background, Controls, Handle, Position, ReactFlow, type Edge, type Node, type NodeProps } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { HardDrive, MapPin, Network, Server, Users } from 'lucide-react'
import { api, type Group, type Host, type Instance, type Overview, type StatsSummary } from '@/lib/api'
import { cn, colorFor, fmtNum } from '@/lib/utils'
import { PageHeader } from '@/components/AppShell'
import { FindingList, ScoreIcon, scoreLabel, useResilience, type GroupReport } from '@/components/Resilience'
import { Card, EmptyState, Skeleton, TabsList, TabsTrigger } from '@/components/ui'
import * as Tabs from '@radix-ui/react-tabs'

// ---- node renderers ----

type InstanceData = { inst: Instance; group?: Group; host?: Host; dim: boolean; qps: number; risky: boolean; showHost: boolean }
type BoxData = { title: string; sub?: string; kind: 'site' | 'host' | 'unassigned'; hostKind?: string; spof?: string; dim: boolean }
type GroupData = { group: Group; report?: GroupReport; dim: boolean; active: boolean }
type ClientsData = { qps: number }

function hostOf(url: string) {
  try {
    return new URL(url).hostname
  } catch {
    return url
  }
}

function InstanceNode({ data }: NodeProps<Node<InstanceData>>) {
  const { inst, group, host, dim, qps, risky, showHost } = data
  const up = inst.status?.online
  return (
    <div
      className={cn('w-[210px] cursor-pointer rounded-xl border bg-elevated px-3 py-2 shadow-card transition-opacity', risky ? 'border-warn' : 'border-border', dim && 'opacity-25')}
      style={{ borderLeft: `4px solid ${group ? colorFor(group.name, group.color) : 'var(--border-strong)'}` }}
    >
      <Handle type="target" position={Position.Left} id="l" className="!h-1.5 !w-1.5 !border-0 !bg-border-strong" />
      <div className="flex items-center gap-2">
        <span className={cn('h-2 w-2 shrink-0 rounded-full', up ? 'bg-ok' : 'bg-danger')} aria-label={up ? 'online' : 'offline'} />
        <span className="truncate text-sm font-medium">{inst.name}</span>
        <span className="ml-auto font-mono text-[10px] text-faint">{inst.status?.version}</span>
      </div>
      <div className="mt-0.5 flex items-center gap-2 font-mono text-[11px] text-muted">
        <span className="truncate">{hostOf(inst.url)}</span>
        {qps >= 0.05 && <span className="ml-auto shrink-0">{qps < 10 ? qps.toFixed(1) : Math.round(qps)} q/s</span>}
      </div>
      {(showHost || group) && (
        <div className="mt-0.5 truncate text-[11px] text-faint">
          {group?.name ?? 'Ungrouped'}
          {showHost && ` · ${host?.name ?? 'no host'}`}
        </div>
      )}
    </div>
  )
}

function BoxNode({ data }: NodeProps<Node<BoxData>>) {
  const Icon = data.kind === 'site' ? MapPin : data.hostKind === 'nas' ? HardDrive : Server
  return (
    <div
      className={cn(
        'h-full w-full rounded-2xl border-2 transition-opacity',
        data.kind === 'site' ? 'border-dashed border-border bg-subtle/30' : data.spof ? 'border-dashed border-warn bg-warn-soft/30' : 'border-border bg-subtle/60',
        data.kind === 'unassigned' && 'border-dotted',
        data.dim && 'opacity-40',
      )}
    >
      <div className="flex items-center gap-1.5 px-3 pt-2 text-xs font-semibold">
        <Icon className="h-3.5 w-3.5 text-muted" />
        {data.title}
        {data.sub && <span className="font-mono font-normal text-faint">{data.sub}</span>}
      </div>
      {data.spof && <div className="px-3 text-[11px] text-warn">only host of {data.spof}</div>}
      {data.kind !== 'site' && <Handle type="target" position={Position.Top} className="!h-1.5 !w-1.5 !border-0 !bg-border-strong" />}
    </div>
  )
}

function GroupNode({ data }: NodeProps<Node<GroupData>>) {
  const { group, report, dim, active } = data
  return (
    <div className={cn('w-[220px] cursor-pointer rounded-xl border bg-elevated px-3 py-2.5 shadow-card transition-opacity', active ? 'ring-2 ring-ring' : 'border-border', dim && 'opacity-30')}>
      <Handle type="source" position={Position.Right} className="!h-1.5 !w-1.5 !border-0 !bg-border-strong" />
      <div className="flex items-center gap-2 text-sm font-semibold">
        <span className="h-2.5 w-2.5 rounded-full" style={{ background: colorFor(group.name, group.color) }} />
        {group.name}
      </div>
      {report && (
        <div className="mt-1 flex items-center gap-1.5 text-xs text-muted">
          <ScoreIcon score={report.score} className="h-3.5 w-3.5" />
          {scoreLabel[report.score]} · {report.online}/{report.members} online
        </div>
      )}
    </div>
  )
}

function ClientsNode({ data }: NodeProps<Node<ClientsData>>) {
  return (
    <div className="flex items-center gap-2 rounded-full border border-border bg-elevated px-4 py-2 text-sm shadow-card">
      <Users className="h-4 w-4 text-muted" />
      DNS clients
      {data.qps > 0 && <span className="font-mono text-xs text-muted">{data.qps.toFixed(1)} q/s</span>}
      <Handle type="source" position={Position.Bottom} className="!h-1.5 !w-1.5 !border-0 !bg-border-strong" />
    </div>
  )
}

const nodeTypes = { instance: InstanceNode, box: BoxNode, grp: GroupNode, clients: ClientsNode }

// ---- layouts ----

const INST_W = 210
const INST_H = 64
const PAD = 16
const HEAD = 44

interface Ctx {
  ov: Overview
  reports: Map<number, GroupReport>
  spofHosts: Record<string, number[]>
  risky: Map<number, string>
  qps: Map<number, number>
  focus: number
}

function physical({ ov, reports, spofHosts, risky, qps, focus }: Ctx): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = []
  const edges: Edge[] = []
  const groupById = new Map(ov.groups.map((g) => [g.id, g]))
  const inFocus = (i: Instance) => !focus || i.group_id === focus
  const hostCols = [...ov.hosts.map((h) => ({ host: h as Host | undefined, site: h.site_id ?? 0 }))]
  const unplaced = ov.instances.filter((i) => !i.host_id || !ov.hosts.some((h) => h.id === i.host_id))
  if (unplaced.length) hostCols.push({ host: undefined, site: 0 })
  // Sites, in name order, then hosts without a site.
  const siteOrder = [...ov.sites].sort((a, b) => a.name.localeCompare(b.name)).map((s) => s.id)
  if (hostCols.some((c) => !c.site)) siteOrder.push(0)
  const showSites = ov.sites.length > 0
  let x = 0
  const top = 140
  for (const sid of siteOrder) {
    const cols = hostCols.filter((c) => c.site === sid)
    if (!cols.length) continue
    const siteX = x
    let maxH = 0
    const siteId = `site-${sid}`
    const startX = x + (showSites ? PAD : 0)
    let cx = startX
    for (const c of cols) {
      const members = c.host ? ov.instances.filter((i) => i.host_id === c.host!.id) : unplaced
      const h = HEAD + Math.max(1, members.length) * (INST_H + 10) + PAD
      const w = INST_W + PAD * 2
      const id = c.host ? `host-${c.host.id}` : 'host-none'
      const spof = c.host ? (spofHosts[String(c.host.id)] ?? []).map((g) => groupById.get(g)?.name).filter(Boolean).join(', ') : ''
      nodes.push({
        id,
        type: 'box',
        position: { x: cx, y: top + (showSites ? HEAD : 0) },
        style: { width: w, height: h },
        selectable: false,
        data: {
          title: c.host?.name ?? 'No host recorded',
          sub: c.host?.address,
          kind: c.host ? 'host' : 'unassigned',
          hostKind: c.host?.kind,
          spof: focus && spof && !(spofHosts[String(c.host?.id)] ?? []).includes(focus) ? '' : spof,
          dim: !!focus && !members.some(inFocus),
        } satisfies BoxData,
      })
      members.forEach((i, k) => {
        nodes.push({
          id: `inst-${i.id}`,
          type: 'instance',
          parentId: id,
          extent: 'parent',
          position: { x: PAD, y: HEAD + k * (INST_H + 10) },
          data: {
            inst: i,
            group: i.group_id ? groupById.get(i.group_id) : undefined,
            host: c.host,
            dim: !inFocus(i),
            qps: qps.get(i.id) ?? 0,
            risky: risky.has(i.id) && (!focus || i.group_id === focus),
            showHost: false,
          } satisfies InstanceData,
        })
      })
      maxH = Math.max(maxH, h)
      cx += w + 24
    }
    if (showSites) {
      const site = ov.sites.find((s) => s.id === sid)
      nodes.unshift({
        id: siteId,
        type: 'box',
        position: { x: siteX, y: top },
        style: { width: cx - siteX - 24 + PAD, height: maxH + HEAD + PAD },
        selectable: false,
        zIndex: -1,
        data: { title: site?.name ?? 'No site', kind: 'site', dim: false } satisfies BoxData,
      })
      x = cx + 24
    } else {
      x = cx
    }
  }
  const total = [...qps.values()].reduce((a, b) => a + b, 0)
  nodes.push({ id: 'clients', type: 'clients', position: { x: Math.max(0, x / 2 - 90), y: 0 }, data: { qps: total } satisfies ClientsData, selectable: false })
  // One edge per host, as wide as the traffic its instances answer.
  const perHost = new Map<string, { q: number; up: boolean; focus: boolean }>()
  for (const i of ov.instances) {
    const id = i.host_id && ov.hosts.some((h) => h.id === i.host_id) ? `host-${i.host_id}` : 'host-none'
    const cur = perHost.get(id) ?? { q: 0, up: false, focus: false }
    cur.q += qps.get(i.id) ?? 0
    cur.up ||= !!i.status?.online
    cur.focus ||= inFocus(i)
    perHost.set(id, cur)
  }
  const maxQ = Math.max(0.001, ...[...perHost.values()].map((v) => v.q))
  for (const [id, v] of perHost) {
    edges.push({
      id: `e-${id}`,
      source: 'clients',
      target: id,
      animated: v.q > 0 && v.up,
      style: { strokeWidth: 1.5 + (v.q / maxQ) * 8, stroke: 'var(--border-strong)', opacity: v.focus ? 0.9 : 0.15 },
    })
  }
  void reports
  return { nodes, edges }
}

function logical({ ov, reports, risky, qps, focus }: Ctx): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = []
  const edges: Edge[] = []
  const hostById = new Map(ov.hosts.map((h) => [h.id, h]))
  const groups = [...ov.groups].sort((a, b) => a.name.localeCompare(b.name))
  const ungrouped = ov.instances.filter((i) => !i.group_id)
  let y = 0
  const rows: { g?: Group; members: Instance[] }[] = groups.map((g) => ({ g, members: ov.instances.filter((i) => i.group_id === g.id) }))
  if (ungrouped.length) rows.push({ members: ungrouped })
  for (const row of rows) {
    const h = Math.max(1, row.members.length) * (INST_H + 12)
    const gid = row.g?.id ?? 0
    const dim = !!focus && gid !== focus
    if (row.g) {
      nodes.push({
        id: `group-${gid}`,
        type: 'grp',
        position: { x: 0, y: y + h / 2 - 30 },
        data: { group: row.g, report: reports.get(gid), dim, active: focus === gid } satisfies GroupData,
      })
    } else {
      nodes.push({ id: 'group-none', type: 'box', position: { x: 0, y: y + h / 2 - 30 }, style: { width: 220, height: 56 }, data: { title: 'Ungrouped', kind: 'unassigned', dim } satisfies BoxData })
    }
    row.members.forEach((i, k) => {
      nodes.push({
        id: `inst-${i.id}`,
        type: 'instance',
        position: { x: 380, y: y + k * (INST_H + 12) },
        data: {
          inst: i,
          group: row.g,
          host: i.host_id ? hostById.get(i.host_id) : undefined,
          dim,
          qps: qps.get(i.id) ?? 0,
          risky: risky.has(i.id),
          showHost: true,
        } satisfies InstanceData,
      })
      if (row.g) {
        const spof = reports.get(gid)?.findings.some((f) => f.kind === 'shared_host' && f.instance_ids.includes(i.id))
        edges.push({
          id: `g-${gid}-${i.id}`,
          source: `group-${gid}`,
          target: `inst-${i.id}`,
          targetHandle: 'l',
          style: { stroke: spof ? 'var(--warn)' : colorFor(row.g.name, row.g.color), strokeWidth: 2, strokeDasharray: spof ? '6 4' : undefined, opacity: dim ? 0.15 : 0.9 },
        })
      }
    })
    y += h + 32
  }
  return { nodes, edges }
}

// ---- page ----

export function TopologyPage() {
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const view = params.get('view') === 'logical' ? 'logical' : 'physical'
  const focus = Number(params.get('group') ?? 0)
  const { data: ov } = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview'), refetchInterval: 30_000 })
  const { data: res } = useResilience()
  const { data: stats } = useQuery({ queryKey: ['stats', '1h', 'topology'], queryFn: () => api.get<StatsSummary>('/stats/summary?range=1h&top=1'), refetchInterval: 60_000 })
  const set = (k: string, v: string) => {
    const n = new URLSearchParams(params)
    if (v) n.set(k, v)
    else n.delete(k)
    setParams(n, { replace: true })
  }

  const graph = useMemo(() => {
    if (!ov) return null
    const seconds = stats ? Math.max(1, (stats.to - stats.from) / 1000) : 3600
    const ctx: Ctx = {
      ov,
      reports: new Map((res?.groups ?? []).map((g) => [g.group_id, g])),
      spofHosts: res?.spof_hosts ?? {},
      risky: new Map(Object.entries(res?.instances ?? {}).filter(([, s]) => s !== 'info').map(([k, s]) => [Number(k), s])),
      qps: new Map((stats?.instances ?? []).map((s) => [s.instance_id, s.queries / seconds])),
      focus,
    }
    return view === 'logical' ? logical(ctx) : physical(ctx)
  }, [ov, res, stats, view, focus])

  const focused = res?.groups.find((g) => g.group_id === focus)

  return (
    <>
      <PageHeader title="Topology" description="Where every DNS server runs, which group it serves, and what would fail together. Click a group to focus it, or an instance to open it." />
      <div className="space-y-4 p-6">
        <div className="flex flex-wrap items-center gap-3">
          <Tabs.Root value={view} onValueChange={(v) => set('view', v === 'physical' ? '' : v)}>
            <TabsList>
              <TabsTrigger value="physical">
                <Server className="mr-1.5 inline h-3.5 w-3.5" /> Physical
              </TabsTrigger>
              <TabsTrigger value="logical">
                <Network className="mr-1.5 inline h-3.5 w-3.5" /> By group
              </TabsTrigger>
            </TabsList>
          </Tabs.Root>
          <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Focus a group">
            <button onClick={() => set('group', '')} className={cn('rounded-full border px-3 py-1 text-xs', !focus ? 'border-ring bg-accent-soft font-medium' : 'border-border hover:bg-hover')} aria-pressed={!focus}>
              All groups
            </button>
            {ov?.groups.map((g) => {
              const rep = res?.groups.find((x) => x.group_id === g.id)
              return (
                <button
                  key={g.id}
                  onClick={() => set('group', focus === g.id ? '' : String(g.id))}
                  aria-pressed={focus === g.id}
                  className={cn('inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs', focus === g.id ? 'border-ring bg-accent-soft font-medium' : 'border-border hover:bg-hover')}
                >
                  <span className="h-2 w-2 rounded-full" style={{ background: colorFor(g.name, g.color) }} />
                  {g.name}
                  {rep && rep.score !== 'redundant' && <ScoreIcon score={rep.score} className="h-3 w-3" />}
                </button>
              )
            })}
          </div>
          <div className="ml-auto flex items-center gap-4 text-xs text-muted">
            <span className="inline-flex items-center gap-1.5">
              <span className="h-3 w-5 rounded border-2 border-dashed border-warn" /> shared host (SPOF)
            </span>
            <span>edge width = queries/s (last hour)</span>
          </div>
        </div>

        {focused && focused.findings.length > 0 && <FindingList findings={focused.findings} />}

        <Card className="h-[640px] overflow-hidden">
          {!ov || !graph ? (
            <Skeleton className="m-5 h-[600px]" />
          ) : ov.instances.length === 0 ? (
            <EmptyState icon={<Network className="h-6 w-6" />} title="Nothing to draw yet" description="Add instances and hosts to see your fleet's layout." />
          ) : (
            <ReactFlow
              key={view}
              nodes={graph.nodes}
              edges={graph.edges}
              nodeTypes={nodeTypes}
              fitView
              fitViewOptions={{ padding: 0.15 }}
              nodesDraggable={false}
              nodesConnectable={false}
              proOptions={{ hideAttribution: true }}
              minZoom={0.2}
              onNodeClick={(_, n) => {
                if (n.id.startsWith('inst-')) void navigate(`/instances/${n.id.slice(5)}`)
                else if (n.id.startsWith('group-') && n.id !== 'group-none') set('group', focus === Number(n.id.slice(6)) ? '' : n.id.slice(6))
              }}
            >
              <Background gap={24} size={1} color="var(--border)" />
              <Controls showInteractive={false} />
            </ReactFlow>
          )}
        </Card>
        {stats && stats.totals.queries > 0 && (
          <p className="text-xs text-faint">
            {fmtNum(stats.totals.queries)} queries in the last hour across {stats.instances.length} instance{stats.instances.length === 1 ? '' : 's'}.
          </p>
        )}
      </div>
    </>
  )
}
