import { useState } from 'react'
import { Link } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Boxes, Pencil, Plus, Trash2, Users } from 'lucide-react'
import { api, type Group, type Overview } from '@/lib/api'
import { useSession } from '@/lib/sessionContext'
import { colorFor, palette } from '@/lib/utils'
import { FindingList, ScoreIcon } from '@/components/Resilience'
import { useResilience } from '@/lib/resilience'
import { scoreLabel } from '@/lib/labels'
import { PageHeader } from '@/components/AppShell'
import { Badge, Button, Card, ConfirmDialog, Dialog, EmptyState, Field, Input, Skeleton, StatusDot } from '@/components/ui'
import { useOnChange } from '@/lib/useOnChange'

export function GroupsPage() {
  const { data: res } = useResilience()
  const { can } = useSession()
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview') })
  const [edit, setEdit] = useState<{ open: boolean; group?: Group }>({ open: false })
  const [members, setMembers] = useState<Group | null>(null)
  const [toDelete, setToDelete] = useState<Group | null>(null)
  const invalidate = () => void qc.invalidateQueries({ queryKey: ['overview'] })

  const del = useMutation({
    mutationFn: (id: number) => api.del(`/groups/${id}`),
    onSuccess: () => {
      toast.success('Group deleted')
      setToDelete(null)
      invalidate()
    },
    onError: (e) => toast.error((e as Error).message),
  })

  const instById = new Map((data?.instances ?? []).map((i) => [i.id, i]))
  const ungrouped = (data?.instances ?? []).filter((i) => !i.group_id)

  return (
    <>
      <PageHeader
        title="Groups"
        description="Each instance belongs to one settings group. Groups inherit the fleet-wide base settings and override what differs."
        actions={
          can('admin') && (
            <Button variant="primary" onClick={() => setEdit({ open: true })}>
              <Plus className="h-4 w-4" /> New group
            </Button>
          )
        }
      />
      <div className="space-y-6 p-6">
        {isLoading ? (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <Skeleton className="h-40" />
            <Skeleton className="h-40" />
          </div>
        ) : data && data.groups.length === 0 ? (
          <Card>
            <EmptyState
              icon={<Boxes className="h-6 w-6" />}
              title="No groups yet"
              description="Create a group for each set of instances that should share settings, for example “Main network” (3 instances) and “Kids VLAN” (2 instances)."
              action={
                can('admin') && (
                  <Button variant="primary" onClick={() => setEdit({ open: true })}>
                    <Plus className="h-4 w-4" /> Create a group
                  </Button>
                )
              }
            />
          </Card>
        ) : (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 2xl:grid-cols-3">
            {data?.groups.map((g) => {
              const color = colorFor(g.name, g.color)
              const online = g.instance_ids.filter((id) => instById.get(id)?.status?.online).length
              const rep = res?.groups.find((x) => x.group_id === g.id)
              return (
                <Card key={g.id} className="relative overflow-hidden">
                  <div className="absolute inset-x-0 top-0 h-1" style={{ background: color }} />
                  <div className="p-5">
                    <div className="flex items-start justify-between gap-3">
                      <div>
                        <h3 className="font-semibold">{g.name}</h3>
                        <p className="mt-0.5 text-sm text-muted">{g.description || 'No description'}</p>
                      </div>
                      {can('admin') && (
                        <div className="flex gap-1">
                          <Button size="icon" variant="ghost" onClick={() => setEdit({ open: true, group: g })} aria-label="Edit group">
                            <Pencil className="h-4 w-4" />
                          </Button>
                          <Button size="icon" variant="ghost" onClick={() => setToDelete(g)} aria-label="Delete group">
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      )}
                    </div>
                    <div className="mt-4 flex items-center gap-2 text-xs text-muted">
                      <Badge tone={g.instance_ids.length === 0 ? 'neutral' : online === g.instance_ids.length ? 'ok' : 'warn'}>
                        {online}/{g.instance_ids.length} online
                      </Badge>
                      {rep && g.instance_ids.length > 0 && (
                        <span className="inline-flex items-center gap-1">
                          <ScoreIcon score={rep.score} className="h-3.5 w-3.5" />
                          {scoreLabel[rep.score]}
                          {rep.hosts > 0 && ` · ${rep.hosts} host${rep.hosts === 1 ? '' : 's'}`}
                        </span>
                      )}
                    </div>
                    {rep && rep.findings.length > 0 && (
                      <div className="mt-3">
                        <FindingList findings={rep.findings} />
                      </div>
                    )}
                    <div className="mt-3 space-y-1.5">
                      {g.instance_ids.length === 0 && <p className="text-sm text-faint">No instances in this group.</p>}
                      {g.instance_ids.map((id) => {
                        const i = instById.get(id)
                        if (!i) return null
                        return (
                          <Link key={id} to={`/instances/${id}`} className="flex items-center gap-2 rounded-lg px-2 py-1.5 text-sm hover:bg-hover">
                            <StatusDot online={i.status?.online} />
                            {i.name}
                            <span className="ml-auto font-mono text-xs text-faint">{i.status?.version}</span>
                          </Link>
                        )
                      })}
                    </div>
                    {can('admin') && (
                      <Button variant="outline" size="sm" className="mt-4" onClick={() => setMembers(g)}>
                        <Users className="h-3.5 w-3.5" /> Manage members
                      </Button>
                    )}
                  </div>
                </Card>
              )
            })}
          </div>
        )}
        {ungrouped.length > 0 && data && data.groups.length > 0 && (
          <Card className="p-4 text-sm text-muted">
            <strong className="text-fg">{ungrouped.length} ungrouped:</strong> {ungrouped.map((i) => i.name).join(', ')}. Ungrouped instances receive only the base settings.
          </Card>
        )}
      </div>
      <GroupDialog open={edit.open} group={edit.group} onOpenChange={(v) => setEdit((e) => ({ ...e, open: v }))} onSaved={invalidate} />
      {members && data && <MembersDialog group={members} overview={data} onClose={() => setMembers(null)} onSaved={invalidate} />}
      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(v) => !v && setToDelete(null)}
        title={`Delete group ${toDelete?.name}?`}
        description="Its instances become ungrouped and will receive only the base settings on the next apply. Nothing is pushed until you apply a plan."
        confirmLabel="Delete group"
        danger
        loading={del.isPending}
        onConfirm={() => toDelete && del.mutate(toDelete.id)}
      />
    </>
  )
}

