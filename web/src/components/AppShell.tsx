import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { Command } from 'cmdk'
import { ArrowUpCircle, CircleHelp, FileCode2, KeyRound, LogOut, Menu, Plus, Rocket, ScrollText, Search, Server, X } from 'lucide-react'
import { api, type DocPage, type Overview } from '@/lib/api'
import { useSession } from '@/lib/sessionContext'
import { navRoutes, docForPath } from '@/lib/routes'
import { onServerEvent, useServerEvents } from '@/lib/events'
import { cn } from '@/lib/utils'
import { Kbd, Sheet, Skeleton } from './ui'
import { Markdown } from './Markdown'
import { Logo } from './Logo'
import { ChangePasswordDialog } from './ChangePasswordDialog'
import { ErrorBoundary } from './ErrorBoundary'
import { UpdateButton } from './UpdateButton'
import { ThemeButton } from './ThemeButton'
import { setTheme, themeIcons, themeOptions, useTheme } from '@/lib/theme'
import { useOnChange } from '@/lib/useOnChange'

export function AppShell() {
  const { user, signOut, can } = useSession()
  const loc = useLocation()
  const [mobileOpen, setMobileOpen] = useState(false)
  const [helpOpen, setHelpOpen] = useState(false)
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [pwOpen, setPwOpen] = useState(false)
  const { pref: theme } = useTheme()
  useServerEvents(!!user)
  const qc = useQueryClient()
  const seesFamily = !!user && (user.family_access || can('admin'))
  const navigate = useNavigate()
  // Live family alerts, only for people allowed to see Family DNS.
  useEffect(() => {
    if (!seesFamily) return
    return onServerEvent((type, data) => {
      if (type !== 'family.alert' || !data || typeof data !== 'object') return
      const d = data as { title: string; body: string }
      void qc.invalidateQueries({ queryKey: ['family'] })
      toast.warning(d.title, { description: d.body, action: { label: 'View', onClick: () => void navigate('/family') } })
    })
  }, [seesFamily, qc, navigate])

  useOnChange(loc.pathname, () => setMobileOpen(false))
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setPaletteOpen((v) => !v)
      }
      if (e.key === '?' && !(e.target instanceof HTMLInputElement) && !(e.target instanceof HTMLTextAreaElement)) {
        setHelpOpen(true)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const visible = navRoutes.filter((r) => (!r.minRole || can(r.minRole)) && (!r.family || user?.family_access || can('admin')))
  const sections = useMemo(() => {
    const m = new Map<string, typeof visible>()
    for (const r of visible) m.set(r.section, [...(m.get(r.section) ?? []), r])
    return [...m.entries()]
  }, [visible])

  const sidebar = (
    <nav className="flex h-full flex-col">
      <div className="flex h-14 items-center gap-2.5 px-4">
        <Logo className="h-7 w-7" />
        <div className="leading-tight">
          <div className="text-sm font-semibold tracking-tight">GuardTower</div>
          <div className="text-[10px] text-faint">AdGuard Home fleet manager</div>
        </div>
      </div>
      <div className="flex-1 space-y-5 overflow-y-auto px-3 py-3">
        {sections.map(([section, routes]) => (
          <div key={section}>
            <div className="mb-1.5 px-2 text-[10px] font-semibold uppercase tracking-wider text-faint">{section}</div>
            <div className="space-y-0.5">
              {routes.map((r) => (
                <NavLink
                  key={r.path}
                  to={r.path}
                  end={r.path === '/'}
                  className={({ isActive }) =>
                    cn(
                      'group flex items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-sm transition-colors',
                      isActive ? 'bg-accent-soft font-medium text-accent' : 'text-muted hover:bg-hover hover:text-fg',
                    )
                  }
                >
                  <r.icon className="h-4 w-4 shrink-0" />
                  {r.label}
                </NavLink>
              ))}
            </div>
          </div>
        ))}
      </div>
      <FleetPulse />
    </nav>
  )

  return (
    <div className="flex h-full">
      <aside className="hidden w-60 shrink-0 border-r border-border bg-elevated lg:block">{sidebar}</aside>
      {mobileOpen && (
        <div className="fixed inset-0 z-40 lg:hidden">
          <div className="absolute inset-0 bg-black/50" onClick={() => setMobileOpen(false)} />
          <aside className="absolute inset-y-0 left-0 w-64 border-r border-border bg-elevated">{sidebar}</aside>
        </div>
      )}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 shrink-0 items-center gap-3 border-b border-border bg-elevated/80 px-4 backdrop-blur">
          <button className="rounded-md p-1.5 text-muted hover:bg-hover lg:hidden" onClick={() => setMobileOpen(true)} aria-label="Open menu">
            <Menu className="h-5 w-5" />
          </button>
          <button
            onClick={() => setPaletteOpen(true)}
            className="flex h-9 w-full max-w-sm items-center gap-2 rounded-lg border border-border bg-bg px-3 text-sm text-faint hover:border-border-strong"
          >
            <Search className="h-4 w-4" />
            <span className="flex-1 text-left">Search instances, pages, docs…</span>
            <Kbd>⌘K</Kbd>
          </button>
          <div className="ml-auto flex items-center gap-1">
            {can('admin') && <UpdateButton />}
            <ThemeButton />
            <button
              onClick={() => setHelpOpen(true)}
              className="flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm text-muted hover:bg-hover hover:text-fg"
              title="Help for this page (?)"
            >
              <CircleHelp className="h-4 w-4" />
              <span className="hidden sm:inline">Help</span>
            </button>
            <DropdownMenu.Root>
              <DropdownMenu.Trigger className="ml-1 flex items-center gap-2 rounded-lg px-2 py-1.5 hover:bg-hover">
                <span className="flex h-7 w-7 items-center justify-center rounded-full bg-accent-soft text-xs font-semibold uppercase text-accent">
                  {user?.username.slice(0, 2)}
                </span>
                <span className="hidden text-left leading-tight sm:block">
                  <span className="block text-xs font-medium">{user?.username}</span>
                  <span className="block text-[10px] capitalize text-faint">{user?.role}</span>
                </span>
              </DropdownMenu.Trigger>
              <DropdownMenu.Portal>
                <DropdownMenu.Content align="end" sideOffset={6} className="z-50 min-w-52 rounded-xl border border-border bg-elevated p-1 shadow-pop">
                  <div className="px-2.5 py-2 text-xs text-faint">Theme</div>
                  <div className="mb-1 grid grid-cols-3 gap-1 px-1">
                    {themeOptions.map(({ value: t, label }) => {
                      const Icon = themeIcons[t]
                      return (
                        <button
                          key={t}
                          onClick={() => setTheme(t)}
                          aria-pressed={theme === t}
                          className={cn(
                            'flex flex-col items-center gap-1 rounded-lg py-2 text-[10px]',
                            theme === t ? 'bg-accent-soft text-accent' : 'text-muted hover:bg-hover',
                          )}
                        >
                          <Icon className="h-4 w-4" />
                          {label}
                        </button>
                      )
                    })}
                  </div>
                  <DropdownMenu.Separator className="my-1 h-px bg-border" />
                  {!user?.oidc && (
                    <DropdownMenu.Item
                      onSelect={() => setPwOpen(true)}
                      className="flex cursor-pointer items-center gap-2 rounded-lg px-2.5 py-2 text-sm outline-none data-[highlighted]:bg-hover"
                    >
                      <KeyRound className="h-4 w-4" /> Change password
                    </DropdownMenu.Item>
                  )}
                  <DropdownMenu.Item
                    onSelect={() => void signOut()}
                    className="flex cursor-pointer items-center gap-2 rounded-lg px-2.5 py-2 text-sm text-danger outline-none data-[highlighted]:bg-hover"
                  >
                    <LogOut className="h-4 w-4" /> Sign out
                  </DropdownMenu.Item>
                </DropdownMenu.Content>
              </DropdownMenu.Portal>
            </DropdownMenu.Root>
          </div>
        </header>
        <main id="main" tabIndex={0} className="min-h-0 flex-1 overflow-y-auto focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
          <ErrorBoundary resetKey={loc.pathname}>
            <Outlet />
          </ErrorBoundary>
        </main>
      </div>
      <HelpDrawer open={helpOpen} onOpenChange={setHelpOpen} />
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
      <ChangePasswordDialog open={pwOpen} onOpenChange={setPwOpen} />
    </div>
  )
}

