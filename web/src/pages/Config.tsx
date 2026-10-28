import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Boxes, Download, FileDown, FileUp, Globe, Layers, Plus, ShieldCheck, Trash2, Undo2 } from 'lucide-react'
import { api, type ConfigField, type ConfigSection, type LayerDoc, type LayerInfo, type Overview } from '@/lib/api'
import { fmtValue, keyOf, mergeLayers } from '@/lib/configMerge'
import { useSession } from '@/lib/sessionContext'
import { cn, colorFor, timeAgo } from '@/lib/utils'
import { PageHeader } from '@/components/AppShell'
import { YamlImportDialog } from '@/components/YamlDialog'
import { Badge, Button, Card, Dialog, Field, Input, Select, Skeleton, Switch, Textarea } from '@/components/ui'
import { useOnChange } from '@/lib/useOnChange'

type ScopeSel = { scope: 'base' | 'group' | 'instance'; id: number }

function parseScope(v: string | null): ScopeSel {
  if (!v || v === 'base') return { scope: 'base', id: 0 }
  const [s, id] = v.split(':')
  return { scope: s as ScopeSel['scope'], id: Number(id) }
}
const scopeParam = (s: ScopeSel) => (s.scope === 'base' ? 'base' : `${s.scope}:${s.id}`)
const layersKey = (s: ScopeSel) => ['layers', s.scope, s.id]
const fetchLayers = (s: ScopeSel) => api.get<Record<string, LayerInfo>>(`/config/layers?scope=${s.scope}&id=${s.id}`)

