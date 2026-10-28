import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { ResilienceReport } from '@/components/Resilience'

export function useResilience() {
  return useQuery({ queryKey: ['resilience'], queryFn: () => api.get<ResilienceReport>('/resilience'), refetchInterval: 60_000 })
}
