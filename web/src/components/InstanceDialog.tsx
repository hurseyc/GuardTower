import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { CheckCircle2, PlugZap, XCircle } from 'lucide-react'
import { api, type Group, type Host, type Instance, type Runtime } from '@/lib/api'
import { Button, Dialog, Field, Input, Select, Switch, Textarea } from './ui'

const runtimes: { value: Runtime; label: string }[] = [
  { value: 'unknown', label: 'Not specified' },
  { value: 'lxc', label: 'Proxmox LXC' },
  { value: 'docker', label: 'Docker container' },
  { value: 'vm', label: 'Virtual machine' },
  { value: 'baremetal', label: 'Bare metal / service' },
  { value: 'router', label: 'Router (OpenWrt etc.)' },
  { value: 'homeassistant', label: 'Home Assistant add-on' },
]

interface FormState {
  name: string
  url: string
  username: string
  password: string
  tls_skip_verify: boolean
  ca_pem: string
  dns_address: string
  group_id: number
  host_id: number
  runtime: Runtime
  is_canary: boolean
  enabled: boolean
  tags: string
}

const empty: FormState = {
  name: '',
  url: '',
  username: '',
  password: '',
  tls_skip_verify: false,
  ca_pem: '',
  dns_address: '',
  group_id: 0,
  host_id: 0,
  runtime: 'unknown',
  is_canary: false,
  enabled: true,
  tags: '',
}

type TestResult = { ok: boolean; error?: string; version?: string; latency_ms?: number; can_autoupdate?: boolean }

