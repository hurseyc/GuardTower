import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { onServerEvent } from './events'

/** Live-refresh rollout queries on server events. */
export function useRolloutEvents() {
  const qc = useQueryClient()
  useEffect(
    () =>
      onServerEvent((type) => {
        if (type !== 'rollout.update') return
        void qc.invalidateQueries({ queryKey: ['rollouts'] })
        void qc.invalidateQueries({ queryKey: ['rollout'] })
        void qc.invalidateQueries({ queryKey: ['sync'] })
        void qc.invalidateQueries({ queryKey: ['instance-config'] })
        void qc.invalidateQueries({ queryKey: ['upgrades'] })
      }),
    [qc],
  )
}
