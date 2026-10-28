import { useState } from 'react'
import { Link, NavLink, useNavigate, useParams, useSearchParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { AlertTriangle, Eye, EyeOff, Heart, Info, Moon, Plus, ShieldCheck, Smartphone, Trash2, UserRound } from 'lucide-react'
import {
  api,
  type FamilyCategory,
  type FamilyClient,
  type FamilyDetail,
  type FamilyEvent,
  type FamilyList,
  type FamilyOverview,
  type FamilySignal,
  type Overview,
  type Person,
} from '@/lib/api'
import { useSession } from '@/lib/session'
import { cn, colorFor, fmtDateTime, fmtNum, timeAgo } from '@/lib/utils'
import { PageHeader } from '@/components/AppShell'
import { Badge, Button, Card, CardHeader, ConfirmDialog, Dialog, EmptyState, Field, Input, Select, Skeleton, Switch, Table, Td, Th } from '@/components/ui'
import { BarList, HourHeatmap, TimeSeriesChart } from '@/components/charts'
import { StatCard } from './Overview'

export const categoryLabel: Record<FamilyCategory, string> = {
  adult: 'Adult',
  gambling: 'Gambling',
  bypass: 'VPN & private DNS',
  social: 'Social media',
  gaming: 'Gaming',
  malware: 'Malware',
  safesearch: 'Safe search',
  ads: 'Ads & tracking',
}
// Fixed palette slot per category (identity, never rank).
const categorySlot: Record<FamilyCategory, number> = { adult: 4, gambling: 3, bypass: 6, social: 0, gaming: 2, malware: 7, safesearch: 5, ads: 1 }
const catColor = (c: FamilyCategory) => `var(--series-${categorySlot[c] + 1})`
const shownCategories: FamilyCategory[] = ['adult', 'gambling', 'bypass', 'social', 'gaming', 'malware']

const ranges = [
  { value: '7', label: '7 days' },
  { value: '30', label: '30 days' },
  { value: '90', label: '90 days' },
]

function useDays() {
  const [params, setParams] = useSearchParams()
  const days = params.get('days') ?? '7'
  const setDays = (d: string) => {
    const n = new URLSearchParams(params)
    n.set('days', d)
    setParams(n, { replace: true })
  }
  return { days, setDays, params, setParams }
}

function FamilyNav() {
  const tabs = [
    { to: '/family', label: 'Overview', end: true },
    { to: '/family/devices', label: 'People & devices' },
    { to: '/family/settings', label: 'Settings' },
  ]
  return (
    <nav className="mt-4 flex gap-1" aria-label="Family DNS sections">
      {tabs.map((t) => (
        <NavLink
          key={t.to}
          to={t.to}
          end={t.end}
          className={({ isActive }) => cn('rounded-lg px-3 py-1.5 text-sm', isActive ? 'bg-accent-soft font-medium text-fg' : 'text-muted hover:bg-hover hover:text-fg')}
        >
          {t.label}
        </NavLink>
      ))}
    </nav>
  )
}

function RangePicker({ days, setDays }: { days: string; setDays: (d: string) => void }) {
  return (
    <div className="flex rounded-lg border border-border p-0.5" role="group" aria-label="Time range">
      {ranges.map((r) => (
        <button key={r.value} onClick={() => setDays(r.value)} aria-pressed={days === r.value} className={cn('rounded-md px-2.5 py-1 text-xs', days === r.value ? 'bg-subtle font-medium' : 'text-muted hover:text-fg')}>
          {r.label}
        </button>
      ))}
    </div>
  )
}

function Avatar({ name, emoji, color, size = 'md' }: { name: string; emoji?: string; color?: string; size?: 'md' | 'lg' }) {
  return (
    <span
      className={cn('inline-flex shrink-0 items-center justify-center rounded-full font-semibold text-white', size === 'lg' ? 'h-12 w-12 text-xl' : 'h-9 w-9 text-base')}
      style={{ background: emoji ? 'var(--bg-subtle)' : colorFor(name, color) }}
      aria-hidden
    >
      {emoji || name.slice(0, 1).toUpperCase()}
    </span>
  )
}

/** A masked domain with a Reveal button (each reveal is audited). */
export function SiteName({ eventId, domain, masked }: { eventId: number; domain: string; masked: boolean }) {
  const [shown, setShown] = useState<string | null>(null)
  const reveal = useMutation({
    mutationFn: () => api.post<{ domain: string }>(`/family/events/${eventId}/reveal`, {}),
    onSuccess: (r) => setShown(r.domain),
    onError: (e) => toast.error((e as Error).message),
  })
  if (!masked) return <span className="font-mono">{domain}</span>
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="font-mono">{shown ?? domain}</span>
      {shown ? (
        <button onClick={() => setShown(null)} className="text-faint hover:text-fg" aria-label="Hide domain">
          <EyeOff className="h-3.5 w-3.5" />
        </button>
      ) : (
        <button onClick={() => reveal.mutate()} className="text-faint hover:text-fg" aria-label="Reveal domain (recorded in the audit log)" title="Reveal (recorded in the audit log)">
          <Eye className="h-3.5 w-3.5" />
        </button>
      )}
    </span>
  )
}

