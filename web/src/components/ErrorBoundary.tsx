import { Component, type ReactNode } from 'react'
import { AlertTriangle } from 'lucide-react'

/** Keeps one broken page from blanking the whole app; resets when `resetKey` changes. */
export class ErrorBoundary extends Component<{ children: ReactNode; resetKey?: string }, { error: Error | null; resetKey?: string }> {
  state = { error: null as Error | null, resetKey: this.props.resetKey }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  static getDerivedStateFromProps(props: { resetKey?: string }, state: { resetKey?: string }) {
    return props.resetKey !== state.resetKey ? { error: null, resetKey: props.resetKey } : null
  }

  componentDidCatch(error: Error) {
    console.error('GuardTower UI error', error)
  }

  render() {
    if (!this.state.error) return this.props.children
    return (
      <div className="m-6 rounded-xl border border-border bg-elevated p-6">
        <div className="flex items-center gap-2 font-semibold">
          <AlertTriangle className="h-5 w-5 text-warn" /> Something went wrong on this page
        </div>
        <p className="mt-2 text-sm text-muted">The rest of GuardTower still works. Reload the page to try again; if it keeps happening, please report it with the details below.</p>
        <pre className="mt-4 overflow-x-auto rounded-lg bg-subtle p-3 font-mono text-xs text-muted">{this.state.error.message}</pre>
        <button onClick={() => location.reload()} className="mt-4 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-accent-fg">
          Reload
        </button>
      </div>
    )
  }
}