export function ConfigPage() {
  const { can } = useSession()
  const [params, setParams] = useSearchParams()
  const sel = parseScope(params.get('scope'))
  const sectionKey = params.get('section') ?? 'dns'
  const [importOpen, setImportOpen] = useState(false)
  const [yamlOpen, setYamlOpen] = useState(false)

  const { data: schema } = useQuery({ queryKey: ['config-schema'], queryFn: () => api.get<ConfigSection[]>('/config/schema'), staleTime: Infinity })
  const { data: overview } = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview') })
  const { data: layers } = useQuery({ queryKey: layersKey(sel), queryFn: () => fetchLayers(sel) })

  // Lower layers, for showing what this scope inherits.
  const inst = sel.scope === 'instance' ? overview?.instances.find((i) => i.id === sel.id) : undefined
  const lowerScopes: ScopeSel[] =
    sel.scope === 'base' ? [] : sel.scope === 'group' ? [{ scope: 'base', id: 0 }] : [{ scope: 'base', id: 0 }, ...(inst?.group_id ? [{ scope: 'group' as const, id: inst.group_id }] : [])]
  const { data: lower } = useQuery({
    queryKey: ['layers-lower', ...lowerScopes.map(scopeParam)],
    queryFn: () => Promise.all(lowerScopes.map(fetchLayers)),
    enabled: sel.scope !== 'base' && (sel.scope !== 'instance' || !!inst),
  })

  const setSel = (s: ScopeSel) => {
    const n = new URLSearchParams(params)
    n.set('scope', scopeParam(s))
    setParams(n, { replace: true })
  }
  const setSection = (k: string) => {
    const n = new URLSearchParams(params)
    n.set('section', k)
    setParams(n, { replace: true })
  }
  const section = schema?.find((s) => s.key === sectionKey) ?? schema?.[0]
  const scopeName =
    sel.scope === 'base' ? 'Base (all instances)' : sel.scope === 'group' ? overview?.groups.find((g) => g.id === sel.id)?.name ?? 'Group' : inst?.name ?? 'Instance'
  const inherited = useMemo(() => (section && lower ? mergeLayers(section, lower.map((l) => l[section.key]?.doc ?? {})) : {}), [section, lower])

  return (
    <>
      <PageHeader
        title="Configuration"
        description="Desired AdGuard Home settings, layered: Base applies to every instance, groups add or override, and single instances can have exceptions."
        actions={
          <>
            <a href="/api/v1/config/yaml" download className="inline-flex items-center gap-1.5 rounded-lg px-3 py-2 text-sm text-muted hover:bg-hover hover:text-fg">
              <FileDown className="h-4 w-4" /> Export YAML
            </a>
            {can('admin') && (
              <Button variant="ghost" onClick={() => setYamlOpen(true)}>
                <FileUp className="h-4 w-4" /> Import YAML
              </Button>
            )}
            {can('admin') && sel.scope !== 'instance' && (
              <Button variant="outline" onClick={() => setImportOpen(true)}>
                <Download className="h-4 w-4" /> Import from an instance
              </Button>
            )}
          </>
        }
      />
      <div className="grid gap-6 p-6 lg:grid-cols-[15rem_14rem_1fr]">
        {/* Scopes */}
        <Card className="h-fit p-2">
          <ScopeButton active={sel.scope === 'base'} onClick={() => setSel({ scope: 'base', id: 0 })} icon={<Globe className="h-4 w-4" />} label="Base" sub="All instances" />
          <div className="mt-3 px-2 pb-1 text-[10px] font-semibold uppercase tracking-wider text-faint">Groups</div>
          {overview?.groups.map((g) => (
            <ScopeButton
              key={g.id}
              active={sel.scope === 'group' && sel.id === g.id}
              onClick={() => setSel({ scope: 'group', id: g.id })}
              icon={<span className="ml-1 mr-0.5 inline-block h-2.5 w-2.5 rounded-full" style={{ background: colorFor(g.name, g.color) }} />}
              label={g.name}
              sub={`${g.instance_ids.length} instance${g.instance_ids.length === 1 ? '' : 's'}`}
            />
          ))}
          {overview?.groups.length === 0 && <p className="px-2 py-1 text-xs text-faint">No groups yet.</p>}
          <div className="mt-3 px-2 pb-1 text-[10px] font-semibold uppercase tracking-wider text-faint">Instance overrides</div>
          {overview?.instances.map((i) => (
            <ScopeButton key={i.id} active={sel.scope === 'instance' && sel.id === i.id} onClick={() => setSel({ scope: 'instance', id: i.id })} icon={<ShieldCheck className="h-4 w-4" />} label={i.name} />
          ))}
        </Card>

        {/* Sections */}
        <Card className="h-fit p-2">
          {!schema ? (
            <Skeleton className="h-64" />
          ) : (
            schema.map((s) => {
              const n = Object.keys(layers?.[s.key]?.doc ?? {}).length
              return (
                <button
                  key={s.key}
                  onClick={() => setSection(s.key)}
                  className={cn(
                    'flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-sm',
                    section?.key === s.key ? 'bg-accent-soft font-medium text-accent' : 'text-muted hover:bg-hover hover:text-fg',
                  )}
                >
                  <span className="flex-1 truncate">{s.label}</span>
                  {n > 0 && <Badge tone="accent">{n}</Badge>}
                </button>
              )
            })
          )}
        </Card>

        {/* Editor */}
        {section && layers ? (
          <SectionEditor
            key={`${scopeParam(sel)}/${section.key}`}
            sel={sel}
            scopeName={scopeName}
            section={section}
            layer={layers[section.key]}
            inherited={inherited}
            readOnly={!can('admin')}
          />
        ) : (
          <Skeleton className="h-96" />
        )}
      </div>
      <YamlImportDialog open={yamlOpen} onOpenChange={setYamlOpen} schema={schema} />
      {overview && schema && <ImportDialog open={importOpen} onOpenChange={setImportOpen} overview={overview} schema={schema} target={sel} targetName={scopeName} />}
    </>
  )
}

function ScopeButton({ active, onClick, icon, label, sub }: { active: boolean; onClick: () => void; icon: React.ReactNode; label: string; sub?: string }) {
  return (
    <button onClick={onClick} className={cn('flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left', active ? 'bg-accent-soft text-accent' : 'text-muted hover:bg-hover hover:text-fg')}>
      <span className="shrink-0">{icon}</span>
      <span className="min-w-0 flex-1">
        <span className={cn('block truncate text-sm', active && 'font-medium')}>{label}</span>
        {sub && <span className="block text-[11px] text-faint">{sub}</span>}
      </span>
    </button>
  )
}

