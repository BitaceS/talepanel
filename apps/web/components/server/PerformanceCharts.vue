<script setup lang="ts">
import { useElementSize } from '@vueuse/core'
import { useApi } from '~/composables/useApi'

// Per-server performance history: CPU, RAM and online players over a chosen
// window, as three small charts on one shared time axis (one measure per
// chart, no dual axes). Hovering one chart shows the same moment in all three.

const props = defineProps<{ serverId: string }>()

interface Point {
  t: string
  cpu: number
  cpu_max: number
  ram_mb: number
  ram_max_mb: number
  players: number
}
interface History {
  range: string
  bucket_s: number
  ram_limit_mb: number
  points: Point[]
}

const RANGES = [
  { key: '1h', label: '1 h' },
  { key: '6h', label: '6 h' },
  { key: '24h', label: '24 h' },
  { key: '7d', label: '7 d' },
] as const
type RangeKey = typeof RANGES[number]['key']
const WINDOW_MS: Record<RangeKey, number> = {
  '1h': 3_600_000, '6h': 21_600_000, '24h': 86_400_000, '7d': 604_800_000,
}

const api = useApi()
const { t } = useI18n()
const range = ref<RangeKey>('24h')
const data = ref<History | null>(null)
const loading = ref(false)
const error = ref('')
const showTable = ref(false)
// The window ends "now" as of the last fetch so the x axis stays still
// between refreshes.
const windowEnd = ref(Date.now())

async function load() {
  loading.value = true
  try {
    data.value = await api.get<History>(`/servers/${props.serverId}/metrics/history?range=${range.value}`)
    windowEnd.value = Date.now()
    error.value = ''
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    error.value = e.data?.error ?? e.message ?? t('servers.performance.loadFailed')
  } finally {
    loading.value = false
  }
}

let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  load()
  timer = setInterval(load, 60_000)
})
onBeforeUnmount(() => clearInterval(timer))
watch(range, () => { hoverT.value = null; load() })
watch(() => props.serverId, () => load())

const points = computed(() => (data.value?.points ?? []).map(p => ({ ...p, ms: new Date(p.t).getTime() })))
type P = typeof points.value[number]

// ── Geometry ────────────────────────────────────────────────────────────────
const box = ref<HTMLElement | null>(null)
const { width: boxWidth } = useElementSize(box)
const H = 120
const PAD = { l: 44, r: 12, t: 8, b: 20 }
const W = computed(() => Math.max(280, Math.round(boxWidth.value)))
const plotW = computed(() => W.value - PAD.l - PAD.r)
const plotH = H - PAD.t - PAD.b

const xStart = computed(() => windowEnd.value - WINDOW_MS[range.value])
function x(ms: number) {
  return PAD.l + ((ms - xStart.value) / WINDOW_MS[range.value]) * plotW.value
}

function niceCeil(v: number): number {
  if (v <= 0) return 1
  const mag = 10 ** Math.floor(Math.log10(v))
  for (const m of [1, 2, 2.5, 5, 10]) if (v <= m * mag) return m * mag
  return 10 * mag
}

// RAM scale on whole (even) gigabytes, so the gridlines read 4 GB / 8 GB
// rather than 4.9 GB / 9.8 GB.
function ramScale(mb: number): number {
  if (mb <= 1024) return niceCeil(mb)
  const gb = Math.ceil(mb / 1024)
  return (gb <= 2 ? gb : niceCeil(gb) % 2 ? niceCeil(gb) + 1 : niceCeil(gb)) * 1024
}

interface Series {
  key: 'cpu' | 'ram' | 'players'
  title: string
  value: (p: P) => number
  peak?: (p: P) => number
  max: number
  fmt: (v: number) => string
  limit?: number
}

function formatMB(mb: number): string {
  if (mb <= 0) return '0'
  return mb >= 1024 ? `${(mb / 1024).toFixed(mb >= 10240 ? 0 : 1)} GB` : `${Math.round(mb)} MB`
}

