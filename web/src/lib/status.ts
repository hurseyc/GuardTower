import { Ban, CheckCircle2, ShieldAlert, ShieldCheck, Shuffle, Baby, Search, type LucideIcon } from 'lucide-react'
import type { QueryStatus } from './api'

/** Query status presentation: always an icon + label, never color alone. */
export const statusMeta: Record<QueryStatus, { label: string; icon: LucideIcon; tone: 'ok' | 'danger' | 'warn' | 'info' | 'neutral' }> = {
  allowed: { label: 'Allowed', icon: CheckCircle2, tone: 'neutral' },
  allowlisted: { label: 'Allowlisted', icon: ShieldCheck, tone: 'ok' },
  blocked: { label: 'Blocked', icon: Ban, tone: 'danger' },
  safebrowsing: { label: 'Safe Browsing', icon: ShieldAlert, tone: 'danger' },
  parental: { label: 'Parental', icon: Baby, tone: 'danger' },
  safesearch: { label: 'Safe Search', icon: Search, tone: 'info' },
  rewritten: { label: 'Rewritten', icon: Shuffle, tone: 'info' },
}

export const allStatuses = Object.keys(statusMeta) as QueryStatus[]