function GroupDialog({ open, group, onOpenChange, onSaved }: { open: boolean; group?: Group; onOpenChange: (v: boolean) => void; onSaved: () => void }) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [color, setColor] = useState('')
  useOnChange(open ? (group?.id ?? 'new') : null, (k) => {
    if (k !== null) {
      setName(group?.name ?? '')
      setDescription(group?.description ?? '')
      setColor(group?.color ?? '')
    }
  })
  const save = useMutation({
    mutationFn: () => (group ? api.put(`/groups/${group.id}`, { name, description, color }) : api.post('/groups', { name, description, color })),
    onSuccess: () => {
      toast.success(group ? 'Group saved' : 'Group created')
      onSaved()
      onOpenChange(false)
    },
    onError: (e) => toast.error((e as Error).message),
  })
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={group ? 'Edit group' : 'New group'}
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
      <form className="space-y-4" onSubmit={(e) => (e.preventDefault(), save.mutate())}>
        <Field label="Name">
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Main network" />
        </Field>
        <Field label="Description">
          <Input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Adults' devices, standard filtering" />
        </Field>
        <Field label="Colour">
          <div className="flex gap-2">
            {palette.map((c) => (
              <button
                type="button"
                key={c}
                onClick={() => setColor(c)}
                className={`h-7 w-7 rounded-full border-2 ${color === c ? 'border-fg' : 'border-transparent'}`}
                style={{ background: c }}
                aria-label={`Colour ${c}`}
              />
            ))}
          </div>
        </Field>
      </form>
    </Dialog>
  )
}

function MembersDialog({ group, overview, onClose, onSaved }: { group: Group; overview: Overview; onClose: () => void; onSaved: () => void }) {
  const [sel, setSel] = useState<Set<number>>(new Set(group.instance_ids))
  const groupName = new Map(overview.groups.map((g) => [g.id, g.name]))
  const save = useMutation({
    mutationFn: () => api.put(`/groups/${group.id}/members`, { instance_ids: [...sel] }),
    onSuccess: () => {
      toast.success('Membership updated')
      onSaved()
      onClose()
    },
    onError: (e) => toast.error((e as Error).message),
  })
  const toggle = (id: number) => setSel((s) => {
    const n = new Set(s)
    if (n.has(id)) n.delete(id)
    else n.add(id)
    return n
  })
  return (
    <Dialog
      open
      onOpenChange={(v) => !v && onClose()}
      title={`Members of ${group.name}`}
      description="An instance can belong to only one settings group; selecting it here moves it from its current group."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Save membership
          </Button>
        </>
      }
    >
      <div className="space-y-1">
        {overview.instances.map((i) => {
          const other = i.group_id && i.group_id !== group.id ? groupName.get(i.group_id) : undefined
          return (
            <label key={i.id} className="flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2 hover:bg-hover">
              <input type="checkbox" checked={sel.has(i.id)} onChange={() => toggle(i.id)} className="h-4 w-4 accent-[var(--accent)]" />
              <StatusDot online={i.status?.online} />
              <span className="text-sm font-medium">{i.name}</span>
              {other && <span className="ml-auto text-xs text-warn">{sel.has(i.id) ? `moves from ${other}` : `in ${other}`}</span>}
            </label>
          )
        })}
      </div>
    </Dialog>
  )
}
