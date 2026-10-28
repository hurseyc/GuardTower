// Chart primitives. Colors come from CSS tokens (validated palette in
// index.css) so light and dark each use their own selected steps.
import { useEffect, useMemo, useRef, useState } from 'react'
import * as echarts from 'echarts/core'
import { HeatmapChart, LineChart } from 'echarts/charts'
import { AxisPointerComponent, GridComponent, LegendComponent, TooltipComponent, VisualMapComponent } from 'echarts/components'
import { SVGRenderer } from 'echarts/renderers'
import { cn, fmtCompact, fmtNum } from '@/lib/utils'

echarts.use([LineChart, HeatmapChart, GridComponent, TooltipComponent, LegendComponent, AxisPointerComponent, VisualMapComponent, SVGRenderer])

/** Minimal ECharts host: one instance per element, resized with its container. */
function EChart({ option, height, label }: { option: echarts.EChartsCoreOption; height: number; label: string }) {
  const ref = useRef<HTMLDivElement>(null)
  const chart = useRef<echarts.ECharts | null>(null)
  useEffect(() => {
    if (!ref.current) return
    const c = echarts.init(ref.current, undefined, { renderer: 'svg' })
    chart.current = c
    const ro = new ResizeObserver(() => c.resize())
    ro.observe(ref.current)
    return () => {
      ro.disconnect()
      c.dispose()
      chart.current = null
    }
  }, [])
  useEffect(() => {
    chart.current?.setOption(option, { notMerge: true })
  }, [option])
  return <div ref={ref} role="img" aria-label={label} style={{ height, width: '100%' }} />
}