const series = computed<Series[]>(() => {
  const pts = points.value
  const cpuPeak = Math.max(0, ...pts.map(p => p.cpu_max))
  const ramPeak = Math.max(0, ...pts.map(p => p.ram_max_mb))
  const limit = data.value?.ram_limit_mb ?? 0
  const playersPeak = Math.max(0, ...pts.map(p => p.players))
  return [
    {
      key: 'cpu', title: t('servers.performance.cpu'), value: p => p.cpu, peak: p => p.cpu_max,
      // 100 % = one core; a busy server can use several.
      max: niceCeil(Math.max(100, cpuPeak)), fmt: v => (v <= 0 ? '0%' : `${v.toFixed(v < 10 ? 1 : 0)}%`),
    },
    {
      key: 'ram', title: t('servers.performance.memory'), value: p => p.ram_mb, peak: p => p.ram_max_mb,
      max: ramScale(Math.max(limit, ramPeak, 256)), fmt: formatMB, limit: limit > 0 ? limit : undefined,
    },
    {
      key: 'players', title: t('servers.performance.playersOnline'), value: p => p.players,
      max: Math.max(4, Math.ceil(niceCeil(playersPeak))), fmt: v => `${Math.round(v)}`,
    },
  ]
})

function y(v: number, max: number) {
  return PAD.t + plotH - (Math.min(v, max) / max) * plotH
}

// Runs of consecutive buckets; a missing bucket (server stopped, node
// offline) breaks the line instead of being bridged.
const runs = computed(() => {
  const gap = (data.value?.bucket_s ?? 60) * 1000 * 1.5
  const out: P[][] = []
  let cur: P[] = []
  for (const p of points.value) {
    const prev = cur[cur.length - 1]
    if (prev && p.ms - prev.ms > gap) { out.push(cur); cur = [] }
    cur.push(p)
  }
  if (cur.length) out.push(cur)
  return out
})

function linePath(s: Series) {
  return runs.value.map(run => run.length === 1
    // A lone bucket is a short tick so it stays visible.
    ? `M${(x(run[0]!.ms) - 1.5).toFixed(1)},${y(s.value(run[0]!), s.max).toFixed(1)}h3`
    : run.map((p, i) => `${i ? 'L' : 'M'}${x(p.ms).toFixed(1)},${y(s.value(p), s.max).toFixed(1)}`).join(''),
  ).join('')
}
function areaPath(s: Series) {
  const base = (PAD.t + plotH).toFixed(1)
  return runs.value.filter(r => r.length > 1).map((run) => {
    const line = run.map((p, i) => `${i ? 'L' : 'M'}${x(p.ms).toFixed(1)},${y(s.value(p), s.max).toFixed(1)}`).join('')
    return `${line}L${x(run[run.length - 1]!.ms).toFixed(1)},${base}L${x(run[0]!.ms).toFixed(1)},${base}Z`
  }).join('')
}

const xTicks = computed(() => {
  const n = W.value < 480 ? 3 : 5
  const span = WINDOW_MS[range.value]
  const long = range.value === '7d'
  return Array.from({ length: n }, (_, i) => {
    const ms = xStart.value + (span * (i + 0.5)) / n
    const d = new Date(ms)
    return {
      x: x(ms),
      label: long
        ? d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric' })
        : d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }),
    }
  })
})

// ── Hover (shared across the three charts) ──────────────────────────────────
const hoverT = ref<number | null>(null)
const hovered = computed<P | null>(() => {
  if (hoverT.value == null || !points.value.length) return null
  const tol = (data.value?.bucket_s ?? 60) * 1000
  let best: P | null = null
  for (const p of points.value) {
    if (!best || Math.abs(p.ms - hoverT.value) < Math.abs(best.ms - hoverT.value)) best = p
  }
  return best && Math.abs(best.ms - hoverT.value) <= tol ? best : null
})

