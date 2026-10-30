import { useState } from 'react'
import { Link } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ArrowUpCircle, CheckCircle2, KeyRound, PlugZap, XCircle } from 'lucide-react'
import { api, type UpgradeConfig, type UpgradeDriver, type UpgradeOverview, type UpgradeTestResult } from '@/lib/api'
import { useSession } from '@/lib/sessionContext'
import { cn } from '@/lib/utils'
import { Badge, Button, Card, CardHeader, Field, Input, Select, Skeleton, Switch, Tip } from './ui'
import { detectPendingHelp, detectPendingLabel } from '@/lib/labels'

const driverOptions: { value: UpgradeDriver; label: string; hint: string }[] = [
  { value: 'auto', label: 'Automatic', hint: 'Docker if configured, then SSH, then AdGuard Home’s built-in updater.' },
  { value: 'builtin', label: 'Built-in updater', hint: 'For binary installs (Proxmox LXC, VM, bare metal). Always installs the latest version; no automatic rollback.' },
  { value: 'docker', label: 'Docker', hint: 'Recreates the container from a newer image through the Docker API (use a socket proxy). Rolls back automatically.' },
  { value: 'ssh', label: 'SSH', hint: 'Replaces the binary over SSH and keeps a backup. Rolls back automatically.' },
  { value: 'none', label: 'Never upgrade', hint: 'GuardTower leaves this instance’s version alone.' },
]

function TestOutcome({ r }: { r: UpgradeTestResult }) {
  return (
    <div className={cn('flex items-start gap-2 rounded-lg p-3 text-sm', r.ok ? 'bg-accent-soft' : 'bg-danger-soft')}>
      {r.ok ? <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-ok" /> : <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-danger" />}
      <span className="break-words">{r.message}</span>
    </div>
  )
}

/** How GuardTower upgrades this instance's AdGuard Home. */
export function UpgradeCard({ instanceId }: { instanceId: number }) {
  const { can } = useSession()
  const admin = can('admin')
  const qc = useQueryClient()
  const { data: cfg } = useQuery({ queryKey: ['upgrade-config', instanceId], queryFn: () => api.get<UpgradeConfig>(`/instances/${instanceId}/upgrade-config`) })
  const { data: ov } = useQuery({ queryKey: ['upgrades'], queryFn: () => api.get<UpgradeOverview>('/upgrades') })
  const state = ov?.instances.find((i) => i.instance_id === instanceId)
  if (!cfg) {
    return (
      <Card className="xl:col-span-3">
        <CardHeader title="Upgrades" />
        <Skeleton className="mx-5 mb-5 h-20" />
      </Card>
    )
  }
  return (
    <Card className="xl:col-span-3">
      <CardHeader
        title="Upgrades"
        description="How GuardTower upgrades AdGuard Home on this instance."
        actions={
          state?.update_available &&
          can('operator') && (
            <Link to={`/upgrades?target=instance:${instanceId}`}>
              <span className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-sm font-medium text-accent-fg">
                <ArrowUpCircle className="h-3.5 w-3.5" /> Upgrade to {state.target}
              </span>
            </Link>
          )
        }
      />
      <div className="px-5 pb-5">
        {state && (
          <div className="mb-4 flex flex-wrap items-center gap-2 text-sm">
            <span className="text-muted">Running</span>
            <span className="font-mono">{state.version || '—'}</span>
            {state.update_available ? <Badge tone="warn">{state.target} available</Badge> : state.up_to_date || !state.blocked ? <Badge tone="ok">up to date</Badge> : <Badge>{state.blocked}</Badge>}
            {state.driver && <Badge tone="info">via {driverOptions.find((d) => d.value === state.driver)?.label}</Badge>}
            {!state.driver && state.detect_pending && (
              <Tip content={detectPendingHelp}>
                <span className="text-xs text-muted underline decoration-dotted">{detectPendingLabel}</span>
              </Tip>
            )}
          </div>
        )}
        <UpgradeForm key={cfg.updated_at ?? 0} cfg={cfg} instanceId={instanceId} editable={admin} onSaved={() => void qc.invalidateQueries({ queryKey: ['upgrades'] })} />
      </div>
    </Card>
  )
}

