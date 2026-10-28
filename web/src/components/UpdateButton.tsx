import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ArrowUpCircle, CheckCircle2, Loader2, RefreshCw } from 'lucide-react'
import { api } from '@/lib/api'
import { cn, timeAgo } from '@/lib/utils'
import { Button, Dialog } from './ui'

export interface SelfUpdateStatus {
  current: string
  latest?: string
  url?: string
  update_available: boolean
  method: 'docker' | 'systemd' | 'package' | 'binary'
  command?: string
  checked_at?: number
  error?: string
  can_self_update: boolean
  updating: boolean
  last_update?: { exit_code: number; at: number; log: string }
}

const key = ['self-update']

/** Header button for admins: lights up when a GuardTower release is waiting, and installs it. */
export function UpdateButton() {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const { data } = useQuery({
    queryKey: key,
    queryFn: () => api.get<SelfUpdateStatus>('/updates/guardtower'),
    // Poll quickly while an update runs; GuardTower restarts during it.
    refetchInterval: (q) => (q.state.data?.updating ? 3000 : 30 * 60_000),
    retry: true,
  })
  const [waitingFor, setWaitingFor] = useState('')
  const installed = !!waitingFor && data?.current === waitingFor && !data.updating
  // Once the new version answers, reload so the browser gets the new UI.
  useEffect(() => {
    if (!installed) return
    toast.success(`GuardTower ${waitingFor} is installed`, { description: 'Reloading…' })
    const t = setTimeout(() => location.reload(), 1200)
    return () => clearTimeout(t)
  }, [installed, waitingFor])
  const check = useMutation({
    mutationFn: () => api.post<SelfUpdateStatus>('/updates/guardtower/check', {}),
    onSuccess: (st) => {
      qc.setQueryData(key, st)
      if (st.error) toast.error('Could not check for updates', { description: st.error })
      else if (!st.update_available) toast.success(`GuardTower ${st.current} is the latest version`)
    },
    onError: (e) => toast.error((e as Error).message),
  })
  const install = useMutation({
    mutationFn: () => api.post<SelfUpdateStatus>('/updates/guardtower/install', {}),
    onSuccess: (st) => {
      qc.setQueryData(key, st)
      setWaitingFor(st.latest ?? '')
    },
    onError: (e) => toast.error((e as Error).message),
  })
  if (!data) return null
  const available = data.update_available && !!data.latest
  const failed = data.last_update && data.last_update.exit_code !== 0
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className={cn(
          'flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm',
          available || data.updating ? 'bg-accent font-medium text-accent-fg hover:opacity-90' : 'text-muted hover:bg-hover hover:text-fg',
        )}
        title={available ? `GuardTower ${data.latest} is available` : `GuardTower ${data.current}`}
      >
        {data.updating ? <Loader2 className="h-4 w-4 animate-spin" /> : <ArrowUpCircle className="h-4 w-4" />}
        <span className="hidden sm:inline">{data.updating ? 'Updating…' : available ? 'Update' : data.current}</span>
      </button>
      <Dialog
        open={open}
        onOpenChange={setOpen}
        title="Update GuardTower"
        description={
          <>
            You run <strong>{data.current}</strong>.{' '}
            {available ? (
              <>
                <strong>{data.latest}</strong> is available
                {data.url && (
                  <>
                    {' '}
                    (
                    <a href={data.url} target="_blank" rel="noreferrer">
                      release notes
                    </a>
                    )
                  </>
                )}
                .
              </>
            ) : (
              'It is the latest release.'
            )}
          </>
        }
        footer={
          <>
            <Button variant="ghost" onClick={() => check.mutate()} loading={check.isPending} disabled={data.updating}>
              <RefreshCw className="h-3.5 w-3.5" /> Check now
            </Button>
            {available && data.can_self_update && (
              <Button variant="primary" onClick={() => install.mutate()} loading={install.isPending || data.updating} disabled={data.updating}>
                {data.updating ? 'Updating…' : `Update to ${data.latest}`}
              </Button>
            )}
          </>
        }
      >
        <div className="space-y-3 text-sm">
          {data.updating && (
            <p className="flex items-center gap-2 rounded-lg bg-accent-soft p-3">
              <Loader2 className="h-4 w-4 shrink-0 animate-spin" /> Downloading and verifying the release, then restarting GuardTower. This page reloads when it's back, usually within a minute.
            </p>
          )}
          {available && !data.can_self_update && data.command && (
            <div>
              <p className="text-muted">This install can't update itself from here. Run this on the server:</p>
              <code className="mt-1 block overflow-x-auto whitespace-nowrap rounded bg-subtle px-2 py-1.5 font-mono text-xs">{data.command}</code>
            </div>
          )}
          {available && data.can_self_update && !data.updating && (
            <p className="text-muted">The update downloads the release from GitHub, checks its checksum and signature, keeps your data and settings, and restarts GuardTower. Signed-in users are briefly disconnected.</p>
          )}
          {!available && !data.updating && (
            <p className="flex items-center gap-2 text-muted">
              <CheckCircle2 className="h-4 w-4 text-ok" aria-hidden /> Up to date{data.checked_at ? `, checked ${timeAgo(data.checked_at)}` : ''}.
            </p>
          )}
          {data.error && <p className="text-danger">{data.error}</p>}
          {failed && !data.updating && (
            <details className="rounded-lg border border-border p-3">
              <summary className="cursor-pointer text-danger">The last update failed {timeAgo(data.last_update!.at)} (exit code {data.last_update!.exit_code}). GuardTower kept running the old version.</summary>
              <pre tabIndex={0} className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap font-mono text-xs text-muted">
                {data.last_update!.log}
              </pre>
            </details>
          )}
        </div>
      </Dialog>
    </>
  )
}