function SignalList({ signals }: { signals: FamilySignal[] }) {
  if (signals.length === 0)
    return (
      <p className="flex items-center gap-2 text-sm text-muted">
        <ShieldCheck className="h-4 w-4 text-ok" /> No signs of filters being bypassed.
      </p>
    )
  return (
    <ul className="space-y-2">
      {signals.map((s, i) => (
        <li key={i} className={cn('flex items-start gap-2 rounded-lg p-2.5 text-sm', s.severity === 'warn' ? 'bg-warn-soft' : 'bg-subtle text-muted')}>
          {s.severity === 'warn' ? <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warn" /> : <Info className="mt-0.5 h-4 w-4 shrink-0" />}
          <span>
            {s.message}
            {s.last_ts ? <span className="text-faint"> · {timeAgo(s.last_ts)}</span> : null}
          </span>
        </li>
      ))}
    </ul>
  )
}

// ---- overview ----

export function FamilyPage() {
  const { days, setDays } = useDays()
  const { data, isLoading } = useQuery({
    queryKey: ['family', 'overview', days],
    queryFn: () => api.get<FamilyOverview>(`/family/overview?days=${days}`),
    refetchInterval: 30_000,
    placeholderData: (p) => p,
  })
  const t = data?.totals
  return (
    <>
      <PageHeader
        title="Family DNS"
        description="Who tried to reach adult and other blocked sites, from which device, and when. Repeated lookups within a few minutes count as one attempt."
        actions={<RangePicker days={days} setDays={setDays} />}
      >
        <FamilyNav />
      </PageHeader>
      <div className="space-y-6 p-6">
        {isLoading || !data ? (
          <Skeleton className="h-40" />
        ) : (
          <>
            <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
              <StatCard label="Adult-site attempts" value={fmtNum(t!.adult)} sub={`${fmtNum(t!.adult_hits)} lookups`} tone={t!.adult ? 'warn' : 'ok'} icon={<Heart className="h-4 w-4" />} />
              <StatCard
                label="Not blocked"
                value={fmtNum(t!.allowed)}
                sub={t!.allowed ? 'Adult sites that resolved: check filtering' : 'Every adult attempt was blocked'}
                tone={t!.allowed ? 'danger' : 'ok'}
                icon={<AlertTriangle className="h-4 w-4" />}
              />
              <StatCard label="During bedtime" value={fmtNum(t!.late_night)} sub={data.bedtime ? `Bedtime ${data.bedtime}` : 'No bedtime set'} icon={<Moon className="h-4 w-4" />} />
              <StatCard label="Safe search enforced" value={fmtNum(t!.safe_search)} sub="Searches switched to safe results" icon={<ShieldCheck className="h-4 w-4" />} />
            </div>

            <Card>
              <CardHeader title="Bypass & coverage signals" description="Hints that filtering may be avoided or incomplete." />
              <div className="px-5 pb-5">
                <SignalList signals={data.signals} />
              </div>
            </Card>

            {data.people.length === 0 ? (
              <Card>
                <EmptyState
                  icon={<UserRound className="h-6 w-6" />}
                  title="Add the people in your household"
                  description="Name each person and assign their devices, then GuardTower shows attempts per person instead of per IP address."
                  action={
                    <Link to="/family/devices">
                      <Button variant="primary">
                        <Plus className="h-4 w-4" /> Add people & devices
                      </Button>
                    </Link>
                  }
                />
              </Card>
            ) : (
              <div className="grid gap-4 md:grid-cols-2 2xl:grid-cols-3">
                {data.people.map((p) => (
                  <PersonCard key={p.person_id} p={p} days={days} />
                ))}
                {(data.unassigned.adult > 0 || data.unassigned.categories.length > 0) && <PersonCard p={data.unassigned} days={days} unassigned />}
              </div>
            )}
            {data.people.length === 0 && data.unassigned.categories.length > 0 && <PersonCard p={data.unassigned} days={days} unassigned />}
          </>
        )}
      </div>
    </>
  )
}

function PersonCard({ p, days, unassigned }: { p: FamilyOverview['unassigned']; days: string; unassigned?: boolean }) {
  const to = `/family/person/${unassigned ? 'unassigned' : p.person_id}?days=${days}`
  return (
    <Link to={to} className="block">
      <Card className="h-full p-5 transition-colors hover:border-border-strong">
        <div className="flex items-center gap-3">
          {unassigned ? (
            <span className="inline-flex h-9 w-9 items-center justify-center rounded-full bg-subtle text-muted">
              <Smartphone className="h-4 w-4" />
            </span>
          ) : (
            <Avatar name={p.name} emoji={p.emoji} color={p.color} />
          )}
          <div className="min-w-0 flex-1">
            <div className="font-semibold">{p.name}</div>
            <div className="text-xs text-muted">
              {p.devices} device{p.devices === 1 ? '' : 's'}
              {p.last_attempt ? ` · last attempt ${timeAgo(p.last_attempt)}` : ''}
            </div>
          </div>
          <div className="text-right">
            <div className={cn('text-2xl font-semibold tabular-nums', p.adult ? 'text-warn' : 'text-ok')}>{p.adult}</div>
            <div className="text-[11px] text-faint">adult attempts</div>
          </div>
        </div>
        <div className="mt-4 flex flex-wrap gap-1.5">
          {p.late_night > 0 && (
            <Badge tone="warn">
              <Moon className="mr-1 inline h-3 w-3" />
              {p.late_night} during bedtime
            </Badge>
          )}
          {p.categories
            .filter((c) => c.category !== 'safesearch')
            .map((c) => (
              <span key={c.category} className="inline-flex items-center gap-1 rounded-full border border-border px-2 py-0.5 text-xs">
                <span className="h-2 w-2 rounded-full" style={{ background: catColor(c.category) }} />
                {categoryLabel[c.category]} {c.attempts}
                {c.allowed > 0 && c.category === 'adult' && <span className="text-danger">({c.allowed} not blocked)</span>}
              </span>
            ))}
          {p.categories.length === 0 && <span className="text-sm text-muted">Nothing notable in this period.</span>}
        </div>
      </Card>
    </Link>
  )
}

// ---- person ----

export function FamilyPersonPage() {
  const { id } = useParams()
  const personParam = id === 'unassigned' ? -1 : Number(id)
  const { days, setDays, params, setParams } = useDays()
  const cats = (params.get('category') ?? 'adult').split(',').filter(Boolean) as FamilyCategory[]
  const { data } = useQuery({
    queryKey: ['family', 'detail', personParam, days, cats.join(',')],
    queryFn: () => api.get<FamilyDetail>(`/family/detail?person=${personParam}&days=${days}${cats.map((c) => `&category=${c}`).join('')}`),
    refetchInterval: 30_000,
    placeholderData: (p) => p,
  })
  const toggleCat = (c: FamilyCategory) => {
    const next = cats.includes(c) ? cats.filter((x) => x !== c) : [...cats, c]
    const n = new URLSearchParams(params)
    n.set('category', (next.length ? next : ['adult']).join(','))
    setParams(n, { replace: true })
  }
  const series = cats.map((c) => ({
    name: categoryLabel[c],
    colorIndex: categorySlot[c],
    data: (data?.daily ?? []).map((d) => [d.day, d.count[c] ?? 0] as [number, number]),
  }))
  return (
    <>
      <PageHeader
        title={data?.name ?? 'Family DNS'}
        description={personParam < 0 ? 'Devices not yet assigned to a person.' : `${data?.devices.length ?? 0} device(s) · attempts are lookups of a site grouped into sessions.`}
        actions={<RangePicker days={days} setDays={setDays} />}
      >
        <FamilyNav />
      </PageHeader>
      <div className="space-y-6 p-6">
        <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Categories">
          {shownCategories.map((c) => (
            <button
              key={c}
              onClick={() => toggleCat(c)}
              aria-pressed={cats.includes(c)}
              className={cn('inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs', cats.includes(c) ? 'border-ring bg-accent-soft font-medium' : 'border-border hover:bg-hover')}
            >
              <span className="h-2 w-2 rounded-full" style={{ background: catColor(c) }} />
              {categoryLabel[c]}
            </button>
          ))}
        </div>
        {!data ? (
          <Skeleton className="h-64" />
        ) : (
          <>
            {data.signals.length > 0 && (
              <Card>
                <CardHeader title="Signals" />
                <div className="px-5 pb-5">
                  <SignalList signals={data.signals} />
                </div>
              </Card>
            )}
            <div className="grid gap-6 xl:grid-cols-3">
              <Card className="xl:col-span-2">
                <CardHeader title="When attempts happen" description={`Attempts by day of week and hour${data.late_night ? ` · ${data.late_night} during bedtime` : ''}`} />
                <div className="px-3 pb-3">
                  <HourHeatmap grid={data.heatmap} />
                </div>
              </Card>
              <Card>
                <CardHeader title="Devices" description="Attempts per device in this period" />
                <div className="px-3 pb-4">
                  <BarList
                    items={data.devices.map((d) => ({ key: d.device, label: d.label ? `${d.label} (${d.device})` : d.device, value: d.attempts, sub: d.last_seen ? timeAgo(d.last_seen) : 'no attempts' }))}
                    colorFor={() => 'var(--series-1)'}
                    emptyText="No devices"
                  />
                </div>
              </Card>
            </div>
            <div className="grid gap-6 xl:grid-cols-3">
              <Card className="xl:col-span-2">
                <CardHeader title="Trend" description="Attempts per day" />
                <div className="px-3 pb-3">
                  <TimeSeriesChart series={series} bucketMs={86400_000} height={220} />
                </div>
              </Card>
              <Card>
                <CardHeader title="Most attempted sites" description={data.top.some((t) => t.masked) ? 'Masked; reveal is recorded in the audit log' : undefined} />
                <ul className="space-y-1 px-5 pb-5 text-sm">
                  {data.top.length === 0 && <li className="text-muted">No attempts in this period.</li>}
                  {data.top.map((t) => (
                    <li key={t.category + t.domain} className="flex items-center gap-2">
                      <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: catColor(t.category) }} aria-label={categoryLabel[t.category]} />
                      <SiteName eventId={t.event_id} domain={t.domain} masked={t.masked} />
                      {t.allowed > 0 && <Badge tone="danger">{t.allowed === t.attempts ? 'not blocked' : `${t.allowed} not blocked`}</Badge>}
                      <span className="ml-auto tabular-nums text-muted">{t.attempts}×</span>
                    </li>
                  ))}
                </ul>
              </Card>
            </div>
            <EventsTable events={data.events} />
          </>
        )}
      </div>
    </>
  )
}

