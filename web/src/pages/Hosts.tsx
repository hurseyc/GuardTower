import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Building2, HardDrive, Pencil, Plus, Server, Trash2 } from 'lucide-react'
import { api, type Host, type HostKind, type Overview, type Site } from '@/lib/api'
import { useSession } from '@/lib/session'
import { PageHeader } from '@/components/AppShell'
import { useResilience } from '@/components/Resilience'
import { Badge, Button, Card, CardHeader, ConfirmDialog, Dialog, EmptyState, Field, Input, Select, StatusDot, Textarea } from '@/components/ui'

const kinds: { value: HostKind; label: string }[] = [
  { value: 'proxmox', label: 'Proxmox node' },
  { value: 'nas', label: 'NAS' },
  { value: 'docker', label: 'Docker host' },
  { value: 'vm', label: 'VM host' },
  { value: 'baremetal', label: 'Bare-metal server' },
  { value: 'router', label: 'Router' },
  { value: 'cloud', label: 'Cloud VM' },
  { value: 'other', label: 'Other' },
]
const kindLabel = Object.fromEntries(kinds.map((k) => [k.value, k.label]))

export function HostsPage() {
  const { can } = useSession()
  const { data: res } = useResilience()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview') })
  const [edit, setEdit] = useState<{ open: boolean; host?: Host }>({ open: false })
  const [siteEdit, setSiteEdit] = useState<{ open: boolean; site?: Site }>({ open: false })
  const [toDelete, setToDelete] = useState<Host | null>(null)
  const invalidate = () => void qc.invalidateQueries({ queryKey: ['overview'] })
  const del = useMutation({
    mutationFn: (id: number) => api.del(`/hosts/${id}`),
    onSuccess: () => {
      toast.success('Host removed')
      setToDelete(null)
      invalidate()
    },
  })
  const delSite = useMutation({ mutationFn: (id: number) => api.del(`/sites/${id}`), onSuccess: invalidate })

  const hosts = data?.hosts ?? []
  const sites = data?.sites ?? []
  const siteName = new Map(sites.map((s) => [s.id, s.name]))
  const instancesOn = (hid: number) => (data?.instances ?? []).filter((i) => i.host_id === hid)
  const unassigned = (data?.instances ?? []).filter((i) => !i.host_id)

  return (
    <>
      <PageHeader
        title="Hosts & sites"
        description="Describe where each instance physically runs so GuardTower can warn about single points of failure."
        actions={
          can('admin') && (
            <>
              <Button variant="outline" onClick={() => setSiteEdit({ open: true })}>
                <Building2 className="h-4 w-4" /> New site
              </Button>
              <Button variant="primary" onClick={() => setEdit({ open: true })}>
                <Plus className="h-4 w-4" /> New host
              </Button>
            </>
          )
        }
      />
      <div className="space-y-6 p-6">
        {hosts.length === 0 ? (
          <Card>
            <EmptyState
              icon={<Server className="h-6 w-6" />}
              title="No hosts defined"
              description="Add each Proxmox node, NAS or server that runs AdGuard Home, then assign instances to them. Two instances in the same group on the same host are a single point of failure."
              action={
                can('admin') && (
                  <Button variant="primary" onClick={() => setEdit({ open: true })}>
                    <Plus className="h-4 w-4" /> Add host
                  </Button>
                )
              }
            />
          </Card>
        ) : (
          <div className="grid gap-4 md:grid-cols-2 2xl:grid-cols-3">
            {hosts.map((h) => {
              const insts = instancesOn(h.id)
              const spofGroups = (res?.spof_hosts[String(h.id)] ?? []).map((gid) => res?.groups.find((g) => g.group_id === gid)?.name).filter(Boolean)
              return (
                <Card key={h.id} className="p-5">
                  <div className="flex items-start gap-3">
                    <div className="rounded-xl border border-border bg-subtle p-2.5 text-muted">
                      {h.kind === 'nas' ? <HardDrive className="h-5 w-5" /> : <Server className="h-5 w-5" />}
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="font-semibold">{h.name}</div>
                      <div className="text-xs text-muted">
                        {kindLabel[h.kind]}
                        {h.address && <span className="font-mono"> · {h.address}</span>}
                      </div>
                      <div className="mt-2 flex flex-wrap gap-1">
                        {h.site_id ? <Badge>site: {siteName.get(h.site_id)}</Badge> : null}
                        {h.power ? <Badge>power: {h.power}</Badge> : null}
                        {spofGroups.length > 0 && (
                          <Badge tone="warn" title="If this host goes down, these groups lose all their DNS servers">
                            only host of {spofGroups.join(', ')}
                          </Badge>
                        )}
                      </div>
                    </div>
                    {can('admin') && (
                      <div className="flex gap-1">
                        <Button size="icon" variant="ghost" onClick={() => setEdit({ open: true, host: h })} aria-label="Edit host">
                          <Pencil className="h-4 w-4" />
                        </Button>
                        <Button size="icon" variant="ghost" onClick={() => setToDelete(h)} aria-label="Delete host">
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    )}
                  </div>
                  <div className="mt-4 space-y-1">
                    {insts.length === 0 ? (
                      <p className="text-sm text-faint">No instances assigned.</p>
                    ) : (
                      insts.map((i) => (
                        <div key={i.id} className="flex items-center gap-2 text-sm">
                          <StatusDot online={i.status?.online} />
                          {i.name}
                        </div>
                      ))
                    )}
                  </div>
                  {h.notes && <p className="mt-3 border-t border-border pt-3 text-xs text-muted">{h.notes}</p>}
                </Card>
              )
            })}
          </div>
        )}
        {unassigned.length > 0 && hosts.length > 0 && (
          <Card className="p-4 text-sm text-muted">
            <strong className="text-fg">Not assigned to a host:</strong> {unassigned.map((i) => i.name).join(', ')}. Edit an instance to set its host.
          </Card>
        )}
        <Card>
          <CardHeader title="Sites" description="Optional: physical locations (home, office, cabin). Used for site-level redundancy checks." />
          <div className="flex flex-wrap gap-2 px-5 pb-5">
            {sites.length === 0 && <span className="text-sm text-faint">No sites defined.</span>}
            {sites.map((s) => (
              <span key={s.id} className="inline-flex items-center gap-2 rounded-lg border border-border px-3 py-1.5 text-sm">
                <Building2 className="h-3.5 w-3.5 text-muted" />
                {s.name}
                {can('admin') && (
                  <>
                    <button className="text-faint hover:text-fg" onClick={() => setSiteEdit({ open: true, site: s })} aria-label="Rename site">
                      <Pencil className="h-3 w-3" />
                    </button>
                    <button className="text-faint hover:text-danger" onClick={() => delSite.mutate(s.id)} aria-label="Delete site">
                      <Trash2 className="h-3 w-3" />
                    </button>
                  </>
                )}
              </span>
            ))}
          </div>
        </Card>
      </div>
      <HostDialog open={edit.open} host={edit.host} sites={sites} onOpenChange={(v) => setEdit((e) => ({ ...e, open: v }))} onSaved={invalidate} />
      <SiteDialog open={siteEdit.open} site={siteEdit.site} onOpenChange={(v) => setSiteEdit((e) => ({ ...e, open: v }))} onSaved={invalidate} />
      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(v) => !v && setToDelete(null)}
        title={`Remove host ${toDelete?.name}?`}
        description="Instances on this host become unassigned. Nothing changes on the machines themselves."
        confirmLabel="Remove"
        danger
        loading={del.isPending}
        onConfirm={() => toDelete && del.mutate(toDelete.id)}
      />
    </>
  )
}

