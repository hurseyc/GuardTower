import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Pencil, Plus, RefreshCw, ShieldCheck, Trash2 } from 'lucide-react'
import { api, type Instance, type Overview, type SyncSummary } from '@/lib/api'
import { useSession } from '@/lib/sessionContext'
import { colorFor, timeAgo } from '@/lib/utils'
import { RiskMarker } from '@/components/Resilience'
import { useResilience } from '@/lib/resilience'
import { PageHeader } from '@/components/AppShell'
import { Badge, Button, Card, ConfirmDialog, EmptyState, Input, Select, Skeleton, StatusDot, Table, Td, Th, Tip } from '@/components/ui'
import { InstanceDialog } from '@/components/InstanceDialog'
import { useOnChange } from '@/lib/useOnChange'

export function InstancesPage() {
  const { data: res } = useResilience()
  const { can } = useSession()
  const qc = useQueryClient()
  const nav = useNavigate()
  const [params, setParams] = useSearchParams()
  const { data, isLoading } = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview') })
  const { data: sync } = useQuery({ queryKey: ['sync'], queryFn: () => api.get<SyncSummary[]>('/sync'), refetchInterval: 60_000 })
  const syncById = new Map((sync ?? []).map((s) => [s.instance_id, s]))
  const [dialog, setDialog] = useState<{ open: boolean; instance?: Instance }>({ open: false })
  const [toDelete, setToDelete] = useState<Instance | null>(null)
  const [q, setQ] = useState('')
  const [groupFilter, setGroupFilter] = useState('all')
  const [statusFilter, setStatusFilter] = useState('all')

  // ?add=1 (from the command palette or Overview) opens the dialog once.
  const addRequested = params.get('add') === '1' && can('admin')
  useOnChange(addRequested, (v) => {
    if (v) setDialog({ open: true })
  })
  useEffect(() => {
    if (!addRequested) return
    params.delete('add')
    setParams(params, { replace: true })
  }, [addRequested, params, setParams])

  const del = useMutation({
    mutationFn: (id: number) => api.del(`/instances/${id}`),
    onSuccess: () => {
      toast.success('Instance removed')
      setToDelete(null)
      void qc.invalidateQueries({ queryKey: ['overview'] })
    },
    onError: (e) => toast.error((e as Error).message),
  })
  const refresh = useMutation({
    mutationFn: (id: number) => api.post(`/instances/${id}/refresh`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['overview'] }),
  })

  const groups = data?.groups ?? []
  const hosts = data?.hosts ?? []
  const groupById = new Map(groups.map((g) => [g.id, g]))
  const hostById = new Map(hosts.map((h) => [h.id, h]))
  const rows = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return (data?.instances ?? []).filter((i) => {
      if (groupFilter !== 'all' && String(i.group_id ?? 0) !== groupFilter) return false
      if (statusFilter === 'online' && !i.status?.online) return false
      if (statusFilter === 'offline' && i.status?.online) return false
      if (!needle) return true
      return [i.name, i.url, ...i.tags, i.status?.version ?? ''].some((s) => s.toLowerCase().includes(needle))
    })
  }, [data, q, groupFilter, statusFilter])

  return (
    <>
      <PageHeader
        title="Instances"
        description="Every AdGuard Home server GuardTower connects to."
        actions={
          can('admin') && (
            <Button variant="primary" onClick={() => setDialog({ open: true })}>
              <Plus className="h-4 w-4" /> Add instance
            </Button>
          )
        }
      />
      <div className="p-6">
        <Card>
          <div className="flex flex-wrap items-center gap-2 border-b border-border p-3">
            <Input placeholder="Filter by name, URL, tag or version…" value={q} onChange={(e) => setQ(e.target.value)} className="max-w-xs" />
            <Select value={groupFilter} onChange={(e) => setGroupFilter(e.target.value)} className="w-44" aria-label="Filter by group">
              <option value="all">All groups</option>
              <option value="0">Ungrouped</option>
              {groups.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name}
                </option>
              ))}
            </Select>
            <Select value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)} className="w-36" aria-label="Filter by status">
              <option value="all">Any status</option>
              <option value="online">Online</option>
              <option value="offline">Offline</option>
            </Select>
            <span className="ml-auto text-xs text-faint">
              {rows.length} of {data?.instances.length ?? 0}
            </span>
          </div>
          {isLoading ? (
            <div className="space-y-2 p-4">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className="h-10" />
              ))}
            </div>
          ) : (data?.instances.length ?? 0) === 0 ? (
            <EmptyState
              icon={<ShieldCheck className="h-6 w-6" />}
              title="No instances yet"
              description="Add the AdGuard Home servers you want to manage. You'll need each one's web address and admin credentials."
              action={
                can('admin') && (
                  <Button variant="primary" onClick={() => setDialog({ open: true })}>
                    <Plus className="h-4 w-4" /> Add instance
                  </Button>
                )
              }
            />
          ) : (
            <Table>
              <thead>
                <tr>
                  <Th>Instance</Th>
                  <Th>Group</Th>
                  <Th>Host</Th>
                  <Th>Version</Th>
                  <Th>Status</Th>
                  <Th>Settings</Th>
                  <Th className="text-right">Actions</Th>
                </tr>
              </thead>
              <tbody>
                {rows.map((i) => {
                  const g = i.group_id ? groupById.get(i.group_id) : undefined
                  const h = i.host_id ? hostById.get(i.host_id) : undefined
                  return (
                    <tr key={i.id} className="cursor-pointer hover:bg-hover/50" onClick={() => nav(`/instances/${i.id}`)}>
                      <Td>
                        <div className="flex items-center gap-2.5">
                          <StatusDot online={i.status?.online} />
                          <div className="min-w-0">
                            <div className="flex items-center gap-1.5 font-medium">
                              <Link to={`/instances/${i.id}`} onClick={(e) => e.stopPropagation()} className="hover:underline">
                                {i.name}
                              </Link>
                              {i.is_canary && <Badge tone="info">canary</Badge>}
                              {!i.enabled && <Badge>disabled</Badge>}
                            </div>
                            <div className="truncate font-mono text-xs text-faint">{i.url}</div>
                          </div>
                        </div>
                      </Td>
                      <Td>
                        {g ? (
                          <span className="inline-flex items-center gap-1.5 text-sm">
                            <span className="h-2 w-2 rounded-full" style={{ background: colorFor(g.name, g.color) }} />
                            {g.name}
                            <RiskMarker findings={(res?.groups.find((x) => x.group_id === g.id)?.findings ?? []).filter((f) => f.instance_ids.includes(i.id))} />
                          </span>
                        ) : (
                          <span className="text-faint">—</span>
                        )}
                      </Td>
                      <Td className="text-sm">{h ? h.name : <span className="text-faint">—</span>}</Td>
                      <Td className="font-mono text-xs">{i.status?.version || '—'}</Td>
                      <Td>
                        {i.status?.online ? (
                          <span className="text-xs text-muted">
                            {i.status.protection_enabled ? 'Protected' : <span className="text-warn">Protection off</span>} · {i.status.latency_ms} ms
                          </span>
                        ) : (
                          <Tip content={i.status?.last_error || 'Not checked yet'}>
                            <span className="text-xs text-danger">Offline · seen {timeAgo(i.status?.last_seen_at)}</span>
                          </Tip>
                        )}
                      </Td>
                      <Td>
                        {(() => {
                          const s = syncById.get(i.id)
                          if (!s) return <span className="text-xs text-faint">not managed</span>
                          if (s.errors && Object.keys(s.errors).length) return <Badge tone="danger">check failed</Badge>
                          return s.total === 0 ? <Badge tone="ok">in sync</Badge> : <Badge tone="warn">{s.total} to change</Badge>
                        })()}
                      </Td>
                      <Td className="text-right" onClick={(e) => e.stopPropagation()}>
                        <div className="flex justify-end gap-1">
                          <Tip content="Check now">
                            <Button size="icon" variant="ghost" onClick={() => refresh.mutate(i.id)} aria-label="Check now">
                              <RefreshCw className={`h-4 w-4 ${refresh.isPending && refresh.variables === i.id ? 'animate-spin' : ''}`} />
                            </Button>
                          </Tip>
                          {can('admin') && (
                            <>
                              <Tip content="Edit">
                                <Button size="icon" variant="ghost" onClick={() => setDialog({ open: true, instance: i })} aria-label="Edit">
                                  <Pencil className="h-4 w-4" />
                                </Button>
                              </Tip>
                              <Tip content="Remove">
                                <Button size="icon" variant="ghost" onClick={() => setToDelete(i)} aria-label="Remove">
                                  <Trash2 className="h-4 w-4" />
                                </Button>
                              </Tip>
                            </>
                          )}
                        </div>
                      </Td>
                    </tr>
                  )
                })}
              </tbody>
            </Table>
          )}
        </Card>
      </div>
      <InstanceDialog open={dialog.open} onOpenChange={(v) => setDialog((d) => ({ ...d, open: v }))} instance={dialog.instance} groups={groups} hosts={hosts} />
      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(v) => !v && setToDelete(null)}
        title={`Remove ${toDelete?.name}?`}
        description="GuardTower stops managing this instance and deletes its stored history. The AdGuard Home server itself is not changed."
        confirmLabel="Remove"
        danger
        loading={del.isPending}
        onConfirm={() => toDelete && del.mutate(toDelete.id)}
      />
    </>
  )
}
