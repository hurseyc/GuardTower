import { useState } from 'react'

const unset = Symbol('unset')

/**
 * Calls onChange during render whenever key changes (and on first render),
 * React's recommended alternative to resetting state in an effect. Use a
 * primitive key, e.g. `open ? instance?.id ?? 'new' : null`, so refetched
 * objects don't trigger a reset.
 */
export function useOnChange<T>(key: T, onChange: (key: T) => void) {
  const [prev, setPrev] = useState<T | typeof unset>(unset)
  if (!Object.is(prev, key)) {
    setPrev(key)
    onChange(key)
  }
}
