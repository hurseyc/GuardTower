import { useState } from 'react'
import { Link, useParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ArrowLeft, ExternalLink, Pencil, RefreshCw, ShieldCheck, ShieldOff } from 'lucide-react'
import { api, type Instance, type Overview } from '@/lib/api'
import { useSession } from '@/lib/session'
import { colorFor, fmtDateTime, timeAgo } from '@/lib/utils'
import { PageHeader } from '@/components/AppShell'
import { Badge, Button, Card, CardHeader, Dialog, Select, Skeleton, StatusDot } from '@/components/ui'
import { InstanceDialog } from '@/components/InstanceDialog'
import { SyncCard } from '@/components/SyncCard'

const runtimeLabel: Record<string, string> = {
  unknown: 'Not specified',
  lxc: 'Proxmox LXC',
  docker: 'Docker container',
  vm: 'Virtual machine',
  baremetal: 'Bare metal / service',
  router: 'Router',
  homeassistant: 'Home Assistant add-on',
}

export function InstanceDetailPage() {
  const id = Number(useParams().id)
  const { can } = useSession()
  const qc = useQueryClient()
  const [edit, setEdit] = useState(false)
  const [pauseOpen, setPauseOpen] = useState(false)
  const [pauseMinutes, setPauseMinutes] = useState(10)
  const { data: inst, isLoading } = useQuery({ queryKey: ['instance', id], queryFn: () => api.get<Instance>(`/instances/${id}`), refetchInterval: 15_000 })
  const { data: ov } = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview') })

  const refresh = useMutation({
    mutationFn: () => api.post<Instance>(`/instances/${id}/refresh`),
    onSuccess: (i) => qc.setQueryData(['instance', id], i),
  })
  const protection = useMutation({
    mutationFn: (b: { enabled: boolean; duration_minutes?: number }) => api.post(`/instances/${id}/protection`, b),
    onSuccess: (_, b) => {
      toast.success(b.enabled ? 'Protection enabled' : b.duration_minutes ? `Protection paused for ${b.duration_minutes} min` : 'Protection disabled')
      setPauseOpen(false)
      setTimeout(() => refresh.mutate(), 400)
    },
    onError: (e) => toast.error((e as Error).message),
  })

  if (isLoading || !inst) {
    return (
      <div className="space-y-4 p-6">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40" />
      </div>
    )
  }
  const st = inst.status
  const group = ov?.groups.find((g) => g.id === inst.group_id)
  const host = ov?.hosts.find((h) => h.id === inst.host_id)

  return (
    <>
      <PageHeader
        title={inst.name}
        description={
          <span className="inline-flex items-center gap-2">
            <StatusDot online={st?.online} />
            {st?.online ? `Online · AdGuard Home ${st.version}` : `Offline · last seen ${timeAgo(st?.last_seen_at)}`}
          </span>
        }
        actions={
          <>
            <Link to="/instances">
              <Button variant="ghost">
                <ArrowLeft className="h-4 w-4" /> All instances
              </Button>
            </Link>
            <a href={inst.url} target="_blank" rel="noreferrer noopener">
              <Button variant="outline">
                <ExternalLink className="h-4 w-4" /> Open AdGuard Home
              </Button>
            </a>
            <Button variant="outline" onClick={() => refresh.mutate()} loading={refresh.isPending}>
              <RefreshCw className="h-4 w-4" /> Check now
            </Button>
            {can('admin') && (
              <Button variant="primary" onClick={() => setEdit(true)}>
                <Pencil className="h-4 w-4" /> Edit
              </Button>
            )}
          </>
        }
      />
      <div className="grid gap-6 p-6 xl:grid-cols-3">
        <Card className="xl:col-span-2">
          <CardHeader title="Connection" />
          <dl className="grid gap-x-6 gap-y-4 px-5 pb-5 text-sm sm:grid-cols-2">
            <Item label="URL">
              <span className="font-mono">{inst.url}</span>
            </Item>
            <Item label="Username">{inst.username || <span className="text-faint">none</span>}</Item>
            <Item label="TLS">
              {inst.url.startsWith('https') ? (inst.tls_skip_verify ? <Badge tone="warn">verification skipped</Badge> : inst.ca_pem ? <Badge tone="ok">custom CA</Badge> : <Badge tone="ok">verified</Badge>) : <Badge>plain HTTP</Badge>}
            </Item>
            <Item label="DNS listeners">
              <span className="font-mono text-xs">{st?.dns_addresses?.length ? st.dns_addresses.join(', ') : '—'}</span>
            </Item>
            <Item label="API latency">{st?.online ? `${st.latency_ms} ms` : '—'}</Item>
            <Item label="Last checked">{st?.checked_at ? fmtDateTime(st.checked_at) : 'never'}</Item>
            {st && !st.online && st.last_error && (
              <div className="sm:col-span-2">
                <div className="rounded-lg bg-danger-soft p-3 text-sm">{st.last_error}</div>
              </div>
            )}
          </dl>
        </Card>
        <Card>
          <CardHeader title="Protection" description="Filtering on this instance only" />
          <div className="px-5 pb-5">
            <div className="flex items-center gap-3 rounded-xl border border-border bg-bg p-4">
              {st?.protection_enabled ? <ShieldCheck className="h-8 w-8 text-ok" /> : <ShieldOff className="h-8 w-8 text-warn" />}
              <div>
                <div className="font-medium">{st?.protection_enabled ? 'Protection is on' : 'Protection is off'}</div>
                <div className="text-xs text-faint">Applies immediately; not a fleet-wide change.</div>
              </div>
            </div>
            {can('operator') && st?.online && (
              <div className="mt-3 flex gap-2">
                {st.protection_enabled ? (
                  <Button variant="outline" className="flex-1 justify-center" onClick={() => setPauseOpen(true)}>
                    Pause / disable
                  </Button>
                ) : (
                  <Button variant="primary" className="flex-1 justify-center" loading={protection.isPending} onClick={() => protection.mutate({ enabled: true })}>
                    Enable protection
                  </Button>
                )}
              </div>
            )}
          </div>
        </Card>
        <Card className="xl:col-span-3">
          <CardHeader title="Placement" description="Where this instance sits in your fleet" />
          <dl className="grid gap-x-6 gap-y-4 px-5 pb-5 text-sm sm:grid-cols-4">
            <Item label="Settings group">
              {group ? (
                <span className="inline-flex items-center gap-1.5">
                  <span className="h-2 w-2 rounded-full" style={{ background: colorFor(group.name, group.color) }} />
                  {group.name}
                </span>
              ) : (
                'Ungrouped'
              )}
            </Item>
            <Item label="Host">{host?.name ?? 'Unassigned'}</Item>
            <Item label="Install type">{runtimeLabel[inst.runtime] ?? inst.runtime}</Item>
            <Item label="Tags">
              <span className="flex flex-wrap gap-1">{inst.tags.length ? inst.tags.map((t) => <Badge key={t}>#{t}</Badge>) : '—'}</span>
            </Item>
          </dl>
        </Card>
        <SyncCard instanceId={inst.id} />
      </div>
      <InstanceDialog open={edit} onOpenChange={setEdit} instance={inst} groups={ov?.groups ?? []} hosts={ov?.hosts ?? []} />
      <Dialog
        open={pauseOpen}
        onOpenChange={setPauseOpen}
        title={`Pause protection on ${inst.name}`}
        description="DNS keeps resolving, but nothing is filtered on this instance."
        footer={
          <>
            <Button variant="ghost" onClick={() => setPauseOpen(false)}>
              Cancel
            </Button>
            <Button variant="danger" loading={protection.isPending} onClick={() => protection.mutate({ enabled: false, duration_minutes: pauseMinutes })}>
              {pauseMinutes ? `Pause for ${pauseMinutes} min` : 'Disable until re-enabled'}
            </Button>
          </>
        }
      >
        <Select value={pauseMinutes} onChange={(e) => setPauseMinutes(Number(e.target.value))}>
          <option value={1}>1 minute</option>
          <option value={10}>10 minutes</option>
          <option value={60}>1 hour</option>
          <option value={480}>8 hours</option>
          <option value={0}>Until I turn it back on</option>
        </Select>
      </Dialog>
    </>
  )
}

function Item({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <dt className="text-xs text-faint">{label}</dt>
      <dd className="mt-1">{children}</dd>
    </div>
  )
}
