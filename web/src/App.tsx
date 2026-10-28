import { BrowserRouter, Navigate, Route, Routes } from 'react-router'
import { useSession } from '@/lib/session'
import { AppShell } from '@/components/AppShell'
import { Skeleton } from '@/components/ui'
import { LoginPage, SetupPage } from '@/pages/Auth'
import { OverviewPage } from '@/pages/Overview'
import { InstancesPage } from '@/pages/Instances'
import { InstanceDetailPage } from '@/pages/InstanceDetail'
import { GroupsPage } from '@/pages/Groups'
import { HostsPage } from '@/pages/Hosts'
import { AuditPage, UsersPage } from '@/pages/Admin'
import { DocsPage } from '@/pages/Docs'
import { AnalyticsPage } from '@/pages/Analytics'
import { QueryLogPage } from '@/pages/QueryLog'
import { SettingsPage } from '@/pages/Settings'
import { ConfigPage } from '@/pages/Config'
import { ChangesPage } from '@/pages/Changes'

function Gate() {
  const { loading, needsSetup, user, can } = useSession()
  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Skeleton className="h-10 w-40" />
      </div>
    )
  }
  if (needsSetup) {
    return (
      <Routes>
        <Route path="*" element={<SetupPage />} />
      </Routes>
    )
  }
  if (!user) {
    return (
      <Routes>
        <Route path="*" element={<LoginPage />} />
      </Routes>
    )
  }
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<OverviewPage />} />
        <Route path="instances" element={<InstancesPage />} />
        <Route path="instances/:id" element={<InstanceDetailPage />} />
        <Route path="groups" element={<GroupsPage />} />
        <Route path="hosts" element={<HostsPage />} />
        <Route path="config" element={<ConfigPage />} />
        <Route path="changes" element={<ChangesPage />} />
        <Route path="analytics" element={<AnalyticsPage />} />
        <Route path="logs" element={<QueryLogPage />} />
        {can('admin') && <Route path="users" element={<UsersPage />} />}
        {can('admin') && <Route path="audit" element={<AuditPage />} />}
        {can('admin') && <Route path="settings" element={<SettingsPage />} />}
        <Route path="docs/*" element={<DocsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

export default function App() {
  return (
    <BrowserRouter>
      <Gate />
    </BrowserRouter>
  )
}