/** Compact live fleet health in the sidebar footer. */
function FleetPulse() {
  const { data } = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview'), refetchInterval: 30_000 })
  if (!data) return null
  const { online, instances } = data.summary
  const all = instances > 0 && online === instances
  return (
    <div className="m-3 rounded-xl border border-border bg-subtle p-3">
      <div className="flex items-center gap-2 text-xs">
        <span className={cn('h-2 w-2 rounded-full', instances === 0 ? 'bg-faint' : all ? 'bg-ok gt-pulse' : 'bg-warn')} />
        <span className="font-medium">{instances === 0 ? 'No instances yet' : all ? 'All instances online' : `${instances - online} offline`}</span>
      </div>
      {instances > 0 && (
        <div className="mt-2 flex gap-1">
          {data.instances.map((i) => (
            <span
              key={i.id}
              title={`${i.name}: ${i.status?.online ? 'online' : 'offline'}`}
              className={cn('h-1.5 flex-1 rounded-full', i.status?.online ? 'bg-ok' : 'bg-danger')}
            />
          ))}
        </div>
      )}
    </div>
  )
}

/** Contextual help: the doc page mapped to the current route. */
function HelpDrawer({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const loc = useLocation()
  const slug = docForPath(loc.pathname)
  const { data, isLoading } = useQuery({
    queryKey: ['doc', slug],
    queryFn: () => api.get<DocPage>(`/docs/${slug}`),
    enabled: open,
  })
  return (
    <Sheet open={open} onOpenChange={onOpenChange} title="Help for this page">
      {isLoading ? (
        <div className="space-y-3">
          <Skeleton className="h-6 w-2/3" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-5/6" />
        </div>
      ) : data ? (
        <>
          <Markdown source={data.body ?? ''} baseSlug={slug} onNavigate={() => onOpenChange(false)} />
          <div className="mt-8 border-t border-border pt-4 text-xs text-faint">
            <NavLink to={`/docs/${slug}`} onClick={() => onOpenChange(false)} className="text-accent hover:underline">
              Open in documentation →
            </NavLink>
          </div>
        </>
      ) : (
        <p className="text-sm text-muted">No help page for this screen yet.</p>
      )}
    </Sheet>
  )
}

function CommandPalette({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const nav = useNavigate()
  const { can, user } = useSession()
  const { data: overview } = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview'), enabled: open })
  const { data: docs } = useQuery({ queryKey: ['docs-index'], queryFn: () => api.get<DocPage[]>('/docs'), enabled: open })
  const go = (to: string) => {
    onOpenChange(false)
    nav(to)
  }
  if (!open) return null
  const item = 'flex cursor-pointer items-center gap-2.5 rounded-lg px-3 py-2 text-sm aria-selected:bg-hover'
  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/50 p-4 pt-[12vh]" onClick={() => onOpenChange(false)}>
      <Command
        className="w-full max-w-xl overflow-hidden rounded-2xl border border-border bg-elevated shadow-pop"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.key === 'Escape' && onOpenChange(false)}
        label="Command palette"
      >
        <div className="flex items-center gap-2 border-b border-border px-4">
          <Search className="h-4 w-4 text-faint" />
          <Command.Input autoFocus placeholder="Type a command or search…" className="h-12 flex-1 bg-transparent text-sm outline-none placeholder:text-faint" />
          <button onClick={() => onOpenChange(false)} className="text-faint hover:text-fg" aria-label="Close">
            <X className="h-4 w-4" />
          </button>
        </div>
        <Command.List className="max-h-[60vh] overflow-y-auto p-2">
          <Command.Empty className="px-3 py-6 text-center text-sm text-muted">No results.</Command.Empty>
          <Command.Group heading="Actions" className="text-[10px] font-semibold uppercase tracking-wider text-faint [&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5">
            {[
              { label: 'Add an instance', to: '/instances?add=1', icon: Plus, role: 'admin' as const },
              { label: 'Review & apply pending changes', to: '/changes', icon: Rocket, role: 'operator' as const },
              { label: 'Plan an AdGuard Home upgrade', to: '/upgrades', icon: ArrowUpCircle, role: 'operator' as const },
              { label: 'Import or export settings (YAML)', to: '/config', icon: FileCode2, role: 'viewer' as const },
              { label: 'Connect Proxmox', to: '/hosts', icon: Server, role: 'admin' as const },
              { label: 'Search the query log', to: '/logs', icon: ScrollText, role: 'viewer' as const },
            ]
              .filter((a) => can(a.role))
              .map((a) => (
                <Command.Item key={a.label} value={`action ${a.label}`} onSelect={() => go(a.to)} className={cn(item, 'normal-case tracking-normal text-fg')}>
                  <a.icon className="h-4 w-4 text-muted" /> {a.label}
                </Command.Item>
              ))}
          </Command.Group>
          <Command.Group heading="Theme" className="text-[10px] font-semibold uppercase tracking-wider text-faint [&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5">
            {themeOptions.map(({ value, label }) => {
              const Icon = themeIcons[value]
              return (
                <Command.Item
                  key={value}
                  value={`theme ${label} ${value === 'system' ? 'system auto' : ''} dark mode light mode`}
                  onSelect={() => {
                    setTheme(value)
                    onOpenChange(false)
                  }}
                  className={cn(item, 'normal-case tracking-normal text-fg')}
                >
                  <Icon className="h-4 w-4 text-muted" /> Theme: {label}
                  {value === 'system' && <span className="ml-auto text-xs text-faint">follow the system</span>}
                </Command.Item>
              )
            })}
          </Command.Group>
          <Command.Group heading="Pages" className="text-[10px] font-semibold uppercase tracking-wider text-faint [&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5">
            {navRoutes
              .filter((r) => (!r.minRole || can(r.minRole)) && (!r.family || user?.family_access || can('admin')))
              .map((r) => (
                <Command.Item key={r.path} value={`page ${r.label}`} onSelect={() => go(r.path)} className={cn(item, 'normal-case tracking-normal text-fg')}>
                  <r.icon className="h-4 w-4 text-muted" /> {r.label}
                </Command.Item>
              ))}
          </Command.Group>
          {overview && overview.instances.length > 0 && (
            <Command.Group heading="Instances" className="text-[10px] font-semibold uppercase tracking-wider text-faint [&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5">
              {overview.instances.map((i) => (
                <Command.Item key={i.id} value={`instance ${i.name} ${i.url} ${i.tags.join(' ')}`} onSelect={() => go(`/instances/${i.id}`)} className={cn(item, 'normal-case tracking-normal text-fg')}>
                  <span className={cn('h-2 w-2 rounded-full', i.status?.online ? 'bg-ok' : 'bg-danger')} />
                  {i.name}
                  <span className="ml-auto font-mono text-xs text-faint">{i.url}</span>
                </Command.Item>
              ))}
            </Command.Group>
          )}
          {docs && (
            <Command.Group heading="Documentation" className="text-[10px] font-semibold uppercase tracking-wider text-faint [&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5">
              {docs.map((d) => (
                <Command.Item key={d.slug} value={`doc ${d.title} ${d.summary} ${d.section}`} onSelect={() => go(`/docs/${d.slug}`)} className={cn(item, 'normal-case tracking-normal text-fg')}>
                  <CircleHelp className="h-4 w-4 text-muted" />
                  {d.title}
                  <span className="ml-auto text-xs text-faint">{d.section}</span>
                </Command.Item>
              ))}
            </Command.Group>
          )}
        </Command.List>
      </Command>
    </div>
  )
}

export function PageHeader({ title, description, actions, children }: { title: string; description?: ReactNode; actions?: ReactNode; children?: ReactNode }) {
  return (
    <div className="border-b border-border bg-elevated/40 px-6 py-5">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
          {description && <p className="mt-1 text-sm text-muted">{description}</p>}
        </div>
        {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
      </div>
      {children}
    </div>
  )
}