function SectionEditor({
  sel,
  scopeName,
  section,
  layer,
  inherited,
  readOnly,
}: {
  sel: ScopeSel
  scopeName: string
  section: ConfigSection
  layer?: LayerInfo
  inherited: Record<string, unknown>
  readOnly: boolean
}) {
  const qc = useQueryClient()
  const initial = layer?.doc ?? {}
  const [draft, setDraft] = useState<LayerDoc>(initial)
  const [error, setError] = useState('')
  const dirty = JSON.stringify(draft) !== JSON.stringify(initial)
  const isBase = sel.scope === 'base'

  const save = useMutation({
    mutationFn: () => api.put<LayerDoc>(`/config/layers/${sel.scope}/${sel.id}/${section.key}`, draft),
    onSuccess: (clean) => {
      setError('')
      setDraft(clean)
      void qc.invalidateQueries({ queryKey: ['layers'] })
      void qc.invalidateQueries({ queryKey: ['layers-lower'] })
      void qc.invalidateQueries({ queryKey: ['instance-config'] })
      toast.success(`${section.label} saved for ${scopeName}`, { description: 'Nothing is pushed to AdGuard Home until you apply the changes.' })
    },
    onError: (e) => setError((e as Error).message),
  })

  const setField = (name: string, value: unknown | undefined) =>
    setDraft((d) => {
      const n = { ...d }
      if (value === undefined) delete n[name]
      else n[name] = value
      return n
    })

  return (
    <Card>
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border px-5 py-4">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-xs text-faint">
            {sel.scope === 'base' ? <Globe className="h-3.5 w-3.5" /> : sel.scope === 'group' ? <Boxes className="h-3.5 w-3.5" /> : <Layers className="h-3.5 w-3.5" />}
            {scopeName}
          </div>
          <h2 className="mt-1 text-base font-semibold">{section.label}</h2>
          <p className="mt-0.5 text-sm text-muted">{section.help}</p>
        </div>
        {!readOnly && (
          <div className="flex items-center gap-2">
            {dirty && (
              <Button variant="ghost" onClick={() => (setDraft(initial), setError(''))}>
                <Undo2 className="h-4 w-4" /> Discard
              </Button>
            )}
            <Button variant="primary" disabled={!dirty} loading={save.isPending} onClick={() => save.mutate()}>
              Save {section.label.toLowerCase()}
            </Button>
          </div>
        )}
      </div>
      {error && <div className="mx-5 mt-4 rounded-lg bg-danger-soft p-3 text-sm">{error}</div>}
      <div className="divide-y divide-border">
        {section.fields.map((f) => (
          <FieldRow key={f.name} section={section} field={f} isBase={isBase} value={draft[f.name]} inherited={inherited[f.name]} onChange={(v) => setField(f.name, v)} readOnly={readOnly} />
        ))}
      </div>
      <div className="border-t border-border px-5 py-3 text-xs text-faint">
        {layer ? `Last changed ${timeAgo(layer.updated_at)} by ${layer.updated_by || 'unknown'}.` : isBase ? 'Nothing in this section is managed yet.' : `${scopeName} inherits everything in this section.`}
      </div>
    </Card>
  )
}

/** Default layer value when a field is switched on at this scope. */
function defaultFor(f: ConfigField, inherited: unknown, isBase: boolean): unknown {
  switch (f.kind) {
    case 'bool':
      return typeof inherited === 'boolean' ? inherited : false
    case 'int':
    case 'duration_ms':
      return typeof inherited === 'number' ? inherited : 0
    case 'string':
      return typeof inherited === 'string' ? inherited : ''
    case 'enum':
      return typeof inherited === 'string' ? inherited : f.enum?.[0] ?? ''
    case 'list':
      return Array.isArray(inherited) ? inherited : []
    case 'set':
      return isBase && Array.isArray(inherited) ? { add: inherited, remove: [] } : { add: [], remove: [] }
    case 'keyed':
      return { upsert: [], remove: [] }
    case 'object':
      return inherited && typeof inherited === 'object' ? inherited : {}
  }
}

