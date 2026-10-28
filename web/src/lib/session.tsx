import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api, setCsrf, type Role, type User } from './api'
import { SessionContext } from '@/lib/sessionContext'

const rank: Record<Role, number> = { viewer: 1, operator: 2, admin: 3 }

export function SessionProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient()
  const [loading, setLoading] = useState(true)
  const [needsSetup, setNeedsSetup] = useState(false)
  const [user, setUser] = useState<User | null>(null)

  const refresh = useCallback(async () => {
    try {
      const s = await api.get<{ needs_setup: boolean }>('/setup')
      setNeedsSetup(s.needs_setup)
      if (!s.needs_setup) {
        try {
          const me = await api.get<{ user: User; csrf_token: string }>('/me')
          setCsrf(me.csrf_token)
          setUser(me.user)
        } catch {
          setUser(null)
        }
      }
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
    const onUnauthorized = () => setUser(null)
    window.addEventListener('gt:unauthorized', onUnauthorized)
    return () => window.removeEventListener('gt:unauthorized', onUnauthorized)
  }, [refresh])

  const signIn = useCallback((u: User, csrf: string) => {
    setCsrf(csrf)
    setUser(u)
    setNeedsSetup(false)
  }, [])

  const signOut = useCallback(async () => {
    try {
      await api.post('/auth/logout')
    } finally {
      setCsrf('')
      setUser(null)
      qc.clear()
    }
  }, [qc])

  const can = useCallback((role: Role) => !!user && rank[user.role] >= rank[role], [user])

  return <SessionContext.Provider value={{ loading, needsSetup, user, refresh, signIn, signOut, can }}>{children}</SessionContext.Provider>
}