function onMove(ev: PointerEvent) {
  const svg = ev.currentTarget as SVGSVGElement
  const rect = svg.getBoundingClientRect()
  const px = ((ev.clientX - rect.left) / rect.width) * W.value
  if (px < PAD.l || px > W.value - PAD.r) { hoverT.value = null; return }
  hoverT.value = xStart.value + ((px - PAD.l) / plotW.value) * WINDOW_MS[range.value]
}

function hoverTime(p: P) {
  const d = new Date(p.ms)
  return range.value === '7d'
    ? d.toLocaleString(undefined, { weekday: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
    : d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}
function tooltipLeft(p: P) {
  const px = x(p.ms)
  return px > W.value * 0.66 ? { right: `${W.value - px + 8}px` } : { left: `${px + 8}px` }
}

// Latest bucket per series, shown beside each title.
const latest = computed(() => points.value[points.value.length - 1] ?? null)
const recent = computed(() => latest.value && windowEnd.value - latest.value.ms < 5 * 60_000)
</script>

<template>
  <div class="bg-tp-surface rounded-xl overflow-hidden">
    <div class="px-5 py-3 border-b border-tp-border flex flex-wrap items-center justify-between gap-3">
      <h3 class="text-tp-text font-display font-semibold text-sm">{{ t('servers.performance.title') }}</h3>
      <div class="flex items-center gap-2">
        <div class="flex bg-tp-surface2 rounded-lg p-0.5" role="group" :aria-label="t('servers.performance.timeRange')">
          <button
            v-for="r in RANGES" :key="r.key" type="button"
            :aria-pressed="range === r.key"
            :class="['px-2.5 py-1 text-xs rounded-md transition-colors whitespace-nowrap',
                     range === r.key ? 'bg-tp-surface3 text-tp-text' : 'text-tp-muted hover:text-tp-text']"
            @click="range = r.key"
          >{{ r.label }}</button>
        </div>
        <button
          type="button" class="px-2.5 py-1 text-xs rounded-md text-tp-muted hover:text-tp-text"
          :aria-pressed="showTable" @click="showTable = !showTable"
        >{{ showTable ? t('servers.performance.charts') : t('servers.performance.table') }}</button>
      </div>
    </div>

    <div ref="box" class="px-5 py-4 space-y-4">
      <p v-if="error" class="text-tp-error text-sm">{{ error }}</p>

      <div v-else-if="!loading && points.length === 0" class="text-tp-muted text-sm py-6 text-center">
        {{ t('servers.performance.noSamples') }}
      </div>

      <template v-else-if="!showTable">
        <div v-for="s in series" :key="s.key" class="relative">
          <div class="flex items-baseline justify-between gap-3 mb-1">
            <span class="text-tp-muted text-xs font-medium truncate">{{ s.title }}</span>
            <span v-if="latest && recent" class="text-tp-text text-xs font-mono whitespace-nowrap">
              {{ s.fmt(s.value(latest)) }}<template v-if="s.limit"> / {{ s.fmt(s.limit) }}</template>
            </span>
          </div>
          <svg
            :viewBox="`0 0 ${W} ${H}`" :width="W" :height="H" class="block max-w-full touch-none"
            role="img" :aria-label="t('servers.performance.chartAria', { title: s.title, range })"
            @pointermove="onMove" @pointerleave="hoverT = null"
          >
            <!-- recessive grid: baseline, half, top -->
            <g class="text-tp-outline">
              <line v-for="f in [0, 0.5, 1]" :key="f"
                :x1="PAD.l" :x2="W - PAD.r" :y1="y(s.max * f, s.max)" :y2="y(s.max * f, s.max)"
                stroke="currentColor" :stroke-opacity="f === 0 ? 0.35 : 0.12" stroke-width="1" />
              <text v-for="f in [0, 0.5, 1]" :key="`l${f}`"
                :x="PAD.l - 6" :y="y(s.max * f, s.max) + 3" text-anchor="end"
                font-size="10" fill="currentColor" class="font-mono">{{ s.fmt(s.max * f) }}</text>
            </g>
            <!-- RAM limit as a labelled reference line -->
            <g v-if="s.limit" class="text-tp-muted">
              <line :x1="PAD.l" :x2="W - PAD.r" :y1="y(s.limit, s.max)" :y2="y(s.limit, s.max)"
                stroke="currentColor" stroke-dasharray="4 3" stroke-width="1" stroke-opacity="0.7" />
              <text :x="W - PAD.r" :y="y(s.limit, s.max) - 4" text-anchor="end" font-size="10" fill="currentColor">{{ t('servers.performance.limit') }}</text>
            </g>

            <g class="text-tp-primary">
              <path :d="areaPath(s)" fill="currentColor" fill-opacity="0.12" />
              <path :d="linePath(s)" fill="none" stroke="currentColor" stroke-width="2"
                stroke-linejoin="round" stroke-linecap="round" />
            </g>

            <g v-if="s.key === 'players'" class="text-tp-outline">
              <text v-for="tick in xTicks" :key="tick.label + tick.x" :x="tick.x" :y="H - 5" text-anchor="middle"
                font-size="10" fill="currentColor">{{ tick.label }}</text>
            </g>

            <!-- crosshair -->
            <g v-if="hovered">
              <line :x1="x(hovered.ms)" :x2="x(hovered.ms)" :y1="PAD.t" :y2="PAD.t + plotH"
                class="text-tp-muted" stroke="currentColor" stroke-opacity="0.5" stroke-width="1" />
              <circle :cx="x(hovered.ms)" :cy="y(s.value(hovered), s.max)" r="4"
                class="text-tp-primary fill-current stroke-tp-surface" stroke-width="2" />
            </g>
            <!-- full-height hit target -->
            <rect :x="PAD.l" :y="0" :width="plotW" :height="H" fill="transparent" />
          </svg>

          <div v-if="hovered"
            class="pointer-events-none absolute top-6 z-10 rounded-lg bg-tp-surface3 border border-tp-border px-2.5 py-1.5 text-xs shadow-lg whitespace-nowrap"
            :style="tooltipLeft(hovered)">
            <div class="text-tp-muted">{{ hoverTime(hovered) }}</div>
            <div class="text-tp-text font-mono">
              {{ s.fmt(s.value(hovered)) }}
              <span v-if="s.peak && s.peak(hovered) > s.value(hovered)" class="text-tp-muted">
                · {{ t('servers.performance.peak', { value: s.fmt(s.peak(hovered)) }) }}
              </span>
            </div>
          </div>
        </div>
        <p class="text-tp-outline text-[11px]">
          {{ t('servers.performance.footnote', {
            bucket: data && data.bucket_s >= 3600
              ? t('servers.performance.bucketHour')
              : t('servers.performance.bucketMinutes', { n: Math.round((data?.bucket_s ?? 60) / 60) }),
          }) }}
        </p>
      </template>

      <div v-else class="max-h-80 overflow-auto">
        <table class="w-full text-xs">
          <thead class="text-tp-muted text-left sticky top-0 bg-tp-surface">
            <tr>
              <th class="py-1.5 pr-3 font-medium">{{ t('servers.performance.columns.time') }}</th>
              <th class="py-1.5 pr-3 font-medium text-right">{{ t('servers.performance.columns.cpu') }}</th>
              <th class="py-1.5 pr-3 font-medium text-right">{{ t('servers.performance.columns.ram') }}</th>
              <th class="py-1.5 font-medium text-right">{{ t('servers.performance.columns.players') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-tp-border text-tp-text font-mono">
            <tr v-for="p in [...points].reverse()" :key="p.t">
              <td class="py-1 pr-3 font-sans text-tp-muted">{{ new Date(p.ms).toLocaleString() }}</td>
              <td class="py-1 pr-3 text-right">{{ p.cpu.toFixed(1) }}% / {{ p.cpu_max.toFixed(1) }}%</td>
              <td class="py-1 pr-3 text-right">{{ formatMB(p.ram_mb) }} / {{ formatMB(p.ram_max_mb) }}</td>
              <td class="py-1 text-right">{{ p.players }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
