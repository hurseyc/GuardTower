import { useState } from 'react'
import { useNavigate } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Camera, RotateCcw } from 'lucide-react'
import { api, type Snapshot } from '@/lib/api'
import { useSession } from '@/lib/sessionContext'
import { fmtDateTime, timeAgo } from '@/lib/utils'
import { Badge, Button, Card, CardHeader, ConfirmDialog, Dialog, Field, Input, Skeleton, Table, Td, Th } from './ui'

const kindLabel: Record<Snapshot['kind'], { label: string; tone: 'neutral' | 'info' | 'ok' | 'warn' }> = {
  'pre-apply': { label: 'before apply', tone: 'info' },
  'post-apply': { label: 'after apply', tone: 'ok' },
  manual: { label: 'manual', tone: 'neutral' },
  'pre-restore': { label: 'before restore', tone: 'warn' },
}

/** Saved copies of an instance's managed settings, with one-click restore. */
export function SnapshotsCard({ instanceId, instanceName }: { instanceId: number; instanceName: string }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { can } = useSession()
  const { data } = useQuery({
    queryKey: ['snapshots', instanceId],
    queryFn: () => api.get<Snapshot[]>(`/instances/${instanceId}/snapshots?limit=20`),
  })
  const [takeOpen, setTakeOpen] = useState(false)
  const [note, setNote] = useState('')
  const [restore, setRestore] = useState<Snapshot | null>(null)

  const take = useMutation({
    mutationFn: () => api.post<{ id: number }>(`/instances/${instanceId}/snapshots`, { note }),
    onSuccess: () => {
      toast.success('Snapshot saved')
      setTakeOpen(false)
      setNote('')
      void qc.invalidateQueries({ queryKey: ['snapshots', instanceId] })
    },
    onError: (e) => toast.error((e as Error).message),
  })
  const doRestore = useMutation({
    mutationFn: (sid: number) => api.post<{ id: number }>(`/snapshots/${sid}/restore`, {}),
    onSuccess: (r) => {
      setRestore(null)
      toast.success('Restore started')
      void navigate(`/changes?rollout=${r.id}`)
    },
    onError: (e) => toast.error((e as Error).message),
  })

  return (
    <Card className="xl:col-span-3">
      <CardHeader
        title="Snapshots"
        description="GuardTower saves this instance's settings before and after every change. Restore any snapshot to roll back."
        actions={
          can('operator') && (
            <Button size="sm" onClick={() => setTakeOpen(true)}>
              <Camera className="h-3.5 w-3.5" /> Take snapshot
            </Button>
          )
        }
      />
      {!data ? (
        <div className="px-5 pb-5">
          <Skeleton className="h-16" />
        </div>
      ) : data.length === 0 ? (
        <p className="px-5 pb-5 text-sm text-muted">No snapshots yet. One is taken automatically before every change GuardTower applies.</p>
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>Taken</Th>
              <Th>Kind</Th>
              <Th>Note</Th>
              <Th className="text-right">
                <span className="sr-only">Actions</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {data.map((s) => (
              <tr key={s.id}>
                <Td className="text-sm" title={fmtDateTime(s.created_at)}>
                  {timeAgo(s.created_at)}
                </Td>
                <Td>
                  <Badge tone={kindLabel[s.kind]?.tone ?? 'neutral'}>{kindLabel[s.kind]?.label ?? s.kind}</Badge>
                </Td>
                <Td className="text-sm text-muted">
                  {s.note || (s.rollout_id ? `Rollout #${s.rollout_id}` : '—')}
                </Td>
                <Td className="text-right">
                  {can('operator') && (
                    <Button size="sm" variant="ghost" onClick={() => setRestore(s)}>
                      <RotateCcw className="h-3.5 w-3.5" /> Restore
                    </Button>
                  )}
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <Dialog
        open={takeOpen}
        onOpenChange={setTakeOpen}
        title="Take a snapshot"
        description={`Save ${instanceName}'s current settings so you can return to them later.`}
        footer={
          <>
            <Button variant="ghost" onClick={() => setTakeOpen(false)}>
              Cancel
            </Button>
            <Button variant="primary" loading={take.isPending} onClick={() => take.mutate()}>
              Save snapshot
            </Button>
          </>
        }
      >
        <Field label="Note" hint="Optional, e.g. “before trying new upstreams”.">
          <Input value={note} onChange={(e) => setNote(e.target.value)} maxLength={200} />
        </Field>
      </Dialog>
      <ConfirmDialog
        open={!!restore}
        onOpenChange={(v) => !v && setRestore(null)}
        title={`Restore ${instanceName}?`}
        description={
          restore &&
          `Every managed setting on ${instanceName} goes back to how it was ${timeAgo(restore.created_at)}. GuardTower snapshots the current state first, so you can undo this.`
        }
        confirmLabel="Restore"
        danger
        loading={doRestore.isPending}
        onConfirm={() => restore && doRestore.mutate(restore.id)}
      />
    </Card>
  )
}