interface Theme {
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

function readTheme(): Theme {
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

function tooltipBase(t: Theme) {
  return {
    backgroundColor: t.surface,
    borderColor: t.border,
    borderWidth: 1,
    padding: [8, 10],
    textStyle: { color: t.text, fontSize: 12, fontFamily: 'Inter Variable, sans-serif' },
    extraCssText: 'border-radius:10px;box-shadow:var(--shadow-lg);',
  }
}

function fmtTime(ms: number, bucketMs: number) {
  const d = new Date(ms)
  if (bucketMs >= 3600_000 * 6) return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
  if (bucketMs >= 3600_000) return d.toLocaleString(undefined, { weekday: 'short', hour: 'numeric' })
  return d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
}

export interface LineSeries {
  name: string
  colorIndex: number
  data: [number, number][]
  area?: boolean
}

/** Time-series line chart with crosshair tooltip. One y-axis only. */
export function TimeSeriesChart({ series, bucketMs, height = 260 }: { series: LineSeries[]; bucketMs: number; height?: number }) {
  const t = useChartTheme()
  const option = useMemo(
    () => ({
      animation: false,
      grid: { left: 8, right: 16, top: 36, bottom: 8, containLabel: true },
      legend: series.length > 1 ? { top: 0, left: 0, icon: 'roundRect', itemWidth: 10, itemHeight: 10, textStyle: { color: t.muted, fontSize: 12 } } : undefined,
      tooltip: {
        trigger: 'axis',
        ...tooltipBase(t),
        axisPointer: { type: 'line', lineStyle: { color: t.faint, width: 1 } },
        formatter: (ps: { seriesName: string; color: string; value: [number, number] }[]) => {
          if (!ps.length) return ''
          const head = `<div style="color:${t.muted};margin-bottom:4px">${new Date(ps[0].value[0]).toLocaleString()}</div>`
          return (
            head +
            ps
              .map(
                (p) =>
                  `<div style="display:flex;align-items:center;gap:8px"><span style="width:8px;height:8px;border-radius:2px;background:${p.color}"></span>${p.seriesName}<b style="margin-left:auto;padding-left:16px">${fmtNum(p.value[1])}</b></div>`,
              )
              .join('')
          )
        },
      },
      xAxis: {
        type: 'time',
        axisLine: { lineStyle: { color: t.border } },
        axisTick: { show: false },
        axisLabel: { color: t.faint, fontSize: 11, hideOverlap: true, formatter: (v: number) => fmtTime(v, bucketMs) },
        splitLine: { show: false },
      },
      yAxis: {
        type: 'value',
        axisLabel: { color: t.faint, fontSize: 11, formatter: (v: number) => fmtCompact(v) },
        splitLine: { lineStyle: { color: t.grid } },
      },
      series: series.map((s) => ({
        name: s.name,
        type: 'line',
        showSymbol: false,
        symbolSize: 8,
        smooth: 0.2,
        lineStyle: { width: 2, color: seriesColor(t, s.colorIndex) },
        itemStyle: { color: seriesColor(t, s.colorIndex), borderColor: t.surface, borderWidth: 2 },
        areaStyle: s.area ? { color: seriesColor(t, s.colorIndex), opacity: t.dark ? 0.16 : 0.1 } : undefined,
        emphasis: { focus: 'series' },
        data: s.data,
      })),
    }),
    [series, bucketMs, t],
  )
  return <EChart option={option} height={height} label={`Line chart: ${series.map((s) => s.name).join(', ')} over time`} />
}

const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

/** Weekday × hour heatmap (sequential single hue). */
export function HourHeatmap({ grid, height = 220 }: { grid: number[][]; height?: number }) {
  const t = useChartTheme()
  const option = useMemo(() => {
    const data: [number, number, number][] = []
    let max = 0
    // Rows Monday-first reads naturally for most people.
    const order = [1, 2, 3, 4, 5, 6, 0]
    order.forEach((d, row) => {
      for (let h = 0; h < 24; h++) {
        const v = grid[d]?.[h] ?? 0
        max = Math.max(max, v)
        data.push([h, row, v])
      }
    })
    return {
      animation: false,
      grid: { left: 8, right: 8, top: 8, bottom: 28, containLabel: true },
      tooltip: {
        ...tooltipBase(t),
        formatter: (p: { value: [number, number, number] }) =>
          `${DAYS[order[p.value[1]]]} ${String(p.value[0]).padStart(2, '0')}:00–${String(p.value[0] + 1).padStart(2, '0')}:00<br/><b>${fmtNum(p.value[2])}</b> queries`,
      },
      xAxis: {
        type: 'category',
        data: Array.from({ length: 24 }, (_, h) => (h % 3 === 0 ? `${h}:00` : '')),
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: { color: t.faint, fontSize: 10, interval: 0 },
        splitArea: { show: false },
      },
      yAxis: {
        type: 'category',
        data: order.map((d) => DAYS[d]),
        inverse: true,
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: { color: t.faint, fontSize: 11 },
      },
      visualMap: {
        min: 0,
        max: Math.max(1, max),
        calculable: false,
        orient: 'horizontal',
        left: 'center',
        bottom: 0,
        itemHeight: 120,
        itemWidth: 10,
        textStyle: { color: t.faint, fontSize: 10 },
        inRange: { color: t.seq },
        show: max > 0,
      },
      series: [{ type: 'heatmap', data, itemStyle: { borderColor: t.surface, borderWidth: 2, borderRadius: 3 }, emphasis: { itemStyle: { borderColor: t.text } } }],
    }
  }, [grid, t])
  return <EChart option={option} height={height} label="Heatmap of queries by day of week and hour" />
}

/** Accessible HTML bar list: label, value and a thin bar. Text stays in text tokens. */
export function BarList({
  items,
  colorFor,
  max,
  emptyText = 'No data in this range',
  format = fmtNum,
  onSelect,
}: {
  items: { key: string; label?: string; value: number; sub?: string }[]
  colorFor?: (key: string, index: number) => string
  max?: number
  emptyText?: string
  format?: (n: number) => string
  onSelect?: (key: string) => void
}) {
  const m = max ?? Math.max(1, ...items.map((i) => i.value))
  if (items.length === 0) return <p className="py-6 text-center text-sm text-faint">{emptyText}</p>
  return (
    <ul className="space-y-1">
      {items.map((it, i) => (
        <li key={it.key}>
          <button
            type="button"
            disabled={!onSelect}
            onClick={() => onSelect?.(it.key)}
            className={cn('group w-full rounded-lg px-2 py-1.5 text-left', onSelect && 'hover:bg-hover')}
            title={onSelect ? `Filter the query log by ${it.label ?? it.key}` : undefined}
          >
            <div className="flex items-baseline gap-2 text-sm">
              <span className="min-w-0 flex-1 truncate font-mono text-[13px]">{it.label || it.key}</span>
              {it.sub && <span className="shrink-0 text-xs text-faint">{it.sub}</span>}
              <span className="shrink-0 tabular-nums text-muted">{format(it.value)}</span>
            </div>
            <div className="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-subtle">
              <div
                className="h-full rounded-full"
                style={{ width: `${Math.max(2, (it.value / m) * 100)}%`, background: colorFor ? colorFor(it.key, i) : 'var(--series-1)' }}
              />
            </div>
          </button>
        </li>
      ))}
    </ul>
  )
}

/** Tiny inline trend line for cards (no axes; tooltip via title). */
export function Sparkline({ values, color = 'var(--series-1)', height = 28 }: { values: number[]; color?: string; height?: number }) {
  if (values.length < 2) return <div style={{ height }} />
  const max = Math.max(1, ...values)
  const w = 100
  const pts = values.map((v, i) => `${(i / (values.length - 1)) * w},${height - 2 - (v / max) * (height - 4)}`).join(' ')
  return (
    <svg viewBox={`0 0 ${w} ${height}`} preserveAspectRatio="none" className="w-full" style={{ height }} aria-hidden="true">
      <polyline points={pts} fill="none" stroke={color} strokeWidth="2" vectorEffect="non-scaling-stroke" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  )
}