function UpgradeForm({ cfg, instanceId, editable, onSaved }: { cfg: UpgradeConfig; instanceId: number; editable: boolean; onSaved: () => void }) {
  const qc = useQueryClient()
  const [c, setC] = useState(cfg)
  const [key, setKey] = useState<string | null>(null)
  const [dockerTest, setDockerTest] = useState<UpgradeTestResult | null>(null)
  const [sshTest, setSSHTest] = useState<UpgradeTestResult | null>(null)
  const input = () => ({ ...c, ssh_key: key })
  const save = useMutation({
    mutationFn: () => api.put<UpgradeConfig>(`/instances/${instanceId}/upgrade-config`, input()),
    onSuccess: (d) => {
      qc.setQueryData(['upgrade-config', instanceId], d)
      setKey(null)
      toast.success('Upgrade settings saved')
      onSaved()
    },
    onError: (e) => toast.error((e as Error).message),
  })
  const test = useMutation({
    mutationFn: (kind: 'docker' | 'ssh') => api.post<UpgradeTestResult>(`/instances/${instanceId}/upgrade-config/test`, { kind, config: input() }).then((r) => ({ kind, r })),
    onSuccess: ({ kind, r }) => (kind === 'docker' ? setDockerTest(r) : setSSHTest(r)),
    onError: (e) => toast.error((e as Error).message),
  })
  const pin = useMutation({
    mutationFn: (hostKey: string) => api.post(`/instances/${instanceId}/upgrade-config/pin`, { host_key: hostKey }),
    onSuccess: () => {
      toast.success('Host key trusted')
      setSSHTest(null)
      void qc.invalidateQueries({ queryKey: ['upgrade-config', instanceId] })
      onSaved()
    },
    onError: (e) => toast.error((e as Error).message),
  })
  const set = <K extends keyof UpgradeConfig>(k: K, v: UpgradeConfig[K]) => setC((x) => ({ ...x, [k]: v }))
  const setDocker = (k: keyof UpgradeConfig['docker'], v: string | boolean) => setC((x) => ({ ...x, docker: { ...x.docker, [k]: v } }))
  const setSSH = (k: keyof UpgradeConfig['ssh'], v: string | boolean | number) => setC((x) => ({ ...x, ssh: { ...x.ssh, [k]: v } }))
  const showDocker = c.driver === 'docker' || c.driver === 'auto'
  const showSSH = c.driver === 'ssh' || c.driver === 'auto'
  const dirty = JSON.stringify(c) !== JSON.stringify(cfg) || key !== null
  const unsavedSSH = c.ssh.host !== cfg.ssh.host || c.ssh.port !== cfg.ssh.port || key !== null

  return (
    <form className="space-y-5" onSubmit={(e) => (e.preventDefault(), save.mutate())}>
      <fieldset disabled={!editable} className="grid gap-4 sm:grid-cols-2">
        <Field label="Upgrade method" hint={driverOptions.find((d) => d.value === c.driver)?.hint}>
          <Select value={c.driver} onChange={(e) => set('driver', e.target.value as UpgradeDriver)}>
            {driverOptions.map((d) => (
              <option key={d.value} value={d.value}>
                {d.label}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Pin to version" hint="Optional, e.g. v0.107.79. GuardTower won't upgrade past it.">
          <Input value={c.pinned_version} onChange={(e) => set('pinned_version', e.target.value)} placeholder="no pin" />
        </Field>
      </fieldset>

      {showDocker && (
        <fieldset disabled={!editable} className="space-y-4 rounded-xl border border-border p-4">
          <legend className="px-1 text-sm font-semibold">Docker</legend>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Docker API endpoint" hint="Your socket proxy, e.g. tcp://nas.lan:2375 (see Setup → Upgrade methods).">
              <Input value={c.docker.endpoint} onChange={(e) => setDocker('endpoint', e.target.value)} placeholder="tcp://nas.lan:2375" />
            </Field>
            <Field label="Container name" hint="As shown by docker ps, e.g. adguardhome.">
              <Input value={c.docker.container} onChange={(e) => setDocker('container', e.target.value)} placeholder="adguardhome" />
            </Field>
            <Field label="Image repository" hint="Leave empty to keep the container's current repository (normally adguard/adguardhome).">
              <Input value={c.docker.image} onChange={(e) => setDocker('image', e.target.value)} placeholder="adguard/adguardhome" />
            </Field>
            <Field label="Config file inside the container" hint="Backed up before each upgrade and restored on rollback.">
              <Input value={c.docker.conf_path} onChange={(e) => setDocker('conf_path', e.target.value)} />
            </Field>
          </div>
          {c.docker.endpoint.startsWith('https://') && (
            <label className="flex items-center gap-2 text-sm">
              <Switch checked={c.docker.tls_skip_verify} onCheckedChange={(v) => setDocker('tls_skip_verify', v)} /> Skip TLS certificate verification
            </label>
          )}
          <div className="flex flex-wrap items-center gap-3">
            <Button type="button" size="sm" onClick={() => test.mutate('docker')} loading={test.isPending && test.variables === 'docker'} disabled={!c.docker.endpoint}>
              <PlugZap className="h-3.5 w-3.5" /> Test Docker connection
            </Button>
          </div>
          {dockerTest && <TestOutcome r={dockerTest} />}
        </fieldset>
      )}

      {showSSH && (
        <fieldset disabled={!editable} className="space-y-4 rounded-xl border border-border p-4">
          <legend className="px-1 text-sm font-semibold">SSH</legend>
          <div className="grid gap-4 sm:grid-cols-3">
            <Field label="Host">
              <Input value={c.ssh.host} onChange={(e) => setSSH('host', e.target.value)} placeholder="adguard-lxc.lan" />
            </Field>
            <Field label="Port">
              <Input type="number" min={1} max={65535} value={c.ssh.port} onChange={(e) => setSSH('port', Number(e.target.value))} />
            </Field>
            <Field label="User">
              <Input value={c.ssh.user} onChange={(e) => setSSH('user', e.target.value)} />
            </Field>
          </div>
          <Field label="Private key" hint={cfg.ssh.has_key && key === null ? 'A key is stored (encrypted). Paste a new one to replace it.' : 'OpenSSH or PEM private key without a passphrase. Stored encrypted.'}>
            <textarea
              className="h-24 w-full rounded-lg border border-border bg-elevated px-3 py-2 font-mono text-xs"
              value={key ?? ''}
              onChange={(e) => setKey(e.target.value)}
              placeholder={cfg.ssh.has_key ? '•••••• stored ••••••' : '-----BEGIN OPENSSH PRIVATE KEY-----'}
              spellCheck={false}
            />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="AdGuard Home directory">
              <Input value={c.ssh.agh_dir} onChange={(e) => setSSH('agh_dir', e.target.value)} />
            </Field>
            <Field label="Service name" hint="systemd, OpenRC or SysV service.">
              <Input value={c.ssh.service} onChange={(e) => setSSH('service', e.target.value)} />
            </Field>
          </div>
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={c.ssh.sudo} onCheckedChange={(v) => setSSH('sudo', v)} /> Use sudo (passwordless) for a non-root user
          </label>
          <div className="flex flex-wrap items-center gap-3 text-sm">
            <Button type="button" size="sm" onClick={() => test.mutate('ssh')} loading={test.isPending && test.variables === 'ssh'} disabled={!c.ssh.host}>
              <PlugZap className="h-3.5 w-3.5" /> Test SSH connection
            </Button>
            {cfg.ssh.host_key && !unsavedSSH ? (
              <span className="inline-flex items-center gap-1 text-muted">
                <KeyRound className="h-3.5 w-3.5 text-ok" /> Host key pinned
              </span>
            ) : (
              <span className="text-muted">The host key is pinned after you test the connection and trust it.</span>
            )}
          </div>
          {sshTest && <TestOutcome r={sshTest} />}
          {sshTest?.host_key && !sshTest.pinned && (
            <div className="space-y-2 rounded-lg border border-border bg-subtle p-3 text-sm">
              <p>
                The server presented host key <code className="font-mono text-xs">{sshTest.host_key.fingerprint}</code>. Check it matches the machine (<code className="font-mono text-xs">ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub</code>) before trusting it.
              </p>
              {unsavedSSH ? (
                <p className="text-warn">Save the SSH settings first, then test again to trust the key.</p>
              ) : (
                <Button type="button" size="sm" variant="primary" loading={pin.isPending} onClick={() => sshTest.host_key && pin.mutate(sshTest.host_key.key)}>
                  <KeyRound className="h-3.5 w-3.5" /> Trust this host key
                </Button>
              )}
            </div>
          )}
        </fieldset>
      )}

      {editable && (
        <div className="flex gap-2">
          <Button type="submit" variant="primary" disabled={!dirty} loading={save.isPending}>
            Save upgrade settings
          </Button>
          {dirty && (
            <Button type="button" variant="ghost" onClick={() => (setC(cfg), setKey(null))}>
              Discard
            </Button>
          )}
        </div>
      )}
    </form>
  )
}
