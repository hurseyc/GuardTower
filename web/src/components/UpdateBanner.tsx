import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowUpCircle, X } from 'lucide-react'
import { api } from '@/lib/api'

export interface SelfUpdateStatus {
  current: string
  latest?: string
  url?: string
  update_available: boolean
  method: 'docker' | 'systemd' | 'package' | 'binary'
  command?: string
  checked_at?: number
  error?: string
}

const dismissKey = 'gt.update.dismissed'

function readDismissed(): string {
  try {
    return localStorage.getItem(dismissKey) ?? ''
  } catch {
    return ''
  }
}

/** Tells admins a newer GuardTower release exists and how to install it. */
export function UpdateBanner() {
  const { data } = useQuery({
    queryKey: ['self-update'],
    queryFn: () => api.get<SelfUpdateStatus>('/updates/guardtower'),
    refetchInterval: 60 * 60_000,
  })
  const [dismissed, setDismissed] = useState(readDismissed)
  if (!data?.update_available || !data.latest || dismissed === data.latest) return null
  const latest = data.latest
  return (
    <div role="status" className="flex items-start gap-3 border-b border-border bg-accent-soft px-4 py-2 text-sm">
      <ArrowUpCircle className="mt-0.5 h-4 w-4 shrink-0 text-accent" aria-hidden />
      <div className="min-w-0 flex-1">
        <span className="font-medium">GuardTower {latest} is available</span>{' '}
        <span className="text-muted">(you run {data.current}).</span>{' '}
        {data.url && (
          <a href={data.url} target="_blank" rel="noreferrer" className="text-accent underline-offset-2 hover:underline">
            Release notes
          </a>
        )}
        {data.command && (
          <code className="mt-1 block overflow-x-auto whitespace-nowrap rounded bg-elevated px-2 py-1 font-mono text-xs">{data.command}</code>
        )}
      </div>
      <button
        type="button"
        aria-label="Hide until the next release"
        title="Hide until the next release"
        className="rounded p-1 text-muted hover:bg-hover"
        onClick={() => {
          try {
            localStorage.setItem(dismissKey, latest)
          } catch {
            /* private mode: hide for this page view only */
          }
          setDismissed(latest)
        }}
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  )
}
