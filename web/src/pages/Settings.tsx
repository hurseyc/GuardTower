import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api, type CollectorState, type Overview, type SettingsResponse } from '@/lib/api'
import { fmtNum, timeAgo } from '@/lib/utils'
import { PageHeader } from '@/components/AppShell'
import { NotifyTargetsCard } from '@/components/NotifyTargets'
import { Badge, Button, Card, CardHeader, Field, Input, StatusDot, Switch, Table, Td, Th, Tip } from '@/components/ui'

function fmtBytes(n: number) {
  if (n < 1024) return `${n} B`
  const u = ['KB', 'MB', 'GB', 'TB']
  let i = -1
  do {
    n /= 1024
    i++
  } while (n >= 1024 && i < u.length - 1)
  return `${n.toFixed(n < 10 ? 1 : 0)} ${u[i]}`
}

export function SettingsPage() {
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings'], queryFn: () => api.get<SettingsResponse>('/settings') })
  const { data: states } = useQuery({ queryKey: ['collector'], queryFn: () => api.get<CollectorState[]>('/collector'), refetchInterval: 10_000 })
  const { data: overview } = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview') })
  const [raw, setRaw] = useState('')
  const [roll, setRoll] = useState('')
  useEffect(() => {
    if (data) {
      setRaw(data.settings['retention.querylog_days'])
      setRoll(data.settings['retention.rollup_days'])
    }
  }, [data])
  const save = useMutation({
    mutationFn: () => api.put<SettingsResponse>('/settings', { 'retention.querylog_days': raw, 'retention.rollup_days': roll }),
    onSuccess: (d) => {
      qc.setQueryData(['settings'], d)
      toast.success('Settings saved')
    },
    onError: (e) => toast.error((e as Error).message),
  })
  const observe = useMutation({
    mutationFn: (on: boolean) => api.put<SettingsResponse>('/settings', { 'mode.observe': on ? 'true' : 'false' }),
    onSuccess: (d) => {
      qc.setQueryData(['settings'], d)
      void qc.invalidateQueries({ queryKey: ['rollouts'] })
      toast.success(d.settings['mode.observe'] === 'true' ? 'Observe mode on: GuardTower will not change instances' : 'Observe mode off: changes can be applied')
    },
    onError: (e) => toast.error((e as Error).message),
  })
  const observing = data?.settings['mode.observe'] !== 'false'
  const stateById = new Map((states ?? []).map((s) => [s.instance_id, s]))

  return (
    <>
      <PageHeader title="Settings" description="Safety mode, notifications, data retention and collection health." />
      <div className="grid gap-6 p-6 xl:grid-cols-3">
        <Card className="xl:col-span-3">
          <CardHeader
            title="Observe mode"
            description="While on, GuardTower only watches: it collects logs and checks settings, but never applies changes or restores snapshots."
          />
          <div className="flex items-center gap-3 px-5 pb-5 text-sm">
            <Switch id="observe-mode" checked={observing} disabled={!data || observe.isPending} onCheckedChange={(v) => observe.mutate(v)} />
            <label htmlFor="observe-mode" className="font-medium">
              {observing ? 'On: read-only' : 'Off: changes can be applied from Review & apply'}
            </label>
          </div>
        </Card>
        <NotifyTargetsCard />
        <Card className="xl:col-span-1">
          <CardHeader title="Data retention" description="Older data is pruned automatically every hour." />
          <form className="space-y-4 px-5 pb-5" onSubmit={(e) => (e.preventDefault(), save.mutate())}>
            <Field label="Keep raw query logs for (days)" hint="Individual queries: search, top lists, per-client views. 1–3650.">
              <Input type="number" min={1} max={3650} value={raw} onChange={(e) => setRaw(e.target.value)} />
            </Field>
            <Field label="Keep hourly statistics for (days)" hint="Totals and charts for long ranges. Tiny; keep this long. 7–3650.">
              <Input type="number" min={7} max={3650} value={roll} onChange={(e) => setRoll(e.target.value)} />
            </Field>
            <Button type="submit" variant="primary" loading={save.isPending}>
              Save retention
            </Button>
            {data && (
              <div className="rounded-lg border border-border bg-subtle p-3 text-xs text-muted">
                Database size <strong className="text-fg">{fmtBytes(data.database_bytes)}</strong> · {fmtNum(data.querylog_rows)} stored queries
              </div>
            )}
          </form>
        </Card>
        <Card className="xl:col-span-2">
          <CardHeader title="Query-log collection" description="GuardTower pulls each instance's query log every 10 seconds." />
          <Table>
            <thead>
              <tr>
                <Th>Instance</Th>
                <Th>Last collected</Th>
                <Th>Newest query</Th>
                <Th className="text-right">Queries stored</Th>
                <Th>Status</Th>
              </tr>
            </thead>
            <tbody>
              {overview?.instances.map((i) => {
                const s = stateById.get(i.id)
                return (
                  <tr key={i.id}>
                    <Td>
                      <span className="inline-flex items-center gap-2 font-medium">
                        <StatusDot online={i.status?.online} />
                        {i.name}
                      </span>
                    </Td>
                    <Td className="text-sm text-muted">{s ? timeAgo(s.last_run_at) : 'never'}</Td>
                    <Td className="text-sm text-muted">{s?.last_ts ? timeAgo(s.last_ts) : '—'}</Td>
                    <Td className="text-right tabular-nums">{fmtNum(s?.rows_total ?? 0)}</Td>
                    <Td>
                      {!s ? (
                        <Badge>waiting</Badge>
                      ) : s.last_error ? (
                        <Tip content={s.last_error}>
                          <Badge tone="danger">error</Badge>
                        </Tip>
                      ) : (
                        <Badge tone="ok">collecting</Badge>
                      )}
                    </Td>
                  </tr>
                )
              })}
            </tbody>
          </Table>
        </Card>
      </div>
    </>
  )
}