function FieldRow({
  section,
  field: f,
  isBase,
  value,
  inherited,
  onChange,
  readOnly,
}: {
  section: ConfigSection
  field: ConfigField
  isBase: boolean
  value: unknown
  inherited: unknown
  onChange: (v: unknown | undefined) => void
  readOnly: boolean
}) {
  const set = value !== undefined
  const additive = f.kind === 'set' || f.kind === 'keyed'
  return (
    <div className="grid gap-3 px-5 py-4 md:grid-cols-[16rem_1fr]">
      <div>
        <div className="flex items-center gap-2">
          <Switch checked={set} disabled={readOnly} onCheckedChange={(on) => onChange(on ? defaultFor(f, inherited, isBase) : undefined)} />
          <span className="text-sm font-medium">{f.label}</span>
        </div>
        <p className="mt-1 pl-11 text-xs text-faint">
          {set ? (isBase ? 'Managed by GuardTower' : additive ? 'Adjusted at this level' : 'Overridden at this level') : isBase ? 'Not managed: left as it is on each instance' : 'Inherited'}
        </p>
        {f.help && <p className="mt-1 pl-11 text-xs text-muted">{f.help}</p>}
      </div>
      <div className="min-w-0">
        {!set ? (
          <div className="rounded-lg border border-dashed border-border px-3 py-2 text-sm text-faint">
            {isBase ? 'Not managed' : inherited === undefined ? 'Not managed by lower levels' : <>Inherits: <span className="text-muted">{fmtValue(f, inherited)}</span></>}
          </div>
        ) : (
          <FieldEditor section={section} field={f} isBase={isBase} value={value} inherited={inherited} onChange={onChange} readOnly={readOnly} />
        )}
      </div>
    </div>
  )
}

function linesOf(v: unknown): string {
  return Array.isArray(v) ? (v as string[]).join('\n') : ''
}
function toLines(s: string): string[] {
  return s
    .split('\n')
    .map((x) => x.trim())
    .filter(Boolean)
}