function EventsTable({ events }: { events: FamilyEvent[] }) {
  return (
    <Card>
      <CardHeader title="Attempts" description="Newest first. One row = one site visited in one sitting." />
      {events.length === 0 ? (
        <p className="px-5 pb-5 text-sm text-muted">No attempts in this period.</p>
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>When</Th>
              <Th>Device</Th>
              <Th>Category</Th>
              <Th>Site</Th>
              <Th className="text-right">Lookups</Th>
              <Th>Result</Th>
            </tr>
          </thead>
          <tbody>
            {events.map((e) => (
              <tr key={e.id}>
                <Td className="whitespace-nowrap text-sm" title={fmtDateTime(e.first_ts)}>
                  {fmtDateTime(e.first_ts)}
                  {e.late_night && <Moon className="ml-1.5 inline h-3.5 w-3.5 text-warn" aria-label="during bedtime" />}
                </Td>
                <Td className="text-sm">
                  {e.device_name || e.device}
                  {e.device_name && <span className="ml-1 font-mono text-xs text-faint">{e.device}</span>}
                </Td>
                <Td>
                  <span className="inline-flex items-center gap-1.5 text-sm">
                    <span className="h-2 w-2 rounded-full" style={{ background: catColor(e.category) }} />
                    {categoryLabel[e.category]}
                  </span>
                </Td>
                <Td className="text-sm">
                  <SiteName eventId={e.id} domain={e.domain} masked={e.masked} />
                </Td>
                <Td className="text-right tabular-nums">{e.count}</Td>
                <Td>{e.blocked ? <Badge tone="ok">blocked</Badge> : <Badge tone="danger">not blocked</Badge>}</Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </Card>
  )
}

// ---- people & devices ----

export function FamilyDevicesPage() {
  const qc = useQueryClient()
  const { data: people } = useQuery({ queryKey: ['family', 'people'], queryFn: () => api.get<Person[]>('/family/people') })
  const { data: clients } = useQuery({ queryKey: ['family', 'clients'], queryFn: () => api.get<FamilyClient[]>('/family/clients?days=7'), refetchInterval: 60_000 })
  const [edit, setEdit] = useState<{ open: boolean; person?: Person }>({ open: false })
  const [del, setDel] = useState<Person | null>(null)
  const invalidate = () => void qc.invalidateQueries({ queryKey: ['family'] })
  const assign = useMutation({
    mutationFn: (b: { device: string; label: string; person_id: number }) => api.put('/family/devices', b),
    onSuccess: () => {
      invalidate()
      toast.success('Device updated')
    },
    onError: (e) => toast.error((e as Error).message),
  })
  const remove = useMutation({
    mutationFn: (id: number) => api.del(`/family/people/${id}`),
    onSuccess: () => {
      setDel(null)
      invalidate()
    },
    onError: (e) => toast.error((e as Error).message),
  })
  const owner = new Map((people ?? []).map((p) => [p.id, p]))
  return (
    <>
      <PageHeader title="Family DNS" description="Name the people in your household and assign their devices." actions={<Button variant="primary" onClick={() => setEdit({ open: true })}><Plus className="h-4 w-4" /> Add person</Button>}>
        <FamilyNav />
      </PageHeader>
      <div className="space-y-6 p-6">
        <Card>
          <CardHeader title="People" />
          {!people ? (
            <Skeleton className="mx-5 mb-5 h-16" />
          ) : people.length === 0 ? (
            <p className="px-5 pb-5 text-sm text-muted">No people yet. Add someone, then assign devices below.</p>
          ) : (
            <ul className="divide-y divide-border">
              {people.map((p) => (
                <li key={p.id} className="flex flex-wrap items-center gap-3 px-5 py-3">
                  <Avatar name={p.name} emoji={p.emoji} color={p.color} />
                  <div className="min-w-0 flex-1">
                    <div className="font-medium">{p.name}</div>
                    <div className="mt-1 flex flex-wrap gap-1">
                      {p.devices.length === 0 && <span className="text-xs text-faint">No devices yet</span>}
                      {p.devices.map((d) => (
                        <span key={d.id} className="inline-flex items-center gap-1 rounded-full border border-border px-2 py-0.5 text-xs">
                          <Smartphone className="h-3 w-3 text-muted" />
                          {d.label || d.device}
                          {d.label && <span className="font-mono text-faint">{d.device}</span>}
                          <button onClick={() => assign.mutate({ device: d.device, label: '', person_id: 0 })} className="text-faint hover:text-danger" aria-label={`Unassign ${d.label || d.device}`}>
                            ×
                          </button>
                        </span>
                      ))}
                    </div>
                  </div>
                  <Button size="sm" variant="ghost" onClick={() => setEdit({ open: true, person: p })}>
                    Edit
                  </Button>
                  <Button size="icon" variant="ghost" onClick={() => setDel(p)} aria-label={`Remove ${p.name}`}>
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </Card>
        <Card>
          <CardHeader title="Devices seen this week" description="Every client that used your DNS servers. Devices with family attempts that nobody owns are listed first." />
          {!clients ? (
            <Skeleton className="mx-5 mb-5 h-24" />
          ) : (
            <Table>
              <thead>
                <tr>
                  <Th>Device</Th>
                  <Th className="text-right">Lookups</Th>
                  <Th className="text-right">Attempts</Th>
                  <Th>Last seen</Th>
                  <Th>Belongs to</Th>
                </tr>
              </thead>
              <tbody>
                {clients.map((c) => (
                  <ClientRow key={c.device} c={c} people={people ?? []} owner={c.person_id ? owner.get(c.person_id) : undefined} onAssign={(pid, label) => assign.mutate({ device: c.device, label, person_id: pid })} />
                ))}
              </tbody>
            </Table>
          )}
        </Card>
      </div>
      <PersonDialog open={edit.open} person={edit.person} onOpenChange={(v) => setEdit({ open: v })} onSaved={invalidate} />
      <ConfirmDialog
        open={!!del}
        onOpenChange={(v) => !v && setDel(null)}
        title={`Remove ${del?.name}?`}
        description="Their devices become unassigned. Recorded attempts are kept."
        confirmLabel="Remove"
        danger
        loading={remove.isPending}
        onConfirm={() => del && remove.mutate(del.id)}
      />
    </>
  )
}

function ClientRow({ c, people, owner, onAssign }: { c: FamilyClient; people: Person[]; owner?: Person; onAssign: (personId: number, label: string) => void }) {
  return (
    <tr>
      <Td>
        <div className="text-sm font-medium">{c.name || c.device}</div>
        {c.name && <div className="font-mono text-xs text-faint">{c.device}</div>}
      </Td>
      <Td className="text-right tabular-nums">{fmtNum(c.queries)}</Td>
      <Td className="text-right tabular-nums">{c.events ? <span className={cn(!owner && 'font-semibold text-warn')}>{c.events}</span> : '—'}</Td>
      <Td className="text-xs text-muted">{timeAgo(c.last_seen)}</Td>
      <Td>
        <Select aria-label={`Owner of ${c.name || c.device}`} value={owner?.id ?? 0} onChange={(e) => onAssign(Number(e.target.value), c.name)} className="w-44">
          <option value={0}>Nobody</option>
          {people.map((p) => (
            <option key={p.id} value={p.id}>
              {p.emoji ? `${p.emoji} ` : ''}
              {p.name}
            </option>
          ))}
        </Select>
      </Td>
    </tr>
  )
}

function PersonDialog({ open, person, onOpenChange, onSaved }: { open: boolean; person?: Person; onOpenChange: (v: boolean) => void; onSaved: () => void }) {
  const [name, setName] = useState(person?.name ?? '')
  const [emoji, setEmoji] = useState(person?.emoji ?? '')
  const save = useMutation({
    mutationFn: () => (person ? api.put(`/family/people/${person.id}`, { name, emoji, color: person.color }) : api.post('/family/people', { name, emoji, color: '' })),
    onSuccess: () => {
      toast.success(person ? 'Saved' : `${name} added`)
      onOpenChange(false)
      onSaved()
    },
    onError: (e) => toast.error((e as Error).message),
  })
  return (
    <Dialog
      key={person?.id ?? 'new'}
      open={open}
      onOpenChange={onOpenChange}
      title={person ? `Edit ${person.name}` : 'Add a person'}
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()} disabled={!name.trim()}>
            Save
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={60} autoFocus />
        </Field>
        <Field label="Emoji" hint="Optional, shown as their avatar, e.g. 🧒">
          <Input value={emoji} onChange={(e) => setEmoji(e.target.value)} maxLength={8} className="w-24" />
        </Field>
      </div>
    </Dialog>
  )
}

// ---- settings ----

export function FamilySettingsPage() {
  const { can } = useSession()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { data: settings } = useQuery({ queryKey: ['family', 'settings'], queryFn: () => api.get<Record<string, string>>('/family/settings') })
  const { data: lists } = useQuery({ queryKey: ['family', 'lists'], queryFn: () => api.get<{ lists: FamilyList[]; categories: FamilyCategory[] }>('/family/lists') })
  const { data: ov } = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview') })
  const [draft, setDraft] = useState<Record<string, string> | null>(null)
  const cur = draft ?? settings ?? {}
  const set = (k: string, v: string) => setDraft({ ...cur, [k]: v })
  const save = useMutation({
    mutationFn: () => api.put<Record<string, string>>('/family/settings', cur),
    onSuccess: (d) => {
      qc.setQueryData(['family', 'settings'], d)
      setDraft(null)
      toast.success('Family settings saved')
    },
    onError: (e) => toast.error((e as Error).message),
  })
  const setCat = useMutation({
    mutationFn: (b: { url: string; category: string }) => api.put('/family/lists', b),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['family'] }),
    onError: (e) => toast.error((e as Error).message),
  })
  const [group, setGroup] = useState(0)
  const [opts, setOpts] = useState({ parental: true, safe_search: true, adult_list: true, block_bypass: true })
  const profile = useMutation({
    mutationFn: () => api.post<{ sections: string[] }>(`/family/profile/${group}`, opts),
    onSuccess: (r) => {
      toast.success('Family profile added to the group', { description: `Changed: ${r.sections.join(', ')}. Review and apply it to roll it out.` })
      void navigate(`/changes?target=group:${group}`)
    },
    onError: (e) => toast.error((e as Error).message),
  })
  return (
    <>
      <PageHeader title="Family DNS" description="How attempts are recorded, shown and reported.">
        <FamilyNav />
      </PageHeader>
      <div className="grid gap-6 p-6 xl:grid-cols-2">
        <Card>
          <CardHeader title="Display & alerts" />
          <form className="space-y-4 px-5 pb-5" onSubmit={(e) => (e.preventDefault(), save.mutate())}>
            <Field label="Bedtime" hint="Attempts in this window are flagged, e.g. 22:00-06:00. Leave empty for none.">
              <Input value={cur['family.bedtime'] ?? ''} onChange={(e) => set('family.bedtime', e.target.value)} placeholder="22:00-06:00" className="w-40 font-mono" />
            </Field>
            <label className="flex items-center gap-2 text-sm">
              <Switch checked={cur['family.mask_domains'] !== 'false'} onCheckedChange={(v) => set('family.mask_domains', v ? 'true' : 'false')} />
              Mask adult and gambling domains (p***hub.com) until revealed
            </label>
            <Field label="Push alerts" hint="Sent to notification targets marked for family alerts (Admin → Settings).">
              <Select value={cur['family.alerts'] ?? 'adult'} onChange={(e) => set('family.alerts', e.target.value)} className="w-72">
                <option value="off">Off</option>
                <option value="adult">Adult-site attempts</option>
                <option value="all">Adult, gambling, VPN & private DNS</option>
              </Select>
            </Field>
            <Field label="Quiet hours" hint="No push alerts in this window (attempts are still recorded).">
              <Input value={cur['family.quiet_hours'] ?? ''} onChange={(e) => set('family.quiet_hours', e.target.value)} placeholder="none" className="w-40 font-mono" />
            </Field>
            <Field label="Weekly digest" hint='"off", or a day and time such as "sun 18:00".'>
              <Input value={cur['family.digest'] ?? ''} onChange={(e) => set('family.digest', e.target.value)} className="w-40 font-mono" />
            </Field>
            <Field label="Keep family history (days)" hint="7–3650. Independent of the raw query log.">
              <Input type="number" min={7} max={3650} value={cur['retention.family_days'] ?? ''} onChange={(e) => set('retention.family_days', e.target.value)} className="w-32" />
            </Field>
            <Button type="submit" variant="primary" disabled={!draft} loading={save.isPending}>
              Save
            </Button>
          </form>
        </Card>
        {can('admin') && (
          <Card>
            <CardHeader title="Family profile" description="Turn on family protection for a whole group. GuardTower adds the settings to the group; you review and apply them like any other change." />
            <div className="space-y-4 px-5 pb-5">
              <Field label="Group">
                <Select value={group} onChange={(e) => setGroup(Number(e.target.value))} className="w-64">
                  <option value={0}>Choose a group…</option>
                  {ov?.groups.map((g) => (
                    <option key={g.id} value={g.id}>
                      {g.name}
                    </option>
                  ))}
                </Select>
              </Field>
              {(
                [
                  ['parental', 'Parental control (AdGuard’s adult-content filter)'],
                  ['safe_search', 'Enforce safe search on Google, Bing, DuckDuckGo, YouTube and others'],
                  ['adult_list', 'Add the HaGeZi NSFW block list'],
                  ['block_bypass', 'Block private-DNS resolvers, popular VPNs and Tor, and switch off Firefox’s built-in DNS-over-HTTPS'],
                ] as const
              ).map(([k, label]) => (
                <label key={k} className="flex items-start gap-2 text-sm">
                  <Switch checked={opts[k]} onCheckedChange={(v) => setOpts({ ...opts, [k]: v })} />
                  <span>{label}</span>
                </label>
              ))}
              <Button variant="primary" disabled={!group} loading={profile.isPending} onClick={() => profile.mutate()}>
                Add to group and review
              </Button>
            </div>
          </Card>
        )}
        <Card className="xl:col-span-2">
          <CardHeader title="Filter list categories" description="Which category a block from each list counts as. Well-known adult lists are recognised automatically; change any of them here." />
          {!lists ? (
            <Skeleton className="mx-5 mb-5 h-16" />
          ) : lists.lists.length === 0 ? (
            <p className="px-5 pb-5 text-sm text-muted">No filter lists seen yet. GuardTower reads them from your instances every hour.</p>
          ) : (
            <Table>
              <thead>
                <tr>
                  <Th>List</Th>
                  <Th>Category</Th>
                  <Th>Instances</Th>
                </tr>
              </thead>
              <tbody>
                {lists.lists.map((l) => (
                  <tr key={l.url}>
                    <Td>
                      <div className="text-sm font-medium">{l.name || l.url}</div>
                      <div className="max-w-xl truncate font-mono text-xs text-faint">{l.url}</div>
                    </Td>
                    <Td>
                      <Select aria-label={`Category of ${l.name || l.url}`} value={l.explicit ? l.category : ''} onChange={(e) => setCat.mutate({ url: l.url, category: e.target.value })} className="w-52">
                        <option value="">Automatic ({categoryLabel[l.category]})</option>
                        {lists.categories.map((c) => (
                          <option key={c} value={c}>
                            {categoryLabel[c]}
                          </option>
                        ))}
                      </Select>
                    </Td>
                    <Td className="text-sm text-muted">{l.instances.length}</Td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Card>
      </div>
    </>
  )
}
