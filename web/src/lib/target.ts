import type { RolloutTarget } from './api'

export function parseTarget(v: string | null): RolloutTarget {
  if (!v || v === 'fleet') return { kind: 'fleet' }
  const [k, rest] = v.split(':')
  if (k === 'tag') return { kind: 'tag', tag: rest }
  return { kind: k as RolloutTarget['kind'], id: Number(rest) }
}

export function targetParam(t: RolloutTarget): string {
  if (t.kind === 'fleet') return 'fleet'
  if (t.kind === 'tag') return `tag:${t.tag}`
  return `${t.kind}:${t.id}`
}
