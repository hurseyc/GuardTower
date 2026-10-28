import { useState, type ReactNode } from 'react'
import { ShieldCheck } from 'lucide-react'
import { api, type User } from '@/lib/api'
import { useSession } from '@/lib/session'
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
        <Field label="Password" hint="At least 10 characters. A passphrase works well.">
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

export function LoginPage() {
  const { signIn } = useSession()
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
      <form className="space-y-4" onSubmit={(e) => (e.preventDefault(), void submit())}>
        <Field label="Username">
          <Input autoFocus autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} />
        </Field>
        <Field label="Password" error={error}>
          <Input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <Button type="submit" variant="primary" className="w-full justify-center" loading={busy}>
          Sign in
        </Button>
      </form>
    </AuthLayout>
  )
}
