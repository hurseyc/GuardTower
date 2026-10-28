import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

type Handler = (type: string, data: unknown) => void
const handlers = new Set<Handler>()

/** Subscribe to server-sent events; returns an unsubscribe function. */
export function onServerEvent(h: Handler) {
  handlers.add(h)
  return () => {
    handlers.delete(h)
  }
}

const EVENT_TYPES = [
  'instance.status',
  'instance.availability',
  'querylog.rows',
  'rollout.update',
  'drift.detected',
  'upgrade.update',
  'family.alert',
  'sync.changed',
]

/** One EventSource for the whole app; invalidates queries on relevant events. */
export function useServerEvents(enabled: boolean) {
  const qc = useQueryClient()
  useEffect(() => {
    if (!enabled) return
    let es: EventSource | null = null
    let retry: ReturnType<typeof setTimeout> | undefined
    let statusTimer: ReturnType<typeof setTimeout> | undefined
    const connect = () => {
      es = new EventSource('/api/v1/events')
      for (const t of EVENT_TYPES) {
        es.addEventListener(t, (ev) => {
          let data: unknown = null
          try {
            data = JSON.parse((ev as MessageEvent).data)
          } catch {
            /* ignore */
          }
          handlers.forEach((h) => h(t, data))
          if (t === 'instance.status') {
            // Coalesce bursts (one event per instance per poll).
            clearTimeout(statusTimer)
            statusTimer = setTimeout(() => {
              void qc.invalidateQueries({ queryKey: ['overview'] })
              void qc.invalidateQueries({ queryKey: ['instances'] })
              void qc.invalidateQueries({ queryKey: ['upgrades'] })
              void qc.invalidateQueries({ queryKey: ['resilience'] })
            }, 300)
          }
          if (t === 'instance.availability' && data && typeof data === 'object') {
            const d = data as { name: string; online: boolean; error?: string }
            if (d.online) toast.success(`${d.name} is back online`)
            else toast.error(`${d.name} went offline`, { description: d.error })
          }
          if (t === 'rollout.update') void qc.invalidateQueries({ queryKey: ['rollouts'] })
          if (t === 'sync.changed') void qc.invalidateQueries({ queryKey: ['sync'] })
          if (t === 'drift.detected' && data && typeof data === 'object') {
            const d = data as { name: string; sections: string[] }
            toast.warning(`${d.name} was changed outside GuardTower`, { description: `Drifted: ${d.sections.join(', ')}` })
            void qc.invalidateQueries({ queryKey: ['sync'] })
          }
          if (t === 'upgrade.update') void qc.invalidateQueries({ queryKey: ['upgrades'] })
        })
      }
      es.onerror = () => {
        es?.close()
        retry = setTimeout(connect, 5000)
      }
    }
    connect()
    return () => {
      clearTimeout(retry)
      clearTimeout(statusTimer)
      es?.close()
    }
  }, [enabled, qc])
}
