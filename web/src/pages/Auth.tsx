import { useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ShieldCheck } from 'lucide-react'
import { api, type User } from '@/lib/api'
import { useSession } from '@/lib/sessionContext'
import { Button, Card, Field, Input } from '@/components/ui'
import { Logo } from '@/components/Logo'

function AuthLayout({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  return (
    <div className="relative flex min-h-full items-center justify-center overflow-hidden px-4 py-12">
      <div className="pointer-events-none absolute inset-0 opacity-60 [background:radial-gradient(600px_circle_at_20%_10%,var(--accent-soft),transparent_60%),radial-gradient(500px_circle_at_90%_90%,color-mix(in_srgb,var(--info)_15%,transparent),transparent_60%)]" />
      <div className="relative w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center text-center">
          <Logo className="h-12 w-12" />
          <h1 className="mt-4 text-2xl font-semibold tracking-tight">{title}</h1>
          <p className="mt-1 text-sm text-muted">{subtitle}</p>
        </div>
        <Card className="p-6">{children}</Card>
        <p className="mt-6 text-center text-[11px] text-faint">GuardTower is not affiliated with AdGuard Software Ltd.</p>
      </div>
    </div>
  )
}

export function SetupPage() {
  const { signIn } = useSession()
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    setError('')
    if (password !== confirm) return setError('Passwords do not match')
    setBusy(true)
    try {
      const r = await api.post<{ user: User; csrf_token: string }>('/setup', { username, password })
      signIn(r.user, r.csrf_token)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthLayout title="Welcome to GuardTower" subtitle="Create the administrator account to get started.">
      <form className="space-y-4" onSubmit={(e) => (e.preventDefault(), void submit())}>
        <Field label="Admin username">
          <Input autoFocus autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} />
        </Field>
        <Field label="Password" hint="At least 8 characters. A passphrase works well.">
          <Input type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <Field label="Confirm password" error={error}>
          <Input type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
        </Field>
        <Button type="submit" variant="primary" className="w-full justify-center" loading={busy}>
          <ShieldCheck className="h-4 w-4" /> Create admin & continue
        </Button>
        <p className="text-xs text-faint">
          GuardTower starts in <strong className="text-muted">observe mode</strong>: it reads your AdGuard Home instances but changes nothing until you
          explicitly apply a plan.
        </p>
      </form>
    </AuthLayout>
  )
}

// Must match internal/demo (User, Password).
const demoLogin = { username: 'demo', password: 'guardtower-demo' }

export function LoginPage() {
  const { signIn } = useSession()
  const { data: meta } = useQuery({
    queryKey: ['meta'],
    queryFn: () => api.get<{ version: string; features?: Record<string, boolean>; sso?: { label: string; url: string } }>('/meta'),
    staleTime: Infinity,
  })
  const [ssoError] = useState(() => new URLSearchParams(window.location.search).get('sso_error') ?? '')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    setError('')
    setBusy(true)
    try {
      const r = await api.post<{ user: User; csrf_token: string }>('/auth/login', { username, password })
      signIn(r.user, r.csrf_token)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <AuthLayout title="Sign in" subtitle="Fleet manager for AdGuard Home">
      {meta?.sso && (
        <div className="mb-5 space-y-3">
          <Button variant="primary" className="w-full justify-center" onClick={() => window.location.assign(meta.sso!.url)}>
            {meta.sso.label}
          </Button>
          {ssoError && (
            <p role="alert" className="text-sm text-danger">
              {ssoError}
            </p>
          )}
          <div className="flex items-center gap-3 text-xs text-faint">
            <span className="h-px flex-1 bg-border" /> or with a local account <span className="h-px flex-1 bg-border" />
          </div>
        </div>
      )}
      <form className="space-y-4" onSubmit={(e) => (e.preventDefault(), void submit())}>
        <Field label="Username">
          <Input autoFocus={!meta?.sso} autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} />
        </Field>
        <Field label="Password" error={error}>
          <Input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <Button type="submit" variant={meta?.sso ? 'secondary' : 'primary'} className="w-full justify-center" loading={busy}>
          Sign in
        </Button>
        {meta?.features?.demo && (
          <div className="rounded-lg border border-border bg-accent-soft p-3 text-sm">
            <p>
              This is a <strong>demo</strong> with a simulated fleet. Sign in as <code className="font-mono">{demoLogin.username}</code> /{' '}
              <code className="font-mono">{demoLogin.password}</code>.
            </p>
            <Button
              type="button"
              className="mt-2"
              onClick={() => {
                setUsername(demoLogin.username)
                setPassword(demoLogin.password)
              }}
            >
              Fill in demo account
            </Button>
          </div>
        )}
      </form>
    </AuthLayout>
  )
}
