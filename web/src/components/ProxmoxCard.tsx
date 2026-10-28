import { useState } from 'react'
import { Link } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import { api, type ProxmoxCluster, type ProxmoxGuest, type ProxmoxSync } from '@/lib/api'
import { timeAgo } from '@/lib/utils'
import { Badge, Button, Card, CardHeader, ConfirmDialog, Dialog, Field, Input, Switch, Table, Td, Th } from './ui'

function syncToast(r: ProxmoxSync) {
  const moved = r.moves.map((m) => `${m.instance} → ${m.to}`).join(', ')
  toast.success(`Found ${r.guests} guests, ${r.matched} running AdGuard Home`, { description: moved ? `Placed: ${moved}` : 'Placement is up to date.' })
}

/** Read-only Proxmox VE connections that keep instance placement current. */
export function ProxmoxCard({ admin }: { admin: boolean }) {
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['proxmox'], queryFn: () => api.get<{ clusters: ProxmoxCluster[]; guests: ProxmoxGuest[] }>('/proxmox') })
  const [edit, setEdit] = useState<{ open: boolean; c?: ProxmoxCluster }>({ open: false })
  const [del, setDel] = useState<ProxmoxCluster | null>(null)
  const invalidate = () => {
    for (const k of ['proxmox', 'overview', 'resilience', 'instances']) void qc.invalidateQueries({ queryKey: [k] })
  }
  const sync = useMutation({
    mutationFn: (id: number) => api.post<ProxmoxSync>(`/proxmox/${id}/sync`, {}),
    onSuccess: (r) => {
      syncToast(r)
      invalidate()
    },
    onError: (e) => {
      toast.error('Sync failed', { description: (e as Error).message })
      invalidate()
    },
  })
  const remove = useMutation({
    mutationFn: (id: number) => api.del(`/proxmox/${id}`),
    onSuccess: () => {
      setDel(null)
      invalidate()
    },
  })
  const clusters = data?.clusters ?? []
  const matched = (data?.guests ?? []).filter((g) => g.instance_id)
  return (
    <Card>
      <CardHeader
        title="Proxmox VE"
        description="Connect Proxmox with a read-only API token and GuardTower places each AdGuard Home LXC or VM on its node, and moves it when you migrate it."
        actions={
          admin && (
            <Button size="sm" onClick={() => setEdit({ open: true })}>
              <Plus className="h-3.5 w-3.5" /> Connect Proxmox
            </Button>
          )
        }
      />
      {clusters.length === 0 ? (
        <p className="px-5 pb-5 text-sm text-muted">
          Not connected. Placement is set by hand on each instance. See{' '}
          <Link className="text-accent hover:underline" to="/docs/setup/proxmox-api">
            Proxmox API token
          </Link>{' '}
          for the two-minute setup.
        </p>
      ) : (
        <div className="space-y-4 px-5 pb-5">
          {clusters.map((c) => (
            <div key={c.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-lg border border-border px-4 py-3 text-sm">
              <span className="font-medium">{c.name}</span>
              <span className="font-mono text-xs text-muted">{c.url}</span>
              {c.last_error ? (
                <Badge tone="danger" title={c.last_error}>
                  sync failed
                </Badge>
              ) : (
                <span className="text-xs text-faint">{c.last_sync_at ? `synced ${timeAgo(c.last_sync_at)}` : 'not synced yet'}</span>
              )}
              {admin && (
                <span className="ml-auto flex gap-1">
                  <Button size="sm" variant="ghost" onClick={() => sync.mutate(c.id)} loading={sync.isPending && sync.variables === c.id}>
                    <RefreshCw className="h-3.5 w-3.5" /> Sync now
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setEdit({ open: true, c })}>
                    Edit
                  </Button>
                  <Button size="icon" variant="ghost" onClick={() => setDel(c)} aria-label={`Disconnect ${c.name}`}>
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </span>
              )}
              {c.last_error && <p className="w-full text-xs text-danger">{c.last_error}</p>}
            </div>
          ))}
          {matched.length > 0 && (
            <Table>
              <thead>
                <tr>
                  <Th>Instance</Th>
                  <Th>Guest</Th>
                  <Th>Node</Th>
                  <Th>Addresses</Th>
                </tr>
              </thead>
              <tbody>
                {matched.map((g) => (
                  <tr key={`${g.cluster_id}-${g.vmid}`}>
                    <Td className="font-medium">
                      <Link className="hover:underline" to={`/instances/${g.instance_id}`}>
                        {g.instance_name}
                      </Link>
                    </Td>
                    <Td className="text-sm">
                      {g.kind === 'lxc' ? 'CT' : 'VM'} {g.vmid} · {g.name}
                    </Td>
                    <Td className="text-sm">{g.node}</Td>
                    <Td className="font-mono text-xs text-muted">{g.ips.join(', ') || '—'}</Td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </div>
      )}
      {edit.open && (
        <ClusterDialog
          cluster={edit.c}
          onClose={() => setEdit({ open: false })}
          onSaved={() => {
            invalidate()
            setEdit({ open: false })
          }}
        />
      )}
      <ConfirmDialog
        open={!!del}
        onOpenChange={(v) => !v && setDel(null)}
        title={`Disconnect ${del?.name}?`}
        description="Instances keep their current hosts; GuardTower just stops updating them from Proxmox."
        confirmLabel="Disconnect"
        danger
        loading={remove.isPending}
        onConfirm={() => del && remove.mutate(del.id)}
      />
    </Card>
  )
}

function ClusterDialog({ cluster, onClose, onSaved }: { cluster?: ProxmoxCluster; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState(cluster?.name ?? 'Proxmox')
  const [url, setUrl] = useState(cluster?.url ?? 'https://')
  const [tokenId, setTokenId] = useState(cluster?.token_id ?? 'guardtower@pve!readonly')
  const [secret, setSecret] = useState<string | null>(null)
  const [skip, setSkip] = useState(cluster?.tls_skip_verify ?? true)
  const body = () => ({ name, url, token_id: tokenId, secret, tls_skip_verify: skip })
  const test = useMutation({
    mutationFn: () => api.post<{ ok: boolean; version?: string; guests?: number; error?: string }>(`/proxmox/test${cluster ? `?id=${cluster.id}` : ''}`, body()),
    onSuccess: (r) =>
      r.ok ? toast.success(`Connected to Proxmox VE ${r.version}`, { description: `${r.guests} containers and VMs visible.` }) : toast.error('Connection failed', { description: r.error }),
    onError: (e) => toast.error((e as Error).message),
  })
  const save = useMutation({
    mutationFn: () => (cluster ? api.put<{ sync?: ProxmoxSync; sync_error?: string }>(`/proxmox/${cluster.id}`, body()) : api.post<{ sync?: ProxmoxSync; sync_error?: string }>('/proxmox', body())),
    onSuccess: (r) => {
      if (r.sync_error) toast.warning('Saved, but the first sync failed', { description: r.sync_error })
      else if (r.sync) syncToast(r.sync)
      onSaved()
    },
    onError: (e) => toast.error((e as Error).message),
  })
  return (
    <Dialog
      open
      onOpenChange={(v) => !v && onClose()}
      title={cluster ? `Edit ${cluster.name}` : 'Connect Proxmox VE'}
      description="Read-only: GuardTower only lists guests, their nodes and addresses."
      footer={
        <>
          <Button variant="ghost" onClick={() => test.mutate()} loading={test.isPending}>
            Test connection
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Save & sync
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="URL" hint="Any node of the cluster, e.g. https://192.168.10.11:8006">
          <Input value={url} onChange={(e) => setUrl(e.target.value)} className="font-mono" />
        </Field>
        <Field label="API token ID" hint="user@realm!token-name, with the PVEAuditor role">
          <Input value={tokenId} onChange={(e) => setTokenId(e.target.value)} className="font-mono" />
        </Field>
        <Field label="Token secret" hint={cluster?.has_secret && secret === null ? 'A secret is stored. Type a new one to replace it.' : 'Shown once when you create the token. Stored encrypted.'}>
          <Input type="password" autoComplete="off" value={secret ?? ''} onChange={(e) => setSecret(e.target.value)} className="font-mono" />
        </Field>
        <label className="flex items-center gap-2 text-sm">
          <Switch checked={skip} onCheckedChange={setSkip} /> Accept Proxmox's self-signed certificate
        </label>
      </div>
    </Dialog>
  )
}
