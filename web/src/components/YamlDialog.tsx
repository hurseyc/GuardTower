import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { FileUp } from 'lucide-react'
import { api, type ConfigSection, type ImportResult } from '@/lib/api'
import { Badge, Button, Dialog, Switch, Textarea } from './ui'

const actionTone = { create: 'ok', update: 'info', delete: 'danger' } as const

/** Paste or upload a settings YAML, preview what changes, then import it. */
export function YamlImportDialog({ open, onOpenChange, schema }: { open: boolean; onOpenChange: (v: boolean) => void; schema?: ConfigSection[] }) {
  const qc = useQueryClient()
  const [text, setText] = useState('')
  const [prune, setPrune] = useState(false)
  const [preview, setPreview] = useState<ImportResult | null>(null)
  const run = useMutation({
    mutationFn: (dry: boolean) => api.postRaw<ImportResult>(`/config/yaml?dry_run=${dry ? 1 : 0}&prune=${prune ? 1 : 0}`, text, 'application/yaml'),
    onSuccess: (r) => {
      if (r.applied) {
        toast.success(`Imported ${r.changes.length} layer change${r.changes.length === 1 ? '' : 's'}`, { description: 'Review & apply to roll them out.' })
        void qc.invalidateQueries()
        setPreview(null)
        setText('')
        onOpenChange(false)
      } else {
        setPreview(r)
      }
    },
    onError: (e) => toast.error((e as Error).message),
  })
  const label = (k: string) => schema?.find((s) => s.key === k)?.label ?? k
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => (setPreview(null), onOpenChange(v))}
      title="Import settings from YAML"
      description="Layers in the file replace the same layers here. Nothing reaches your instances until you apply it from Review & apply."
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          {preview && preview.changes.length > 0 ? (
            <Button variant="primary" loading={run.isPending} onClick={() => run.mutate(false)}>
              Import {preview.changes.length} change{preview.changes.length === 1 ? '' : 's'}
            </Button>
          ) : (
            <Button variant="primary" loading={run.isPending} disabled={!text.trim()} onClick={() => run.mutate(true)}>
              Preview
            </Button>
          )}
        </>
      }
    >
      <div className="space-y-3">
        <label className="inline-flex cursor-pointer items-center gap-2 text-sm text-accent hover:underline">
          <FileUp className="h-4 w-4" /> Choose a file…
          <input
            type="file"
            accept=".yaml,.yml,text/yaml"
            className="sr-only"
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f) void f.text().then((t) => (setText(t), setPreview(null)))
            }}
          />
        </label>
        <Textarea aria-label="Settings YAML" value={text} onChange={(e) => (setText(e.target.value), setPreview(null))} rows={10} className="font-mono text-xs" placeholder="version: 1&#10;base:&#10;  dns:&#10;    upstream_dns: …" />
        <label className="flex items-center gap-2 text-sm">
          <Switch checked={prune} onCheckedChange={(v) => (setPrune(v), setPreview(null))} /> Also delete layers the file doesn’t mention
        </label>
        {preview && (
          <div className="rounded-lg border border-border p-3 text-sm">
            {preview.changes.length === 0 ? (
              <p className="text-muted">Everything in the file already matches ({preview.unchanged} layer{preview.unchanged === 1 ? '' : 's'}).</p>
            ) : (
              <ul className="max-h-56 space-y-1 overflow-auto">
                {preview.changes.map((c, i) => (
                  <li key={i} className="flex items-center gap-2">
                    <Badge tone={actionTone[c.action]}>{c.action}</Badge>
                    <span>{c.scope === 'base' ? 'Base' : c.name}</span>
                    <span className="text-muted">· {label(c.section)}</span>
                  </li>
                ))}
              </ul>
            )}
            {preview.unchanged > 0 && preview.changes.length > 0 && <p className="mt-2 text-xs text-faint">{preview.unchanged} unchanged</p>}
          </div>
        )}
      </div>
    </Dialog>
  )
}
