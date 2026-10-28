import { useEffect, useState } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { api, type AuditEntry, type Role, type User } from '@/lib/api'
import { useSession } from '@/lib/session'
import { fmtDateTime, timeAgo } from '@/lib/utils'
import { PageHeader } from '@/components/AppShell'
import { Badge, Button, Card, ConfirmDialog, Dialog, Field, Input, Select, Switch, Table, Td, Th } from '@/components/ui'

const roleHelp: Record<Role, string> = {
  viewer: 'Read-only access to dashboards, logs and configuration.',
  operator: 'Viewer, plus pause protection, run plans and upgrades.',
  admin: 'Full control, including instances, users and settings.',
}

export function UsersPage() {
  const qc = useQueryClient()
  const { user: me } = useSession()
  const { data } = useQuery({ queryKey: ['users'], queryFn: () => api.get<User[]>('/users') })
  const [edit, setEdit] = useState<{ open: boolean; user?: User }>({ open: false })
  const [toDelete, setToDelete] = useState<User | null>(null)
  const del = useMutation({
    mutationFn: (id: number) => api.del(`/users/${id}`),
    onSuccess: () => {
      toast.success('User deleted')
      setToDelete(null)
      void qc.invalidateQueries({ queryKey: ['users'] })
    },
    onError: (e) => toast.error((e as Error).message),
  })
  return (
    <>
      <PageHeader
        title="Users"
        description="Accounts that can sign in to GuardTower."
        actions={
          <Button variant="primary" onClick={() => setEdit({ open: true })}>
            <Plus className="h-4 w-4" /> Add user
          </Button>
        }
      />
      <div className="p-6">
        <Card>
          <Table>
            <thead>
              <tr>
                <Th>User</Th>
                <Th>Role</Th>
                <Th>Family DNS</Th>
                <Th>Last sign-in</Th>
                <Th className="text-right">Actions</Th>
              </tr>
            </thead>
            <tbody>
              {data?.map((u) => (
                <tr key={u.id}>
                  <Td>
                    <div className="flex items-center gap-2 font-medium">
                      {u.username}
                      {u.id === me?.id && <Badge tone="accent">you</Badge>}
                      {u.oidc && <Badge tone="info">SSO</Badge>}
                    </div>
                  </Td>
                  <Td>
                    <Badge tone={u.role === 'admin' ? 'accent' : 'neutral'} className="capitalize">
                      {u.role}
                    </Badge>
                  </Td>
                  <Td>{u.family_access ? <Badge tone="ok">granted</Badge> : <span className="text-faint">—</span>}</Td>
                  <Td className="text-sm text-muted">{timeAgo(u.last_login_at)}</Td>
                  <Td className="text-right">
                    <div className="flex justify-end gap-1">
                      <Button size="icon" variant="ghost" onClick={() => setEdit({ open: true, user: u })} aria-label="Edit user">
                        <Pencil className="h-4 w-4" />
                      </Button>
                      {u.id !== me?.id && (
                        <Button size="icon" variant="ghost" onClick={() => setToDelete(u)} aria-label="Delete user">
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      )}
                    </div>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Card>
      </div>
      <UserDialog open={edit.open} user={edit.user} onOpenChange={(v) => setEdit((e) => ({ ...e, open: v }))} />
      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(v) => !v && setToDelete(null)}
        title={`Delete ${toDelete?.username}?`}
        description="They are signed out immediately. Their audit history is kept."
        confirmLabel="Delete user"
        danger
        loading={del.isPending}
        onConfirm={() => toDelete && del.mutate(toDelete.id)}
      />
    </>
  )
}

function UserDialog({ open, user, onOpenChange }: { open: boolean; user?: User; onOpenChange: (v: boolean) => void }) {
  const qc = useQueryClient()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<Role>('viewer')
  const [family, setFamily] = useState(false)
  useEffect(() => {
    if (open) {
      setUsername(user?.username ?? '')
      setPassword('')
      setRole(user?.role ?? 'viewer')
      setFamily(user?.family_access ?? false)
    }
  }, [open, user])
  const save = useMutation({
    mutationFn: () =>
      user
        ? api.put(`/users/${user.id}`, { username, password, role, family_access: family })
        : api.post('/users', { username, password, role, family_access: family }),
    onSuccess: () => {
      toast.success('User saved')
      void qc.invalidateQueries({ queryKey: ['users'] })
      onOpenChange(false)
    },
    onError: (e) => toast.error((e as Error).message),
  })
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={user ? `Edit ${user.username}` : 'Add user'}
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Save
          </Button>
        </>
      }
    >
      <form className="space-y-4" onSubmit={(e) => (e.preventDefault(), save.mutate())}>
        <Field label="Username">
          <Input value={username} disabled={!!user} onChange={(e) => setUsername(e.target.value)} autoComplete="off" />
        </Field>
        {!user?.oidc && (
          <Field label={user ? 'New password' : 'Password'} hint={user ? 'Leave blank to keep the current password. Changing it signs the user out.' : 'At least 10 characters'}>
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
          </Field>
        )}
        <Field
          label="Role"
          hint={user?.oidc ? `${roleHelp[role]} Single sign-on accounts get their role from their groups again at every sign-in.` : roleHelp[role]}
        >
          <Select value={role} onChange={(e) => setRole(e.target.value as Role)}>
            <option value="viewer">Viewer</option>
            <option value="operator">Operator</option>
            <option value="admin">Admin</option>
          </Select>
        </Field>
        <div className="flex items-center justify-between rounded-lg border border-border px-3 py-2.5">
          <div>
            <div className="text-sm font-medium">Family DNS access</div>
            <div className="text-xs text-faint">Can view per-person activity, including adult-content attempts.</div>
          </div>
          <Switch checked={family} onCheckedChange={setFamily} />
        </div>
      </form>
    </Dialog>
  )
}

