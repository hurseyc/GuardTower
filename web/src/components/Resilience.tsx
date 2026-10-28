import { AlertTriangle, Info, ShieldAlert, ShieldCheck, ShieldX } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Tip } from './ui'
import { scoreLabel } from '@/lib/labels'

export type Severity = 'info' | 'warn' | 'critical'
export type GroupScore = 'down' | 'spof' | 'at_risk' | 'unknown' | 'redundant'

export interface Finding {
  severity: Severity
  kind: 'single_member' | 'shared_host' | 'shared_power' | 'shared_site' | 'unplaced' | 'degraded' | 'down'
  group_id: number
  instance_ids: number[]
  host_id?: number
  message: string
  fix?: string
}

export interface GroupReport {
  group_id: number
  name: string
  score: GroupScore
  members: number
  online: number
  hosts: number
  findings: Finding[]
}

export interface ResilienceReport {
  groups: GroupReport[]
  spof_hosts: Record<string, number[]>
  instances: Record<string, Severity>
  summary: { redundant: number; at_risk: number; spof: number; down: number; unknown: number }
}

/** A small, quiet marker: amber for risks, red only when DNS is actually down. */
export function ScoreIcon({ score, className }: { score: GroupScore; className?: string }) {
  const cls = cn('h-4 w-4 shrink-0', className)
  switch (score) {
    case 'redundant':
      return <ShieldCheck className={cn(cls, 'text-ok')} aria-label="Redundant" />
    case 'down':
      return <ShieldX className={cn(cls, 'text-danger')} aria-label="Down" />
    case 'unknown':
      return <Info className={cn(cls, 'text-faint')} aria-label="Redundancy unknown" />
    default:
      return <ShieldAlert className={cn(cls, 'text-warn')} aria-label={scoreLabel[score]} />
  }
}

/** Icon with the findings as a tooltip; renders nothing when all is well. */
export function RiskMarker({ findings, className }: { findings: Finding[]; className?: string }) {
  const shown = findings.filter((f) => f.severity !== 'info')
  if (shown.length === 0) return null
  const worst = shown.some((f) => f.kind === 'down') ? 'down' : 'spof'
  return (
    <Tip
      content={
        <ul className="max-w-xs space-y-1 text-xs">
          {shown.map((f, i) => (
            <li key={i}>{f.message}</li>
          ))}
        </ul>
      }
    >
      <span className={cn('inline-flex', className)} tabIndex={0} aria-label={shown.map((f) => f.message).join(' ')}>
        <ScoreIcon score={worst} />
      </span>
    </Tip>
  )
}

export function FindingList({ findings, compact }: { findings: Finding[]; compact?: boolean }) {
  if (findings.length === 0) return null
  return (
    <ul className="space-y-2">
      {findings.map((f, i) => (
        <li
          key={i}
          className={cn(
            'flex items-start gap-2 rounded-lg p-2.5 text-sm',
            f.severity === 'info' ? 'bg-subtle text-muted' : f.kind === 'down' ? 'bg-danger-soft' : 'bg-warn-soft',
          )}
        >
          {f.severity === 'info' ? <Info className="mt-0.5 h-4 w-4 shrink-0" /> : <AlertTriangle className={cn('mt-0.5 h-4 w-4 shrink-0', f.kind === 'down' ? 'text-danger' : 'text-warn')} />}
          <span>
            {f.message}
            {!compact && f.fix && <span className="text-muted"> {f.fix}</span>}
          </span>
        </li>
      ))}
    </ul>
  )
}