export function InstanceDialog({
  open,
  onOpenChange,
  instance,
  groups,
  hosts,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  instance?: Instance
  groups: Group[]
  hosts: Host[]
}) {
  const qc = useQueryClient()
  const [f, setF] = useState<FormState>(empty)
  const [test, setTest] = useState<TestResult | null>(null)
  const [testing, setTesting] = useState(false)
  const [showAdvanced, setShowAdvanced] = useState(false)

  useEffect(() => {
    if (!open) return
    setTest(null)
    if (instance) {
      setF({
        ...empty,
        name: instance.name,
        url: instance.url,
        username: instance.username,
        tls_skip_verify: instance.tls_skip_verify,
        ca_pem: instance.ca_pem,
        dns_address: instance.dns_address,
        group_id: instance.group_id ?? 0,
        host_id: instance.host_id ?? 0,
        runtime: instance.runtime,
        is_canary: instance.is_canary,
        enabled: instance.enabled,
        tags: instance.tags.join(', '),
      })
      setShowAdvanced(instance.tls_skip_verify || !!instance.ca_pem || !!instance.dns_address)
    } else {
      setF(empty)
      setShowAdvanced(false)
    }
  }, [open, instance])

  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => setF((s) => ({ ...s, [k]: v }))
  const payload = () => ({
    name: f.name,
    url: f.url,
    username: f.username,
    // Empty password on edit means "keep the stored one".
    password: instance && f.password === '' ? null : f.password,
    tls_skip_verify: f.tls_skip_verify,
    ca_pem: f.ca_pem,
    dns_address: f.dns_address,
    group_id: Number(f.group_id),
    host_id: Number(f.host_id),
    runtime: f.runtime,
    is_canary: f.is_canary,
    enabled: f.enabled,
    tags: f.tags.split(',').map((t) => t.trim()).filter(Boolean),
  })

  const runTest = async () => {
    setTesting(true)
    try {
      setTest(await api.post<TestResult>('/instances/test', { id: instance?.id ?? 0, ...payload() }))
    } catch (e) {
      setTest({ ok: false, error: (e as Error).message })
    } finally {
      setTesting(false)
    }
  }

  const save = useMutation({
    mutationFn: () => (instance ? api.put<Instance>(`/instances/${instance.id}`, payload()) : api.post<Instance>('/instances', payload())),
    onSuccess: (inst) => {
      toast.success(instance ? `Saved ${inst.name}` : `Added ${inst.name}`)
      void qc.invalidateQueries({ queryKey: ['overview'] })
      void qc.invalidateQueries({ queryKey: ['instances'] })
      void qc.invalidateQueries({ queryKey: ['instance', inst.id] })
      onOpenChange(false)
    },
    onError: (e) => toast.error((e as Error).message),
  })

  const isHTTPS = f.url.trim().toLowerCase().startsWith('https')

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      wide
      title={instance ? `Edit ${instance.name}` : 'Add AdGuard Home instance'}
      description="GuardTower connects to the AdGuard Home web/API port with the same username and password you use for its web interface."
      footer={
        <>
          <Button variant="outline" onClick={runTest} loading={testing} disabled={!f.url}>
            <PlugZap className="h-4 w-4" /> Test connection
          </Button>
          <div className="flex-1" />
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()} disabled={!f.name || !f.url}>
            {instance ? 'Save changes' : 'Add instance'}
          </Button>
        </>
      }
    >
      <form className="grid gap-4 sm:grid-cols-2" onSubmit={(e) => (e.preventDefault(), save.mutate())}>
        <Field label="Name" hint="A short, unique label, e.g. agh-lxc-1">
          <Input autoFocus value={f.name} onChange={(e) => set('name', e.target.value)} placeholder="agh-lxc-1" />
        </Field>
        <Field label="URL" hint="Web interface address including port">
          <Input value={f.url} onChange={(e) => set('url', e.target.value)} placeholder="http://192.168.1.53:3000" className="font-mono" />
        </Field>
        <Field label="Username">
          <Input autoComplete="off" value={f.username} onChange={(e) => set('username', e.target.value)} />
        </Field>
        <Field label="Password" hint={instance?.has_password ? 'Leave blank to keep the saved password' : 'Stored encrypted (AES-256-GCM)'}>
          <Input type="password" autoComplete="new-password" value={f.password} onChange={(e) => set('password', e.target.value)} placeholder={instance?.has_password ? '••••••••' : ''} />
        </Field>
        <Field label="Settings group" hint="The group decides which settings this instance receives">
          <Select value={f.group_id} onChange={(e) => set('group_id', Number(e.target.value))}>
            <option value={0}>No group (base settings only)</option>
            {groups.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Runs on host" hint="Used for single-point-of-failure warnings">
          <Select value={f.host_id} onChange={(e) => set('host_id', Number(e.target.value))}>
            <option value={0}>Unassigned</option>
            {hosts.map((h) => (
              <option key={h.id} value={h.id}>
                {h.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Install type" hint="Determines how upgrades are performed">
          <Select value={f.runtime} onChange={(e) => set('runtime', e.target.value as Runtime)}>
            {runtimes.map((r) => (
              <option key={r.value} value={r.value}>
                {r.label}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Tags" hint="Comma-separated, e.g. lxc, upstairs">
          <Input value={f.tags} onChange={(e) => set('tags', e.target.value)} placeholder="lxc, primary" />
        </Field>
        <div className="flex items-center justify-between rounded-lg border border-border px-3 py-2.5 sm:col-span-1">
          <div>
            <div className="text-sm font-medium">Canary</div>
            <div className="text-xs text-faint">Receives changes first during rollouts</div>
          </div>
          <Switch checked={f.is_canary} onCheckedChange={(v) => set('is_canary', v)} />
        </div>
        <div className="flex items-center justify-between rounded-lg border border-border px-3 py-2.5">
          <div>
            <div className="text-sm font-medium">Enabled</div>
            <div className="text-xs text-faint">Disabled instances are not polled</div>
          </div>
          <Switch checked={f.enabled} onCheckedChange={(v) => set('enabled', v)} />
        </div>

        <div className="sm:col-span-2">
          <button type="button" onClick={() => setShowAdvanced((v) => !v)} className="text-xs font-medium text-accent hover:underline">
            {showAdvanced ? 'Hide' : 'Show'} TLS & DNS options
          </button>
        </div>
        {showAdvanced && (
          <>
            <div className="flex items-center justify-between rounded-lg border border-border px-3 py-2.5 sm:col-span-2">
              <div>
                <div className="text-sm font-medium">Skip TLS certificate verification</div>
                <div className="text-xs text-faint">For self-signed certificates. Prefer uploading your CA below.</div>
              </div>
              <Switch checked={f.tls_skip_verify} onCheckedChange={(v) => set('tls_skip_verify', v)} disabled={!isHTTPS && !f.tls_skip_verify} />
            </div>
            <Field label="Custom CA certificate (PEM)" className="sm:col-span-2" hint="Trust a private CA for this instance only">
              <Textarea rows={4} value={f.ca_pem} onChange={(e) => set('ca_pem', e.target.value)} placeholder="-----BEGIN CERTIFICATE-----" />
            </Field>
            <Field label="DNS address for health probes" className="sm:col-span-2" hint="Optional. Defaults to the instance host on port 53, e.g. 192.168.1.53:53">
              <Input value={f.dns_address} onChange={(e) => set('dns_address', e.target.value)} className="font-mono" placeholder="192.168.1.53:53" />
            </Field>
          </>
        )}
      </form>
      {test && (
        <div
          className={`mt-4 flex items-start gap-3 rounded-lg border p-3 text-sm ${
            test.ok ? 'border-transparent bg-accent-soft text-fg' : 'border-transparent bg-danger-soft text-fg'
          }`}
        >
          {test.ok ? <CheckCircle2 className="mt-0.5 h-4 w-4 text-ok" /> : <XCircle className="mt-0.5 h-4 w-4 text-danger" />}
          <div>
            {test.ok ? (
              <>
                Connected to AdGuard Home <strong>{test.version}</strong> in {test.latency_ms} ms.
                {test.can_autoupdate !== undefined && (
                  <span className="text-muted"> Built-in updater: {test.can_autoupdate ? 'available' : 'not available (typical for Docker)'}.</span>
                )}
              </>
            ) : (
              test.error
            )}
          </div>
        </div>
      )}
    </Dialog>
  )
}
