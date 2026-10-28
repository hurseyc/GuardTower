// Client-side mirror of the server's layer merge (internal/config/merge.go),
// used to show what a scope inherits. The server remains the source of truth.
import type { ConfigField, ConfigSection, LayerDoc } from './api'

export function keyOf(sectionKey: string, field: ConfigField, item: Record<string, unknown>): string {
  if (sectionKey === 'rewrites') return `${String(item.domain ?? '')} → ${String(item.answer ?? '')}`
  if (field.name === 'clients') return String(item.name ?? '')
  return String(item.url ?? item.name ?? '')
}

function strs(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
}

function items(v: unknown): Record<string, unknown>[] {
  return Array.isArray(v) ? v.filter((x): x is Record<string, unknown> => !!x && typeof x === 'object') : []
}

/** Merge layer docs (lowest first) into effective values. */
export function mergeLayers(sec: ConfigSection, docs: LayerDoc[]): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const d of docs) {
    for (const f of sec.fields) {
      if (!(f.name in d)) continue
      const v = d[f.name]
      if (f.kind === 'set') {
        const ops = (v ?? {}) as { add?: unknown; remove?: unknown }
        let cur = strs(out[f.name])
        for (const a of strs(ops.add)) if (!cur.includes(a)) cur.push(a)
        const rm = strs(ops.remove)
        cur = cur.filter((x) => !rm.includes(x))
        out[f.name] = cur
      } else if (f.kind === 'keyed') {
        const ops = (v ?? {}) as { upsert?: unknown; remove?: unknown }
        const m = new Map<string, Record<string, unknown>>()
        for (const it of items(out[f.name])) m.set(keyOf(sec.key, f, it), it)
        for (const it of items(ops.upsert)) m.set(keyOf(sec.key, f, it), it)
        for (const k of strs(ops.remove)) m.delete(k)
        out[f.name] = [...m.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([, it]) => it)
      } else {
        out[f.name] = v
      }
    }
  }
  return out
}

export function fmtValue(f: ConfigField, v: unknown): string {
  if (v === undefined) return '—'
  if (f.kind === 'bool') return v ? 'On' : 'Off'
  if (f.kind === 'duration_ms' && typeof v === 'number') return fmtDuration(v)
  if (Array.isArray(v)) {
    if (v.length === 0) return 'none'
    if (typeof v[0] === 'string') return (v as string[]).join(', ')
    return `${v.length} item${v.length === 1 ? '' : 's'}`
  }
  if (v && typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

export function fmtDuration(ms: number): string {
  const h = ms / 3600_000
  if (h >= 24 && h % 24 === 0) return `${h / 24} day${h === 24 ? '' : 's'}`
  if (h >= 1 && Number.isInteger(h)) return `${h} hour${h === 1 ? '' : 's'}`
  return `${ms} ms`
}
