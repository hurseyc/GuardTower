import { useMemo } from 'react'
import type { Overview, RolloutTarget } from '@/lib/api'
import { Select } from './ui'

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

/** Picks the whole fleet, a group, a tag or one instance. */
export function TargetSelect({ value, onChange, overview, className = 'w-64' }: { value: RolloutTarget; onChange: (t: RolloutTarget) => void; overview?: Overview; className?: string }) {
  const tags = useMemo(() => [...new Set((overview?.instances ?? []).flatMap((i) => i.tags))].sort(), [overview])
  return (
    <Select value={targetParam(value)} onChange={(e) => onChange(parseTarget(e.target.value))} className={className}>
      <option value="fleet">Whole fleet</option>
      {overview?.groups.length ? (
        <optgroup label="Groups">
          {overview.groups.map((g) => (
            <option key={g.id} value={`group:${g.id}`}>
              {g.name}
            </option>
          ))}
        </optgroup>
      ) : null}
      {tags.length ? (
        <optgroup label="Tags">
          {tags.map((t) => (
            <option key={t} value={`tag:${t}`}>
              #{t}
            </option>
          ))}
        </optgroup>
      ) : null}
      <optgroup label="Single instance">
        {overview?.instances.map((i) => (
          <option key={i.id} value={`instance:${i.id}`}>
            {i.name}
          </option>
        ))}
      </optgroup>
    </Select>
  )
}
