import { createContext, useContext } from 'react'
import type { Role, User } from './api'

export interface SessionState {
  loading: boolean
  needsSetup: boolean
  user: User | null
  refresh: () => Promise<void>
  signIn: (user: User, csrf: string) => void
  signOut: () => Promise<void>
  can: (role: Role) => boolean
}

export const SessionContext = createContext<SessionState | null>(null)

export function useSession() {
  const ctx = useContext(SessionContext)
  if (!ctx) throw new Error('useSession outside SessionProvider')
  return ctx
}
