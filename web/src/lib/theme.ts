import { useSyncExternalStore } from 'react'
import { Monitor, Moon, Sun } from 'lucide-react'

/** The stored preference; 'system' is shown as "Auto" and follows the OS. */
export type ThemePref = 'light' | 'dark' | 'system'

export const themeOptions: { value: ThemePref; label: string }[] = [
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
  { value: 'system', label: 'Auto' },
]

export const themeIcons: Record<ThemePref, typeof Sun> = { light: Sun, dark: Moon, system: Monitor }

const key = 'gt-theme'
const media = () => window.matchMedia('(prefers-color-scheme: dark)')
const listeners = new Set<() => void>()

function read(): ThemePref {
  try {
    const t = localStorage.getItem(key)
    return t === 'light' || t === 'dark' ? t : 'system'
  } catch {
    return 'system'
  }
}

let pref: ThemePref = read()

function isDark(p: ThemePref) {
  return p === 'dark' || (p === 'system' && media().matches)
}

// public/theme.js sets the class before first paint; this keeps it in step afterwards.
function apply() {
  document.documentElement.classList.toggle('dark', isDark(pref))
  listeners.forEach((l) => l())
}

export function setTheme(p: ThemePref) {
  pref = p
  try {
    localStorage.setItem(key, p)
  } catch {
    /* private mode */
  }
  apply()
}

function subscribe(l: () => void) {
  if (listeners.size === 0) {
    media().addEventListener('change', apply)
    window.addEventListener('storage', onStorage)
  }
  listeners.add(l)
  return () => {
    listeners.delete(l)
    if (listeners.size === 0) {
      media().removeEventListener('change', apply)
      window.removeEventListener('storage', onStorage)
    }
  }
}

// Another tab changed the theme.
function onStorage(e: StorageEvent) {
  if (e.key !== key) return
  pref = read()
  apply()
}

/** The preference and whether the page is dark right now; Auto follows OS changes live. */
export function useTheme(): { pref: ThemePref; dark: boolean } {
  const p = useSyncExternalStore(subscribe, () => pref)
  const dark = useSyncExternalStore(subscribe, () => isDark(pref))
  return { pref: p, dark }
}
