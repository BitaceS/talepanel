import { useApi } from '~/composables/useApi'

// Live server events over Server-Sent Events (GET /servers/:id/stream).
//
// EventSource cannot send an Authorization header and the access token stays
// in memory, never in a URL. So every connection first asks for a single-use
// ticket (POST /servers/:id/stream-ticket, valid 30 s) and passes that
// instead. Because a ticket works once, EventSource's own auto-reconnect
// cannot succeed: on any error we close it and reconnect ourselves with a new
// ticket and ?after=<last log id>, so no line is lost or shown twice.
//
// After `maxFailures` connection attempts in a row that never opened, the
// composable gives up and reports mode 'polling'; the page then falls back to
// its HTTP polling loops. A stream that did open and later ended (the API
// closes streams after 30 min, or a network blip) reconnects quickly and does
// not count as a failure.

export interface StreamLogLine {
  id: number
  logged_at: string
  level: string
  message: string
}

export interface StreamMetrics {
  cpu: { usage_percent: number }
  memory: { used_mb: number; limit_mb: number }
  uptime_s: number
  note?: string
}

export type StreamMode = 'connecting' | 'live' | 'polling'

interface Options {
  maxLines?: number
  maxFailures?: number
}

export function useServerStream(serverId: Ref<string>, options: Options = {}) {
  const maxLines = options.maxLines ?? 1000
  const maxFailures = options.maxFailures ?? 3

  const config = useRuntimeConfig()
  const api = useApi()

  const logs = shallowRef<StreamLogLine[]>([])
  const status = ref<string | null>(null)
  const metrics = ref<StreamMetrics | null>(null)
  const mode = ref<StreamMode>('connecting')

  let source: EventSource | null = null
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined
  let flushTimer: ReturnType<typeof setTimeout> | undefined
  let failures = 0
  let lastId = 0
  let stopped = true
  let generation = 0 // invalidates callbacks of a replaced connection
  let pending: StreamLogLine[] = []
  const seen = new Set<number>()

  // Batch log lines so a burst of hundreds of lines is one reactive update.
  function queueLine(line: StreamLogLine) {
    if (seen.has(line.id)) return
    seen.add(line.id)
    if (line.id > lastId) lastId = line.id
    pending.push(line)
    if (!flushTimer) flushTimer = setTimeout(flushLines, 100)
  }

  function flushLines() {
    flushTimer = undefined
    if (pending.length === 0) return
    const next = logs.value.concat(pending)
    pending = []
    const trimmed = next.length > maxLines ? next.slice(next.length - maxLines) : next
    if (trimmed.length !== next.length) {
      for (const l of next.slice(0, next.length - maxLines)) seen.delete(l.id)
    }
    logs.value = trimmed
  }

  function closeSource() {
    if (source) {
      source.close()
      source = null
    }
  }

  function scheduleReconnect(delayMs: number) {
    clearTimeout(reconnectTimer)
    reconnectTimer = setTimeout(() => { void connect() }, delayMs)
  }

  function onFailure(opened: boolean) {
    closeSource()
    if (stopped) return
    if (opened) {
      // Was live: reconnect right away, resuming after the last line.
      failures = 0
      mode.value = 'connecting'
      scheduleReconnect(500)
      return
    }
    failures++
    if (failures >= maxFailures) {
      mode.value = 'polling'
      stopped = true
      return
    }
    mode.value = 'connecting'
    scheduleReconnect(Math.min(1000 * 2 ** (failures - 1), 30_000))
  }

  function parse<T>(e: Event): T | null {
    try {
      return JSON.parse((e as MessageEvent<string>).data) as T
    } catch {
      return null
    }
  }

  async function connect() {
    if (stopped || !serverId.value) return
    const gen = ++generation
    const id = serverId.value

    let ticket: string
    try {
      const res = await api.post<{ ticket: string }>(`/servers/${id}/stream-ticket`)
      ticket = res.ticket
    } catch {
      if (gen === generation) onFailure(false)
      return
    }
    if (gen !== generation || stopped) return

    const params = new URLSearchParams({ ticket })
    if (lastId > 0) params.set('after', String(lastId))
    const es = new EventSource(`${config.public.apiBase}/servers/${id}/stream?${params.toString()}`)
    source = es
    let opened = false

    es.onopen = () => {
      if (gen !== generation) return
      opened = true
      failures = 0
      mode.value = 'live'
    }
    es.onerror = () => {
      if (gen !== generation) return
      // The ticket is spent, so EventSource's built-in retry would only
      // fail; take over with a fresh ticket.
      onFailure(opened)
    }
    es.addEventListener('log', (e) => {
      if (gen !== generation) return
      const line = parse<StreamLogLine>(e)
      if (line) queueLine(line)
    })
    es.addEventListener('status', (e) => {
      if (gen !== generation) return
      const data = parse<{ status: string }>(e)
      if (data) status.value = data.status
    })
    es.addEventListener('metrics', (e) => {
      if (gen !== generation) return
      const data = parse<StreamMetrics>(e)
      if (data) metrics.value = data
    })
  }

  function reset() {
    clearTimeout(flushTimer)
    flushTimer = undefined
    pending = []
    seen.clear()
    lastId = 0
    logs.value = []
    status.value = null
    metrics.value = null
  }

  function start() {
    stop()
    stopped = false
    failures = 0
    mode.value = 'connecting'
    void connect()
  }

  function stop() {
    stopped = true
    generation++
    clearTimeout(reconnectTimer)
    closeSource()
  }

  watch(serverId, (id, old) => {
    if (id === old) return
    reset()
    if (id) start()
  })

  onMounted(() => { if (serverId.value) start() })
  onBeforeUnmount(() => {
    stop()
    clearTimeout(flushTimer)
  })

  return {
    logs: readonly(logs),
    status: readonly(status),
    metrics: readonly(metrics),
    mode: readonly(mode),
    /** Try the live stream again (e.g. after it fell back to polling). */
    restart: start,
    stop,
  }
}