function FieldEditor({
  section,
  field: f,
  isBase,
  value,
  inherited,
  onChange,
  readOnly,
}: {
  section: ConfigSection
  field: ConfigField
  isBase: boolean
  value: unknown
  inherited: unknown
  onChange: (v: unknown) => void
  readOnly: boolean
}) {
  switch (f.kind) {
    case 'bool':
      return (
        <label className="inline-flex items-center gap-2 text-sm">
          <Switch checked={!!value} disabled={readOnly} onCheckedChange={onChange} /> {value ? 'On' : 'Off'}
        </label>
      )
    case 'int':
    case 'duration_ms':
      return <Input type="number" min={0} className="max-w-48" value={String(value ?? 0)} disabled={readOnly} onChange={(e) => onChange(e.target.value === '' ? 0 : Number(e.target.value))} />
    case 'string':
      return <Input value={String(value ?? '')} disabled={readOnly} onChange={(e) => onChange(e.target.value)} className="font-mono" />
    case 'enum':
      return (
        <Select value={String(value)} disabled={readOnly} onChange={(e) => onChange(e.target.value)} className="max-w-60">
          {f.enum?.map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </Select>
      )
    case 'list':
      return <Textarea rows={Math.min(10, Math.max(3, (value as string[]).length + 1))} disabled={readOnly} value={linesOf(value)} onChange={(e) => onChange(toLines(e.target.value))} placeholder="One per line" />
    case 'object':
      return <JSONEditor value={value} onChange={onChange} readOnly={readOnly} />
    case 'set': {
      const ops = (value ?? { add: [], remove: [] }) as { add: string[]; remove: string[] }
      return (
        <div className={cn('grid gap-3', !isBase && 'sm:grid-cols-2')}>
          <Field label={isBase ? 'Items' : 'Add'} hint="One per line">
            <Textarea rows={Math.min(12, Math.max(3, ops.add.length + 1))} disabled={readOnly} value={ops.add.join('\n')} onChange={(e) => onChange({ ...ops, add: toLines(e.target.value) })} />
          </Field>
          {!isBase && (
            <Field label="Remove inherited" hint={Array.isArray(inherited) && inherited.length ? `Inherited: ${(inherited as string[]).slice(0, 4).join(', ')}${(inherited as string[]).length > 4 ? '…' : ''}` : 'Nothing inherited'}>
              <Textarea rows={Math.min(12, Math.max(3, ops.remove.length + 1))} disabled={readOnly} value={ops.remove.join('\n')} onChange={(e) => onChange({ ...ops, remove: toLines(e.target.value) })} />
            </Field>
          )}
        </div>
      )
    }
    case 'keyed':
      return <KeyedEditor section={section} field={f} isBase={isBase} value={value} inherited={inherited} onChange={onChange} readOnly={readOnly} />
  }
}

function JSONEditor({ value, onChange, readOnly }: { value: unknown; onChange: (v: unknown) => void; readOnly: boolean }) {
  const [text, setText] = useState(JSON.stringify(value, null, 2))
  const [err, setErr] = useState('')
  useOnChange(JSON.stringify(value, null, 2), setText)
  return (
    <div>
      <Textarea
        rows={6}
        disabled={readOnly}
        value={text}
        onChange={(e) => {
          setText(e.target.value)
          try {
            const v = JSON.parse(e.target.value)
            setErr('')
            onChange(v)
          } catch {
            setErr('Not valid JSON yet')
          }
        }}
      />
      {err && <p className="mt-1 text-xs text-warn">{err}</p>}
    </div>
  )
}

function KeyedEditor({
  section,
  field: f,
  isBase,
  value,
  inherited,
  onChange,
  readOnly,
}: {
  section: ConfigSection
  field: ConfigField
  isBase: boolean
  value: unknown
  inherited: unknown
  onChange: (v: unknown) => void
  readOnly: boolean
}) {
  const ops = (value ?? { upsert: [], remove: [] }) as { upsert: Record<string, unknown>[]; remove: string[] }
  const inheritedItems = Array.isArray(inherited) ? (inherited as Record<string, unknown>[]) : []
  const editable = (f.item_fields ?? []).filter((x) => ['string', 'bool', 'int', 'list'].includes(x.kind))
  const update = (i: number, k: string, v: unknown) => {
    const up = ops.upsert.map((it, j) => (j === i ? { ...it, [k]: v } : it))
    onChange({ ...ops, upsert: up })
  }
  const blank = () => Object.fromEntries(editable.map((x) => [x.name, x.kind === 'bool' ? true : x.kind === 'list' ? [] : '']))
  return (
    <div className="space-y-3">
      {ops.upsert.length === 0 && <p className="text-sm text-faint">{isBase ? 'No items yet.' : 'No items added or changed at this level.'}</p>}
      {ops.upsert.map((it, i) => (
        <div key={i} className="grid items-end gap-2 rounded-lg border border-border bg-bg p-3 sm:grid-cols-[repeat(auto-fit,minmax(9rem,1fr))_auto]">
          {editable.map((x) =>
            x.kind === 'bool' ? (
              <label key={x.name} className="flex items-center gap-2 pb-2 text-xs text-muted">
                <Switch checked={!!it[x.name]} disabled={readOnly} onCheckedChange={(v) => update(i, x.name, v)} /> {x.label}
              </label>
            ) : (
              <Field key={x.name} label={x.label}>
                <Input
                  value={x.kind === 'list' ? ((it[x.name] as string[]) ?? []).join(', ') : String(it[x.name] ?? '')}
                  disabled={readOnly}
                  className="font-mono text-xs"
                  onChange={(e) => update(i, x.name, x.kind === 'list' ? e.target.value.split(',').map((s) => s.trim()).filter(Boolean) : e.target.value)}
                />
              </Field>
            ),
          )}
          {!readOnly && (
            <Button size="icon" variant="ghost" aria-label="Remove item" onClick={() => onChange({ ...ops, upsert: ops.upsert.filter((_, j) => j !== i) })}>
              <Trash2 className="h-4 w-4" />
            </Button>
          )}
        </div>
      ))}
      {!readOnly && (
        <Button size="sm" variant="outline" onClick={() => onChange({ ...ops, upsert: [...ops.upsert, blank()] })}>
          <Plus className="h-3.5 w-3.5" /> Add {f.label.toLowerCase().replace(/s$/, '')}
        </Button>
      )}
      {!isBase && inheritedItems.length > 0 && (
        <div className="rounded-lg border border-border p-3">
          <div className="mb-2 text-xs font-medium text-muted">Inherited {f.label.toLowerCase()} · click to exclude here</div>
          <div className="flex flex-wrap gap-1.5">
            {inheritedItems.map((it) => {
              const k = keyOf(section.key, f, it)
              const removed = ops.remove.includes(k)
              return (
                <button
                  key={k}
                  disabled={readOnly}
                  onClick={() => onChange({ ...ops, remove: removed ? ops.remove.filter((x) => x !== k) : [...ops.remove, k] })}
                  className={cn('rounded-md border px-2 py-1 font-mono text-[11px]', removed ? 'border-transparent bg-danger-soft text-danger line-through' : 'border-border text-muted hover:bg-hover')}
                  title={removed ? 'Excluded at this level: click to include again' : 'Click to exclude at this level'}
                >
                  {String(it.name || k)}
                </button>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}

function ImportDialog({
  open,
  onOpenChange,
  overview,
  schema,
  target,
  targetName,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  overview: Overview
  schema: ConfigSection[]
  target: ScopeSel
  targetName: string
}) {
  const qc = useQueryClient()
  const [instanceId, setInstanceId] = useState(0)
  const [sections, setSections] = useState<string[]>(schema.map((s) => s.key))
  useOnChange(open, (o) => {
    if (o) {
      setInstanceId(overview.instances.find((i) => i.status?.online)?.id ?? overview.instances[0]?.id ?? 0)
      setSections(schema.map((s) => s.key))
    }
  })
  const run = useMutation({
    mutationFn: () => api.post('/config/import', { instance_id: instanceId, scope: target.scope, id: target.id, sections }),
    onSuccess: () => {
      toast.success(`Imported into ${targetName}`, { description: 'Review the sections, then check which instances differ.' })
      void qc.invalidateQueries({ queryKey: ['layers'] })
      void qc.invalidateQueries({ queryKey: ['layers-lower'] })
      onOpenChange(false)
    },
    onError: (e) => toast.error((e as Error).message),
  })
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Import settings into ${targetName}`}
      description="Copies an instance's current settings into this level as the desired state. Existing settings for the selected sections at this level are replaced. Nothing changes on any AdGuard Home instance."
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="primary" loading={run.isPending} disabled={!instanceId || sections.length === 0} onClick={() => run.mutate()}>
            Import {sections.length} section{sections.length === 1 ? '' : 's'}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Field label="Copy from instance" hint="Pick the instance whose settings are closest to what you want everywhere">
          <Select value={instanceId} onChange={(e) => setInstanceId(Number(e.target.value))}>
            {overview.instances.map((i) => (
              <option key={i.id} value={i.id} disabled={!i.status?.online}>
                {i.name}
                {i.status?.online ? '' : ' (offline)'}
              </option>
            ))}
          </Select>
        </Field>
        <div>
          <div className="mb-2 text-xs font-medium text-muted">Sections</div>
          <div className="grid gap-1 sm:grid-cols-2">
            {schema.map((s) => (
              <label key={s.key} className="flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-sm hover:bg-hover">
                <input
                  type="checkbox"
                  className="h-4 w-4 accent-[var(--accent)]"
                  checked={sections.includes(s.key)}
                  onChange={(e) => setSections((cur) => (e.target.checked ? [...cur, s.key] : cur.filter((x) => x !== s.key)))}
                />
                {s.label}
              </label>
            ))}
          </div>
        </div>
      </div>
    </Dialog>
  )
}
