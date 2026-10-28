import { useEffect, useState } from 'react'

export interface Theme {
  dark: boolean
  series: string[]
  seq: string[]
  text: string
  muted: string
  faint: string
  grid: string
  surface: string
  border: string
}

export function readTheme(): Theme {
  const cs = getComputedStyle(document.documentElement)
  const v = (n: string) => cs.getPropertyValue(n).trim()
  return {
    dark: document.documentElement.classList.contains('dark'),
    series: [1, 2, 3, 4, 5, 6, 7, 8].map((i) => v(`--series-${i}`)),
    seq: [0, 1, 2, 3, 4, 5].map((i) => v(`--seq-${i}`)),
    text: v('--fg'),
    muted: v('--fg-muted'),
    faint: v('--fg-subtle'),
    grid: v('--grid'),
    surface: v('--bg-elevated'),
    border: v('--border'),
  }
}

/** Current chart theme; re-reads tokens when the light/dark class changes. */
export function useChartTheme(): Theme {
  const [theme, setTheme] = useState<Theme>(readTheme)
  useEffect(() => {
    const obs = new MutationObserver(() => setTheme(readTheme()))
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => obs.disconnect()
  }, [])
  return theme
}

/** Series color by stable index (fixed order; callers pass the entity's index, never its rank). */
export function seriesColor(theme: Theme, index: number) {
  return theme.series[index % theme.series.length]
}
