import { useState } from 'react'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Button, Dialog, Field, Input } from './ui'

export function ChangePasswordDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const submit = async () => {
    setError('')
    if (next !== confirm) return setError('Passwords do not match')
    setBusy(true)
    try {
      await api.post('/me/password', { current, next })
      toast.success('Password changed')
      onOpenChange(false)
      setCurrent('')
      setNext('')
      setConfirm('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Change password"
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="primary" loading={busy} onClick={submit}>
            Change password
          </Button>
        </>
      }
    >
      <form className="space-y-4" onSubmit={(e) => (e.preventDefault(), void submit())}>
        <Field label="Current password">
          <Input type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} />
        </Field>
        <Field label="New password" hint="At least 10 characters">
          <Input type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} />
        </Field>
        <Field label="Confirm new password" error={error}>
          <Input type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
        </Field>
      </form>
    </Dialog>
  )
}