export function AuditPage() {
  const { data, fetchNextPage, hasNextPage, isFetchingNextPage } = useInfiniteQuery({
    queryKey: ['audit'],
    initialPageParam: 0,
    queryFn: ({ pageParam }) => api.get<AuditEntry[]>(`/audit?limit=100${pageParam ? `&before=${pageParam}` : ''}`),
    getNextPageParam: (last) => (last.length === 100 ? last[last.length - 1].id : undefined),
  })
  const rows = data?.pages.flat() ?? []
  return (
    <>
      <PageHeader title="Audit log" description="Every change made through GuardTower: who, what and when." />
      <div className="p-6">
        <Card>
          <Table>
            <thead>
              <tr>
                <Th>When</Th>
                <Th>User</Th>
                <Th>Action</Th>
                <Th>Target</Th>
                <Th>Details</Th>
                <Th>IP</Th>
              </tr>
            </thead>
            <tbody>
              {rows.map((e) => (
                <tr key={e.id}>
                  <Td className="whitespace-nowrap text-xs text-muted" title={fmtDateTime(e.ts)}>
                    {timeAgo(e.ts)}
                  </Td>
                  <Td className="text-sm">{e.username || <span className="text-faint">system</span>}</Td>
                  <Td>
                    <Badge tone={e.action.includes('failed') ? 'danger' : e.action.includes('delete') ? 'warn' : 'neutral'} className="font-mono">
                      {e.action}
                    </Badge>
                  </Td>
                  <Td className="text-sm">{e.target}</Td>
                  <Td className="max-w-sm truncate font-mono text-xs text-faint" title={e.detail}>
                    {e.detail === '{}' ? '' : e.detail}
                  </Td>
                  <Td className="font-mono text-xs text-faint">{e.ip}</Td>
                </tr>
              ))}
            </tbody>
          </Table>
          {hasNextPage && (
            <div className="border-t border-border p-3 text-center">
              <Button variant="ghost" size="sm" loading={isFetchingNextPage} onClick={() => void fetchNextPage()}>
                Load older entries
              </Button>
            </div>
          )}
        </Card>
      </div>
    </>
  )
}