function HostDialog({ open, host, sites, onOpenChange, onSaved }: { open: boolean; host?: Host; sites: Site[]; onOpenChange: (v: boolean) => void; onSaved: () => void }) {
  const [f, setF] = useState<Omit<Host, 'id'>>({ name: '', kind: 'proxmox', address: '', power: '', notes: '', site_id: 0 })
  useEffect(() => {
    if (open) setF(host ? { ...host, site_id: host.site_id ?? 0 } : { name: '', kind: 'proxmox', address: '', power: '', notes: '', site_id: 0 })
  }, [open, host])
  const save = useMutation({
    mutationFn: () => {
      const body = { name: f.name, kind: f.kind, address: f.address, power: f.power, notes: f.notes, site_id: Number(f.site_id) }
      return host ? api.put(`/hosts/${host.id}`, body) : api.post('/hosts', body)
    },
    onSuccess: () => {
      toast.success('Host saved')
      onSaved()
      onOpenChange(false)
    },
    onError: (e) => toast.error((e as Error).message),
  })
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={host ? 'Edit host' : 'New host'}
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="primary" loading={save.isPending} disabled={!f.name.trim()} onClick={() => save.mutate()}>
            Save
          </Button>
        </>
      }
    >
      <form className="grid gap-4 sm:grid-cols-2" onSubmit={(e) => (e.preventDefault(), save.mutate())}>
        <Field label="Name">
          <Input autoFocus value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="pve-01" />
        </Field>
        <Field label="Type">
          <Select value={f.kind} onChange={(e) => setF({ ...f, kind: e.target.value as HostKind })}>
            {kinds.map((k) => (
              <option key={k.value} value={k.value}>
                {k.label}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Address" hint="IP or hostname (informational)">
          <Input value={f.address} onChange={(e) => setF({ ...f, address: e.target.value })} className="font-mono" placeholder="192.168.1.10" />
        </Field>
        <Field label="Site">
          <Select value={f.site_id} onChange={(e) => setF({ ...f, site_id: Number(e.target.value) })}>
            <option value={0}>None</option>
            {sites.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Power / UPS" className="sm:col-span-2" hint="Hosts sharing the same value are treated as one power failure domain, e.g. “UPS-rack”">
          <Input value={f.power} onChange={(e) => setF({ ...f, power: e.target.value })} placeholder="UPS-rack" />
        </Field>
        <Field label="Notes" className="sm:col-span-2">
          <Textarea rows={2} value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} className="font-sans" />
        </Field>
      </form>
    </Dialog>
  )
}

function SiteDialog({ open, site, onOpenChange, onSaved }: { open: boolean; site?: Site; onOpenChange: (v: boolean) => void; onSaved: () => void }) {
  const [name, setName] = useState('')
  useEffect(() => {
    if (open) setName(site?.name ?? '')
  }, [open, site])
  const save = useMutation({
    mutationFn: () => (site ? api.put(`/sites/${site.id}`, { name }) : api.post('/sites', { name })),
    onSuccess: () => {
      onSaved()
      onOpenChange(false)
    },
    onError: (e) => toast.error((e as Error).message),
  })
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={site ? 'Rename site' : 'New site'}
      footer={
        <Button variant="primary" loading={save.isPending} disabled={!name.trim()} onClick={() => save.mutate()}>
          Save
        </Button>
      }
    >
      <form onSubmit={(e) => (e.preventDefault(), save.mutate())}>
        <Field label="Site name">
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Home" />
        </Field>
      </form>
    </Dialog>
  )
}
