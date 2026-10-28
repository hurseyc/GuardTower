import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

const nf = new Intl.NumberFormat()
const cf = new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 })

export const fmtNum = (n: number) => nf.format(n)
export const fmtCompact = (n: number) => cf.format(n)
export const fmtPct = (n: number, digits = 1) => `${(Number.isFinite(n) ? n : 0).toFixed(digits)}%`

/** Relative time from a unix timestamp in seconds or milliseconds. */
export function timeAgo(ts?: number): string {
  if (!ts) return 'never'
  const ms = ts < 1e12 ? ts * 1000 : ts
  const s = Math.round((Date.now() - ms) / 1000)
  if (s < 5) return 'just now'
  if (s < 60) return `${s}s ago`
  const m = Math.round(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.round(m / 60)
  if (h < 48) return `${h}h ago`
  return `${Math.round(h / 24)}d ago`
}

export function fmtDateTime(ts: number): string {
  const ms = ts < 1e12 ? ts * 1000 : ts
  return new Date(ms).toLocaleString()
}

/** Stable colour for a string (used for group/tag chips when no colour is set). */
export const palette = ['#34d399', '#38bdf8', '#a78bfa', '#f472b6', '#fbbf24', '#fb923c', '#2dd4bf', '#818cf8']
export function colorFor(key: string, explicit?: string): string {
  if (explicit) return explicit
  let h = 0
  for (const c of key) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return palette[h % palette.length]
}

export function hostFromURL(u: string): string {
  try {
    return new URL(u).host
  } catch {
    return u
  }
}
