import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { BellRing, Plus, Send, Trash2 } from 'lucide-react'
import { api, type NotifyTarget } from '@/lib/api'
import { Badge, Button, Card, CardHeader, ConfirmDialog, Dialog, Field, Input, Select, Switch, Table, Td, Th } from './ui'

const kindLabel = { ntfy: 'ntfy', discord: 'Discord', webhook: 'Webhook' }
const urlHint = {
  ntfy: 'Topic URL, e.g. https://ntfy.sh/our-family-alerts (pick a hard-to-guess topic) or your own ntfy server.',
  discord: 'Channel webhook URL from Discord: Server settings → Integrations → Webhooks.',
  webhook: 'GuardTower POSTs JSON {title, body, event, data} to this URL.',
}

/** Where GuardTower sends alerts and digests. */
export function NotifyTargetsCard() {
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['notify-targets'], queryFn: () => api.get<NotifyTarget[]>('/notify/targets') })
  const [edit, setEdit] = useState<{ open: boolean; t?: NotifyTarget }>({ open: false })
  const [del, setDel] = useState<NotifyTarget | null>(null)
  const test = useMutation({
    mutationFn: (id: number) => api.post<{ ok: boolean; error?: string }>(`/notify/targets/${id}/test`, {}),
    onSuccess: (r) => (r.ok ? toast.success('Test message sent') : toast.error('Test failed', { description: r.error })),
    onError: (e) => toast.error((e as Error).message),
  })
  const remove = useMutation({
    mutationFn: (id: number) => api.del(`/notify/targets/${id}`),
    onSuccess: () => {
      setDel(null)
      void qc.invalidateQueries({ queryKey: ['notify-targets'] })
    },
  })
  return (
    <Card className="xl:col-span-3">
      <CardHeader
        title={
          <span className="inline-flex items-center gap-2">
            <BellRing className="h-4 w-4 text-muted" /> Notifications
          </span>
        }
        description="Where GuardTower sends Family DNS alerts and the weekly digest."
        actions={
          <Button size="sm" onClick={() => setEdit({ open: true })}>
            <Plus className="h-3.5 w-3.5" /> Add target
          </Button>
        }
      />
      {!data || data.length === 0 ? (
        <p className="px-5 pb-5 text-sm text-muted">No notification targets yet. ntfy is the easiest: install the app on your phone and subscribe to a private topic.</p>
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>Name</Th>
              <Th>Type</Th>
              <Th>URL</Th>
              <Th>Receives</Th>
              <Th>
                <span className="sr-only">Actions</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {data.map((t) => (
              <tr key={t.id}>
                <Td className="font-medium">
                  {t.name} {!t.enabled && <Badge>off</Badge>}
                </Td>
                <Td className="text-sm">{kindLabel[t.kind]}</Td>
                <Td className="max-w-xs truncate font-mono text-xs text-muted">{t.url}</Td>
                <Td className="text-sm">{t.family ? 'Family alerts & digest' : '—'}</Td>
                <Td className="whitespace-nowrap text-right">
                  <Button size="sm" variant="ghost" onClick={() => test.mutate(t.id)} loading={test.isPending && test.variables === t.id}>
                    <Send className="h-3.5 w-3.5" /> Test
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setEdit({ open: true, t })}>
                    Edit
                  </Button>
                  <Button size="icon" variant="ghost" onClick={() => setDel(t)} aria-label={`Delete ${t.name}`}>
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {edit.open && <TargetDialog target={edit.t} onClose={() => setEdit({ open: false })} />}
      <ConfirmDialog
        open={!!del}
        onOpenChange={(v) => !v && setDel(null)}
        title={`Delete ${del?.name}?`}
        description="GuardTower stops sending notifications there."
        confirmLabel="Delete"
        danger
        loading={remove.isPending}
        onConfirm={() => del && remove.mutate(del.id)}
      />
    </Card>
  )
}

function TargetDialog({ target, onClose }: { target?: NotifyTarget; onClose: () => void }) {
  const qc = useQueryClient()
  const [name, setName] = useState(target?.name ?? '')
  const [kind, setKind] = useState<NotifyTarget['kind']>(target?.kind ?? 'ntfy')
  const [url, setUrl] = useState(target?.url ?? '')
  const [token, setToken] = useState<string | null>(null)
  const [enabled, setEnabled] = useState(target?.enabled ?? true)
  const [family, setFamily] = useState(target?.family ?? true)
  const save = useMutation({
    mutationFn: () => {
      const body = { name, kind, url, token, enabled, family }
      return target ? api.put(`/notify/targets/${target.id}`, body) : api.post('/notify/targets', body)
    },
    onSuccess: () => {
      toast.success('Notification target saved')
      void qc.invalidateQueries({ queryKey: ['notify-targets'] })
      onClose()
    },
    onError: (e) => toast.error((e as Error).message),
  })
  return (
    <Dialog
      open
      onOpenChange={(v) => !v && onClose()}
      title={target ? `Edit ${target.name}` : 'Add a notification target'}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Save
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Parents' phones" />
        </Field>
        <Field label="Type">
          <Select value={kind} onChange={(e) => setKind(e.target.value as NotifyTarget['kind'])}>
            <option value="ntfy">ntfy (push to phones)</option>
            <option value="discord">Discord</option>
            <option value="webhook">Generic webhook</option>
          </Select>
        </Field>
        <Field label="URL" hint={urlHint[kind]}>
          <Input value={url} onChange={(e) => setUrl(e.target.value)} className="font-mono" />
        </Field>
        {kind !== 'discord' && (
          <Field label="Access token" hint={target?.has_token && token === null ? 'A token is stored. Type a new one to replace it.' : 'Optional bearer token (ntfy access token or webhook secret). Stored encrypted.'}>
            <Input type="password" autoComplete="off" value={token ?? ''} onChange={(e) => setToken(e.target.value)} />
          </Field>
        )}
        <label className="flex items-center gap-2 text-sm">
          <Switch checked={family} onCheckedChange={setFamily} /> Send Family DNS alerts and the weekly digest here
        </label>
        <label className="flex items-center gap-2 text-sm">
          <Switch checked={enabled} onCheckedChange={setEnabled} /> Enabled
        </label>
      </div>
    </Dialog>
  )
}
