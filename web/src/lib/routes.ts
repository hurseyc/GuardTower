import type { LucideIcon } from 'lucide-react'
import {
  BarChart3,
  BookOpen,
  Boxes,
  History,
  LayoutDashboard,
  ScrollText,
  SlidersHorizontal,
  Server,
  Settings,
  ShieldCheck,
  Users,
} from 'lucide-react'
import type { Role } from './api'

/**
 * Every navigable page. `doc` is the help page (docs/<doc>.md) opened by the
 * "?" button; a unit test fails if a referenced doc does not exist, which keeps
 * the in-app wiki in step with the UI.
 */
export interface NavRoute {
  path: string
  label: string
  icon: LucideIcon
  doc: string
  section: 'Fleet' | 'Insights' | 'Change' | 'Family' | 'Admin' | 'Help'
  minRole?: Role
  family?: boolean
}

export const navRoutes: NavRoute[] = [
  { path: '/', label: 'Overview', icon: LayoutDashboard, doc: 'operation/dashboard', section: 'Fleet' },
  { path: '/instances', label: 'Instances', icon: ShieldCheck, doc: 'setup/adding-instances', section: 'Fleet' },
  { path: '/groups', label: 'Groups', icon: Boxes, doc: 'operation/groups', section: 'Fleet' },
  { path: '/hosts', label: 'Hosts & sites', icon: Server, doc: 'operation/hosts', section: 'Fleet' },
  { path: '/config', label: 'Configuration', icon: SlidersHorizontal, doc: 'operation/configuration', section: 'Change' },
  { path: '/analytics', label: 'Analytics', icon: BarChart3, doc: 'operation/analytics', section: 'Insights' },
  { path: '/logs', label: 'Query log', icon: ScrollText, doc: 'operation/query-log', section: 'Insights' },
  { path: '/users', label: 'Users', icon: Users, doc: 'admin/users', section: 'Admin', minRole: 'admin' },
  { path: '/audit', label: 'Audit log', icon: History, doc: 'admin/audit-log', section: 'Admin', minRole: 'admin' },
  { path: '/settings', label: 'Settings', icon: Settings, doc: 'admin/settings', section: 'Admin', minRole: 'admin' },
  { path: '/docs', label: 'Documentation', icon: BookOpen, doc: 'index', section: 'Help' },
]

/** Extra (non-nav) routes that also need help pages. */
export const otherRoutes: { path: string; doc: string }[] = [
  { path: '/instances/:id', doc: 'operation/instance-detail' },
  { path: '/setup', doc: 'getting-started/first-run' },
  { path: '/login', doc: 'admin/users' },
]

export function docForPath(pathname: string): string {
  const all = [...otherRoutes, ...navRoutes]
  // Longest pattern that matches wins.
  let best = 'index'
  let bestLen = -1
  for (const r of all) {
    const re = new RegExp('^' + r.path.replace(/:[^/]+/g, '[^/]+') + '$')
    if (re.test(pathname) && r.path.length > bestLen) {
      best = r.doc
      bestLen = r.path.length
    }
  }
  return best
}
