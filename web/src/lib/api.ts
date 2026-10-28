// Thin fetch wrapper: JSON in/out, CSRF header on mutations, typed errors.

let csrfToken = ''

export function setCsrf(token: string) {
  csrfToken = token
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

type Method = 'GET' | 'POST' | 'PUT' | 'DELETE'

async function request<T>(method: Method, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (method !== 'GET' && csrfToken) headers['X-CSRF-Token'] = csrfToken
  const res = await fetch(`/api/v1${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: 'same-origin',
  })
  const text = await res.text()
  const data = text ? safeJSON(text) : null
  if (!res.ok) {
    const msg = (data && typeof data === 'object' && 'error' in data ? String(data.error) : '') || res.statusText
    if (res.status === 401 && !path.startsWith('/auth/')) window.dispatchEvent(new CustomEvent('gt:unauthorized'))
    throw new ApiError(res.status, msg)
  }
  return data as T
}

function safeJSON(t: string): unknown {
  try {
    return JSON.parse(t)
  } catch {
    return t
  }
}

export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  post: <T>(p: string, b?: unknown) => request<T>('POST', p, b ?? {}),
  put: <T>(p: string, b?: unknown) => request<T>('PUT', p, b ?? {}),
  del: <T>(p: string) => request<T>('DELETE', p),
}

// ---- shared API types ----

export type Role = 'admin' | 'operator' | 'viewer'

export interface User {
  id: number
  username: string
  role: Role
  family_access: boolean
  oidc: boolean
  created_at: number
  last_login_at?: number
}

export interface InstanceStatus {
  online: boolean
  version: string
  protection_enabled: boolean
  dns_addresses: string[]
  latency_ms: number
  last_error: string
  last_seen_at?: number
  checked_at?: number
}

export type Runtime = 'unknown' | 'lxc' | 'docker' | 'vm' | 'baremetal' | 'router' | 'homeassistant'

export interface Instance {
  id: number
  name: string
  url: string
  username: string
  has_password: boolean
  tls_skip_verify: boolean
  ca_pem: string
  dns_address: string
  group_id?: number
  host_id?: number
  runtime: Runtime
  is_canary: boolean
  enabled: boolean
  tags: string[]
  created_at: number
  status?: InstanceStatus
}

export interface Group {
  id: number
  name: string
  description: string
  color: string
  created_at: number
  instance_ids: number[]
}

export type HostKind = 'proxmox' | 'nas' | 'docker' | 'vm' | 'baremetal' | 'router' | 'cloud' | 'other'

export interface Host {
  id: number
  name: string
  site_id?: number
  power: string
  kind: HostKind
  address: string
  notes: string
}

export interface Site {
  id: number
  name: string
}

export interface Overview {
  instances: Instance[]
  groups: Group[]
  hosts: Host[]
  sites: Site[]
  summary: { instances: number; online: number; groups: number; hosts: number }
}

export interface AuditEntry {
  id: number
  ts: number
  user_id?: number
  username: string
  action: string
  target: string
  detail: string
  ip: string
}

export interface DocPage {
  slug: string
  title: string
  section: string
  order: number
  summary: string
  routes?: string[]
  body?: string
}

// ---- query log & analytics ----

export type QueryStatus = 'allowed' | 'allowlisted' | 'blocked' | 'safebrowsing' | 'parental' | 'safesearch' | 'rewritten'

export interface QueryRow {
  id: number
  instance_id: number
  ts: number
  client: string
  client_name: string
  client_id?: string
  domain: string
  qtype: string
  status: QueryStatus
  blocked: boolean
  reason: string
  filter_id?: number
  rule?: string
  service?: string
  upstream?: string
  elapsed_ms: number
  cached: boolean
  proto?: string
  answer?: string
}

export interface QueryPage {
  rows: QueryRow[]
  next?: { ts: number; id: number }
}

export interface TopItem {
  key: string
  label?: string
  count: number
}

export interface StatsSummary {
  from: number
  to: number
  bucket_ms: number
  totals: {
    queries: number
    blocked: number
    cached: number
    avg_elapsed_ms: number
    blocked_pct: number
    cached_pct: number
    clients: number
    from_rollups: boolean
  }
  series: { t: number; queries: number; blocked: number }[]
  instances: { instance_id: number; queries: number; blocked: number; avg_elapsed_ms: number }[]
  top_domains: TopItem[]
  top_blocked: TopItem[]
  top_clients: TopItem[]
  statuses: TopItem[]
  heatmap: number[][]
}

export interface CollectorState {
  instance_id: number
  last_ts: number
  last_run_at: number
  last_error: string
  rows_total: number
}

export interface SettingsResponse {
  settings: Record<string, string>
  database_bytes: number
  querylog_rows: number
}

// ---- configuration ----

export type FieldKind = 'bool' | 'int' | 'string' | 'enum' | 'list' | 'set' | 'keyed' | 'object' | 'duration_ms'

export interface ConfigField {
  name: string
  label: string
  help?: string
  kind: FieldKind
  enum?: string[]
  key_label?: string
  item_fields?: ConfigField[]
}

export interface ConfigSection {
  key: string
  label: string
  help: string
  fields: ConfigField[]
}

export type LayerDoc = Record<string, unknown>

export interface LayerInfo {
  scope: 'base' | 'group' | 'instance'
  id: number
  section: string
  doc: LayerDoc
  updated_at: number
  updated_by: string
}

export interface Change {
  section: string
  field: string
  label: string
  op: 'set' | 'add' | 'remove' | 'update'
  item?: string
  from?: unknown
  to?: unknown
}

export interface SectionState {
  section: string
  managed: boolean
  desired?: Record<string, unknown>
  origins?: Record<string, { scope: string; name: string }>
  live?: Record<string, unknown>
  changes: Change[]
  live_error?: string
  unsupported?: string[]
}

export interface SyncSummary {
  instance_id: number
  drifted: string[]
  sections: Record<string, number>
  errors?: Record<string, string>
  total: number
  checked_at: number
}

// ---- rollouts & snapshots ----

export interface RolloutTarget {
  kind: 'fleet' | 'group' | 'tag' | 'instance'
  id?: number
  tag?: string
}

export interface PlanItem {
  instance_id: number
  name: string
  canary: boolean
  online: boolean
  changes: Change[]
  unsupported?: string[]
  error?: string
}

export interface Plan {
  target: string
  sections?: string[]
  items: PlanItem[]
  total: number
  observe: boolean
  warnings: string[]
}

export type StepStatus = 'pending' | 'applying' | 'verifying' | 'succeeded' | 'failed' | 'rolled_back' | 'skipped'

export interface RolloutStep {
  id: number
  instance_id: number
  instance_name: string
  ordinal: number
  canary: boolean
  status: StepStatus
  changes: Change[]
  snapshot_id?: number
  detail: string
  started_at?: number
  finished_at?: number
}

export interface Rollout {
  id: number
  created_at: number
  created_by: string
  kind: 'apply' | 'restore' | 'upgrade'
  target: string
  sections: string[]
  status: 'running' | 'succeeded' | 'failed' | 'rolled_back' | 'cancelled'
  error: string
  finished_at?: number
  steps?: RolloutStep[]
}

export interface Snapshot {
  id: number
  instance_id: number
  created_at: number
  kind: 'pre-apply' | 'post-apply' | 'manual' | 'pre-restore'
  rollout_id?: number
  note?: string
}

// ---- AdGuard Home upgrades ----

export type UpgradeDriver = 'auto' | 'builtin' | 'docker' | 'ssh' | 'none'

export interface Release {
  tag: string
  name: string
  published_at: number
  url: string
  notes: string
  prerelease: boolean
}

export interface UpgradeInstanceState {
  instance_id: number
  name: string
  group_id?: number
  online: boolean
  version: string
  configured: UpgradeDriver
  driver: '' | 'builtin' | 'docker' | 'ssh'
  can_autoupdate: boolean
  builtin_version?: string
  pinned?: string
  target?: string
  update_available: boolean
  blocked?: string
  check_error?: string
  checked_at?: number
}

export interface UpgradeWindow {
  days: number[]
  start: string
  end: string
}

export interface UpgradePolicy {
  group_id: number
  policy: 'manual' | 'auto'
  window: UpgradeWindow
  last_auto?: number
}

export interface UpgradeOverview {
  latest?: Release
  releases: Release[]
  instances: UpgradeInstanceState[]
  policies: UpgradePolicy[]
  checks_enabled: boolean
  checked_at?: number
  check_error?: string
  observe: boolean
}

export interface UpgradePlanItem {
  instance_id: number
  name: string
  canary: boolean
  online: boolean
  driver: string
  from: string
  to?: string
  skip?: string
}

export interface UpgradePlan {
  target: string
  version?: string
  items: UpgradePlanItem[]
  total: number
  observe: boolean
  warnings: string[]
}

export interface DockerConfig {
  endpoint: string
  container: string
  image: string
  tls_skip_verify: boolean
  ca_pem: string
  conf_path: string
}

export interface SSHConfig {
  host: string
  port: number
  user: string
  has_key: boolean
  host_key: string
  sudo: boolean
  agh_dir: string
  service: string
}

export interface UpgradeConfig {
  instance_id: number
  driver: UpgradeDriver
  pinned_version: string
  docker: DockerConfig
  ssh: SSHConfig
  updated_at?: number
}

export interface UpgradeTestResult {
  ok: boolean
  message: string
  host_key?: { key: string; fingerprint: string }
  pinned: boolean
}
