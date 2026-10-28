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
