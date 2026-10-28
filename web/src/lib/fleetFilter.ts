import { useSearchParams } from 'react-router'

export const ranges = [
  { value: '1h', label: '1 hour' },
  { value: '6h', label: '6 hours' },
  { value: '24h', label: '24 hours' },
  { value: '7d', label: '7 days' },
  { value: '30d', label: '30 days' },
]

/** Shared filter state kept in the URL so views are linkable. */
export function useFleetFilter(defaultRange = '24h') {
  const [params, setParams] = useSearchParams()
  const range = params.get('range') ?? defaultRange
  const group = params.get('group') ?? ''
  const instance = params.get('instance') ?? ''
  const set = (k: string, v: string) => {
    const n = new URLSearchParams(params)
    if (v) n.set(k, v)
    else n.delete(k)
    setParams(n, { replace: true })
  }
  const qs = new URLSearchParams()
  if (group) qs.set('group', group)
  if (instance) qs.set('instance', instance)
  return { range, group, instance, set, scopeQuery: qs.toString() }
}
