<script setup lang="ts">
import {
  Play, Square, RotateCcw, Zap,
  Cpu, Users,
  ArrowLeft, Terminal, FolderOpen, Puzzle,
  Globe, DatabaseBackup, Settings,
  Send, ChevronRight, Search, Download, Trash2, Package, ExternalLink,
  Gamepad2, Loader2, Plus, Database,
  File, FileText, FileArchive, FolderPlus, ArrowUpLeft, Save, X,
  Upload, PackageOpen, Copy, Info, CalendarClock, SlidersHorizontal,
} from '@lucide/vue'
import { useServersStore } from '~/stores/servers'
import { useModsStore } from '~/stores/mods'
import { useGameCommandsStore } from '~/stores/gameCommands'
import { useWorldsStore } from '~/stores/worlds'
import { usePlayersStore } from '~/stores/players'
import { useBackupsStore } from '~/stores/backups'
import { useInvitationsStore } from '~/stores/invitations'
import { useApi } from '~/composables/useApi'
import type { CFMod, ServerUpdateInfo } from '~/types'
import type { CommandParam, GameCommand } from '~/stores/gameCommands'

definePageMeta({ middleware: 'auth' })

const route = useRoute()
const { t } = useI18n()
const serversStore = useServersStore()
const api = useApi()

const worldsStore = useWorldsStore()
const playersStore = usePlayersStore()
const backupsStore = useBackupsStore()
const invitationsStore = useInvitationsStore()

const serverId = computed(() => route.params.id as string)

onMounted(async () => {
  await serversStore.fetchServer(serverId.value)
})

const server = computed(() => serversStore.currentServer)

// ── Live stream (SSE) ─────────────────────────────────────────────────────────
// Logs, status and metrics arrive over one Server-Sent Events stream. If it
// cannot connect (3 failed attempts in a row) the composable reports
// 'polling' and the HTTP polling loops below take over again.
const stream = useServerStream(serverId)
const streamLive = computed(() => stream.mode.value === 'live')
const streamPolling = computed(() => stream.mode.value === 'polling')

// A status event only carries the status; refetch the server so the rest
// (updated_at, last_exit_code, ...) matches.
watch(() => stream.status.value, (status) => {
  if (status && server.value && status !== server.value.status) {
    serversStore.fetchServer(serverId.value).catch(() => { /* silent */ })
  }
})

// ── Status polling ────────────────────────────────────────────────────────────
// Fallback while the live stream is not connected.
// While the status is transitional (installing/starting/stopping) we poll fast
// (4s) so the Start/Stop buttons unlock the moment the daemon reports the next
// stable status.  While stable we still poll, just slowly (15s), so spontaneous
// crashes — and the lag between a power action and the daemon's status push —
// surface without the user having to F5.
let statusTimer: ReturnType<typeof setInterval> | null = null
let statusTimerInterval = 0
const transitionalStatuses = new Set(['installing', 'starting', 'stopping'])

function ensureStatusPolling(intervalMs: number) {
  if (statusTimer && statusTimerInterval === intervalMs) return
  if (statusTimer) clearInterval(statusTimer)
  statusTimerInterval = intervalMs
  statusTimer = setInterval(async () => {
    if (!serverId.value) return
    try { await serversStore.fetchServer(serverId.value) } catch { /* silent */ }
  }, intervalMs)
}
function stopStatusPolling() {
  if (statusTimer) { clearInterval(statusTimer); statusTimer = null; statusTimerInterval = 0 }
}

watch([() => server.value?.status, streamLive], ([status, live]) => {
  if (!status || live) { stopStatusPolling(); return }
  ensureStatusPolling(transitionalStatuses.has(status) ? 4000 : 15000)
}, { immediate: true })

useHead({
  title: computed(() => server.value ? `${server.value.name} · TalePanel` : 'Server · TalePanel'),
})

// Short display ID
const displayId = computed(() => {
  if (!serverId.value) return ''
  const short = serverId.value.replace(/-/g, '').substring(0, 8).toUpperCase()
  return `DVR-${short.substring(0, 3)}-${short.substring(3, 5)}`
})

// ── Tabs ──────────────────────────────────────────────────────────────────────
type TabId = 'overview' | 'console' | 'game-control' | 'files' | 'config' | 'mods' | 'worlds' | 'players' | 'backups' | 'tasks' | 'databases' | 'settings'

interface Tab { id: TabId; label: string; icon: unknown }

// label holds the i18n key; the template translates it.
const tabs: Tab[] = [
  { id: 'overview',  label: 'server.tabs.overview',  icon: Cpu },
  { id: 'console',   label: 'server.tabs.console',   icon: Terminal },
  { id: 'game-control', label: 'server.tabs.gameControl', icon: Gamepad2 },
  { id: 'files',     label: 'server.tabs.files',     icon: FolderOpen },
  { id: 'config',    label: 'server.tabs.config',    icon: SlidersHorizontal },
  { id: 'mods',      label: 'server.tabs.mods',      icon: Puzzle },
  { id: 'worlds',    label: 'server.tabs.worlds',    icon: Globe },
  { id: 'players',   label: 'server.tabs.players',   icon: Users },
  { id: 'databases', label: 'server.tabs.databases', icon: Database },
  { id: 'backups',   label: 'server.tabs.backups',   icon: DatabaseBackup },
  { id: 'tasks',     label: 'server.tabs.schedules', icon: CalendarClock },
  { id: 'settings',  label: 'server.tabs.settings',  icon: Settings },
]

const activeTab = ref<TabId>('overview')

// ── Power Actions ─────────────────────────────────────────────────────────────
const actionLoading = reactive<Record<string, boolean>>({
  start: false, stop: false, restart: false, kill: false,
})

const toastMessage = ref('')
const toastType = ref<'success' | 'error'>('success')
let toastTimer: ReturnType<typeof setTimeout>

// Templates can't reach window globals; destructive buttons confirm through this.
function confirmAction(message: string): boolean {
  return window.confirm(message)
}

async function deleteServerAndLeave() {
  if (!confirmAction(t('server.settings.deleteConfirm'))) return
  try {
    await serversStore.deleteServer(serverId.value)
    await navigateTo('/servers')
  } catch {
    showToast(t('server.settings.deleteFailed'), 'error')
  }
}

function showToast(message: string, type: 'success' | 'error' = 'success') {
  toastMessage.value = message
  toastType.value = type
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toastMessage.value = '' }, 3000)
}

// ── Launch settings (RAM + allowlisted Hytale flags; apply on next start) ──
// key: i18n key under server.settings.launch.flags (label + hint).
const LAUNCH_FLAG_OPTIONS = [
  { flag: '--accept-early-plugins', key: 'acceptEarlyPlugins' },
  { flag: '--allow-op', key: 'allowOp', adminOnly: true },
  { flag: '--disable-sentry', key: 'disableSentry' },
]
const launchForm = reactive({ ram_limit_mb: 0, launch_flags: [] as string[], use_hytale_account: false, hytale_version: 'latest' })
// Stored as 'latest' / 'release' / 'pre-release'; 'latest' and 'release' are the same channel.
const normChannel = (v?: string | null) => (v === 'pre-release' ? 'pre-release' : 'latest')
const launchSaving = ref(false)

watch(server, (s) => {
  if (!s) return
  launchForm.ram_limit_mb = s.ram_limit_mb ?? 0
  launchForm.launch_flags = [...(s.launch_flags ?? [])]
  launchForm.use_hytale_account = !!s.use_hytale_account
  launchForm.hytale_version = normChannel(s.hytale_version)
}, { immediate: true })

// ── Network routing (hub / gamemode role for the bundled network mod) ──────
// Needs the global network.manage permission (admins and owners).
// Label and hint: server.settings.network.roles.<value>.
const NETWORK_ROLE_OPTIONS = [
  { value: 'none' },
  { value: 'hub' },
  { value: 'gamemode' },
] as const
type NetworkRoleOption = typeof NETWORK_ROLE_OPTIONS[number]['value']
const networkRole = ref<NetworkRoleOption>('none')
const networkSaving = ref(false)
const networkDistributing = ref(false)

watch(server, (s) => {
  if (!s) return
  networkRole.value = s.network_role ?? 'none'
}, { immediate: true })

const networkRoleChanged = computed(() => networkRole.value !== (server.value?.network_role ?? 'none'))

async function saveNetworkRole() {
  if (!server.value) return
  networkSaving.value = true
  try {
    await api.post(`/network/servers/${serverId.value}/role`, { role: networkRole.value })
    await serversStore.fetchServer(serverId.value)
    showToast(t('server.settings.network.roleSaved'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? t('server.settings.network.roleSaveFailed'), 'error')
  } finally {
    networkSaving.value = false
  }
}

async function redistributeNetworkConfig() {
  networkDistributing.value = true
  try {
    await api.post('/network/distribute')
    showToast(t('server.settings.network.distributed'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? t('server.settings.network.distributeFailed'), 'error')
  } finally {
    networkDistributing.value = false
  }
}

async function saveLaunchSettings() {
  if (!server.value) return
  launchSaving.value = true
  try {
    const body: Record<string, unknown> = { launch_flags: launchForm.launch_flags }
    if (authStore.isAdmin && launchForm.ram_limit_mb !== server.value.ram_limit_mb) {
      body.ram_limit_mb = Number(launchForm.ram_limit_mb)
    }
    if (launchForm.use_hytale_account !== !!server.value.use_hytale_account) {
      body.use_hytale_account = launchForm.use_hytale_account
    }
    const channelChanged = launchForm.hytale_version !== normChannel(server.value.hytale_version)
    if (channelChanged) body.hytale_version = launchForm.hytale_version
    await api.patch(`/servers/${serverId.value}`, body)
    await serversStore.fetchServer(serverId.value)
    if (channelChanged) await loadUpdateInfo()
    showToast(server.value?.status === 'running' ? t('server.settings.launch.savedRestart') : t('server.settings.launch.saved'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? t('server.settings.launch.saveFailed'), 'error')
  } finally {
    launchSaving.value = false
  }
}

async function runAction(action: 'start' | 'stop' | 'restart' | 'kill') {
  if (!server.value) return
  actionLoading[action] = true
  try {
    await serversStore[`${action}Server`](serverId.value)
    showToast(t(`server.power.sent.${action}`))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? e.message ?? t(`server.power.failed.${action}`), 'error')
  } finally {
    actionLoading[action] = false
  }
}

// ── Reinstall (Hytale exit codes 8 / 11) ──────────────────────────────────────
// 8: Hytale staged a self-update and expected its start script to apply it.
// 11: Hytale refused to boot from inside the launcher's install directory.
// Both need fresh server files; the API remembers the last crash's exit code.
const reinstallExitCode = computed(() => {
  const code = server.value?.last_exit_code
  return code === 8 || code === 11 ? code : null
})
const reinstalling = ref(false)

async function reinstallServer() {
  if (!server.value) return
  if (!confirmAction(t('server.reinstall.confirm'))) return
  reinstalling.value = true
  try {
    await serversStore.reinstallServer(serverId.value)
    showToast(t('server.reinstall.started'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? e.message ?? t('server.reinstall.failed'), 'error')
  } finally {
    reinstalling.value = false
  }
}

// ── Hytale updates ────────────────────────────────────────────────────────────
// The panel tracks which build it installed; GET /update compares it with the
// newest build of the server's channel. "Update now" backs up, stops,
// installs and starts again; scheduled updates live in the Schedules tab.
const updateInfo = ref<ServerUpdateInfo | null>(null)
const updateStarting = ref(false)
const updateWarnMinutes = ref(0)

async function loadUpdateInfo() {
  try {
    updateInfo.value = await serversStore.getUpdateInfo(serverId.value)
  } catch {
    updateInfo.value = null // older API or no access: hide the hint
  }
}

const patchlineLabel = computed(() =>
  updateInfo.value?.patchline === 'pre-release' ? t('server.update.preRelease') : t('server.update.release'))

async function startUpdate() {
  const info = updateInfo.value
  if (!server.value || !info) return
  const running = server.value.status === 'running'
  const warn = running ? Math.min(15, Math.max(0, Number(updateWarnMinutes.value) || 0)) : 0
  const version = info.latest
  const steps = running
    ? (warn > 0
        ? t('server.update.stepsRunningWarn', { n: warn, version }, warn)
        : t('server.update.stepsRunning', { version }))
    : t('server.update.stepsStopped', { version })
  if (!confirmAction(t('server.update.confirm', { version, steps }))) return
  updateStarting.value = true
  try {
    updateInfo.value = await serversStore.startUpdate(serverId.value, warn)
    showToast(t('server.update.started'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? e.message ?? t('server.update.failed'), 'error')
    await loadUpdateInfo()
  } finally {
    updateStarting.value = false
  }
}

// Re-check when the server settles (an update or reinstall just finished).
watch(() => server.value?.status, (now, before) => {
  if (now && now !== before && ['running', 'stopped', 'crashed'].includes(now)) loadUpdateInfo()
})
onMounted(loadUpdateInfo)

const canStart   = computed(() => server.value?.status === 'stopped' || server.value?.status === 'crashed')
const canStop    = computed(() => server.value?.status === 'running' || server.value?.status === 'starting')
const canRestart = computed(() => server.value?.status === 'running')
const canKill    = computed(() => server.value?.status !== 'stopped' && server.value?.status !== 'installing')

// ── Migrate to Node ───────────────────────────────────────────────────────────
const showMigrateModal = ref(false)
const migrateNodes = ref<Array<{id: string, name: string, status: string}>>([])
const migrateTargetId = ref('')
const migrateLoading = ref(false)
const migrateError = ref('')

const isAdmin = computed(() => {
  const role = authStore.user?.role
  return role === 'admin' || role === 'owner'
})

async function openMigrateModal() {
  showMigrateModal.value = true
  migrateError.value = ''
  migrateTargetId.value = ''
  try {
    const data = await api.get<{ nodes: Array<{id: string, name: string, status: string}> }>('/nodes')
    migrateNodes.value = (data.nodes ?? []).filter(n => n.id !== server.value?.node_id && n.status === 'online')
  } catch {
    migrateError.value = t('server.migrate.loadNodesFailed')
  }
}

async function submitMigrate() {
  if (!migrateTargetId.value || !serverId.value) return
  migrateLoading.value = true
  migrateError.value = ''
  try {
    // Migration transfers the server's data synchronously and can take a while
    // for large worlds; the request resolves once the move is complete.
    await api.post(`/servers/${serverId.value}/migrate`, { target_node_id: migrateTargetId.value })
    showMigrateModal.value = false
    showToast(t('server.migrate.migrated'))
    await serversStore.fetchServer(serverId.value)
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    migrateError.value = e.data?.error ?? e.message ?? t('server.migrate.failed')
  } finally {
    migrateLoading.value = false
  }
}

// ── Metrics polling ───────────────────────────────────────────────────────────
interface Metrics {
  cpu: { usage_percent: number }
  memory: { used_mb: number; limit_mb: number }
  uptime_s: number
  note?: string
}

const metrics = ref<Metrics | null>(null)
let metricsTimer: ReturnType<typeof setInterval> | undefined

// Players online right now, from the sessions the panel records (the same
// source as the dashboard). The players store holds everyone ever seen.
const onlinePlayers = ref<number | null>(null)
async function fetchOnlinePlayers() {
  if (!serverId.value) return
  try {
    const data = await api.get<{ online_by_server: Record<string, number> }>('/servers/activity')
    onlinePlayers.value = data.online_by_server?.[serverId.value] ?? 0
  } catch { /* silent */ }
}

async function fetchMetrics() {
  if (!serverId.value || activeTab.value !== 'overview') return
  try {
    const data = await api.get<Metrics>(`/servers/${serverId.value}/metrics`)
    metrics.value = data
  } catch { /* silent */ }
}

// Online players come from the session table, not from the stream or the
// metrics: refresh them on their own, slower timer in every mode.
let onlinePlayersTimer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void fetchOnlinePlayers()
  onlinePlayersTimer = setInterval(fetchOnlinePlayers, 15000)
})
onUnmounted(() => clearInterval(onlinePlayersTimer))

// The stream pushes metrics every 5 s; poll only as the fallback.
watch(() => stream.metrics.value, (m) => {
  if (m && !streamPolling.value) metrics.value = { ...m, cpu: { ...m.cpu }, memory: { ...m.memory } }
})

watch([activeTab, streamPolling], ([tab, polling]) => {
  clearInterval(metricsTimer)
  metricsTimer = undefined
  if (tab === 'overview' && polling) {
    fetchMetrics()
    metricsTimer = setInterval(fetchMetrics, 5000)
  }
}, { immediate: true })

watch(activeTab, (tab) => {
  if (tab === 'worlds') worldsStore.fetchWorlds(serverId.value)
  if (tab === 'players') playersStore.fetchPlayers(serverId.value)
  if (tab === 'databases') fetchDatabases()
  if (tab === 'settings') loadServerInvitations()
  if (tab === 'backups') {
    backupsStore.fetchBackups(serverId.value)
    backupsStore.fetchSchedules(serverId.value)
  }
}, { immediate: true })

onUnmounted(() => {
  clearInterval(metricsTimer)
  clearInterval(logsTimer)
  stopStatusPolling()
})

function formatUptime(s: number): string {
  if (s <= 0) return '\u2014'
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${sec}s`
  return `${sec}s`
}

// ── Console (logs + command input) ────────────────────────────────────────────
interface LogLine {
  id: number
  logged_at: string
  level: string
  message: string
}

const logs = ref<LogLine[]>([])
const consoleEl = ref<HTMLElement | null>(null)
let logsTimer: ReturnType<typeof setInterval> | undefined
const userScrolledUp = ref(false)

function onConsoleScroll() {
  const el = consoleEl.value
  if (!el) return
  // Consider "at bottom" if within 60px of the bottom
  userScrolledUp.value = el.scrollTop + el.clientHeight < el.scrollHeight - 60
}

// ── Hytale OAuth detection ────────────────────────────────────────────────────
// The Hytale Downloader prints a device-code URL during provisioning that the
// operator MUST open in a browser before downloads can start.  Buried in a
// scrolling log it's easy to miss, so we surface it as a modal as soon as we
// see it and dismiss it once provisioning moves past 'installing'.
const hytaleAuthUrl = ref('')
const hytaleAuthCode = ref('')
const hytaleAuthDismissed = ref(false)
// Two flows ship in different Hytale releases:
//   (1) Device code:    /oauth2/device/verify?user_code=XXXX
//   (2) Auth code+PKCE: /oauth2/auth?response_type=code&client_id=hytale-server&...
const hytaleDeviceRegex = /https:\/\/oauth\.accounts\.hytale\.com\/oauth2\/device\/verify\?user_code=([A-Za-z0-9_-]+)/
const hytaleAuthCodeRegex = /https:\/\/oauth\.accounts\.hytale\.com\/oauth2\/auth\?[^\s"'<>]+/

watch(logs, (lines) => {
  if (hytaleAuthUrl.value || hytaleAuthDismissed.value) return
  // Players control chat, and chat lands in this log: a player could post
  // their own device-login URL and have the operator approve it. The link only
  // counts while provisioning (no game server, so no chat) and only from lines
  // logged after the server entered 'installing' (updated_at moves with status;
  // 60s of slack for clock skew between node and panel).
  if (server.value?.status !== 'installing') return
  const since = Date.parse(server.value.updated_at) - 60_000
  for (const line of lines) {
    if (!(Date.parse(line.logged_at) >= since)) continue
    const dm = hytaleDeviceRegex.exec(line.message)
    if (dm) {
      hytaleAuthUrl.value = dm[0]
      hytaleAuthCode.value = dm[1] ?? ''
      break
    }
    const am = hytaleAuthCodeRegex.exec(line.message)
    if (am) {
      hytaleAuthUrl.value = am[0]
      hytaleAuthCode.value = ''
      break
    }
  }
}, { deep: true })

const showHytaleAuthModal = computed(() =>
  !!hytaleAuthUrl.value && !hytaleAuthDismissed.value
)

async function copyHytaleAuthCode() {
  try {
    await navigator.clipboard.writeText(hytaleAuthCode.value)
    showToast(t('server.hytaleAuth.codeCopied'))
  } catch {
    showToast(t('server.hytaleAuth.copyFailed'), 'error')
  }
}

async function fetchLogs() {
  if (!serverId.value || activeTab.value !== 'console') return
  try {
    const data = await api.get<{ logs: LogLine[] }>(`/servers/${serverId.value}/logs`)
    logs.value = data.logs ?? []
    await nextTick()
    if (!userScrolledUp.value) {
      scrollConsoleToBottom()
    }
  } catch { /* silent */ }
}

function scrollConsoleToBottom() {
  if (consoleEl.value) {
    consoleEl.value.scrollTop = consoleEl.value.scrollHeight
  }
}

async function scrollConsoleIfFollowing() {
  await nextTick()
  if (!userScrolledUp.value) scrollConsoleToBottom()
}

// Live: the stream's lines are the console. Polling fallback: fetchLogs.
watch(() => stream.logs.value, (lines) => {
  if (streamPolling.value) return
  logs.value = [...lines]
  if (activeTab.value === 'console') scrollConsoleIfFollowing()
})

watch([activeTab, streamPolling], ([tab, polling]) => {
  clearInterval(logsTimer)
  logsTimer = undefined
  if (tab !== 'console') return
  if (polling) {
    fetchLogs()
    logsTimer = setInterval(fetchLogs, 1000)
  } else {
    scrollConsoleIfFollowing()
  }
})

const consoleInput = ref('')
const sendingCmd = ref(false)

async function sendCommand() {
  const cmd = consoleInput.value.trim()
  if (!cmd || sendingCmd.value) return
  sendingCmd.value = true
  try {
    await api.post(`/servers/${serverId.value}/console`, { cmd })
    consoleInput.value = ''
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? t('server.console.sendFailed'), 'error')
  } finally {
    sendingCmd.value = false
  }
}

function handleConsoleKey(e: KeyboardEvent) {
  if (e.key === 'Enter') sendCommand()
}

async function triggerHytaleAuth() {
  if (sendingCmd.value || server.value?.status !== 'running') return
  hytaleAuthDismissed.value = false
  sendingCmd.value = true
  try {
    await api.post(`/servers/${serverId.value}/console`, { cmd: 'auth login device' })
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? t('server.console.sendFailed'), 'error')
  } finally {
    sendingCmd.value = false
  }
}

function logLevelColor(level: string): string {
  switch (level?.toLowerCase()) {
    case 'error': case 'fatal': return 'text-tp-error'
    case 'warn':  case 'warning': return 'text-tp-warning'
    case 'info':  return 'text-tp-tertiary'
    case 'system': return 'text-tp-warning'
    case 'debug': return 'text-tp-outline'
    default:      return 'text-tp-muted'
  }
}

function formatLogTime(iso: string): string {
  try {
    return new Date(iso).toLocaleTimeString('en-US', { hour12: false })
  } catch { return '' }
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

const urlRegex = /(https?:\/\/[^\s"'<>]+)/g

// The Hytale server writes ANSI colour/reset sequences (ESC [ ... m) into its
// log; drop them so they don't show up as "[m" noise in the console.
// eslint-disable-next-line no-control-regex -- matching ESC is the point
const ansiRegex = /\x1b\[[0-9;?]*[ -/]*[@-~]/g

function renderLogMessage(message: string): string {
  const safe = escapeHtml(message.replace(ansiRegex, ''))
  return safe.replace(urlRegex, (url) =>
    `<a href="${url}" target="_blank" rel="noopener noreferrer" class="underline text-tp-accent hover:text-tp-primary break-all">${url}</a>`
  )
}

// ── Clipboard copy ───────────────────────────────────────────────────────────
const copiedField = ref('')
async function copyToClipboard(text: string, field: string) {
  try {
    await navigator.clipboard.writeText(text)
    copiedField.value = field
    setTimeout(() => { copiedField.value = '' }, 2000)
  } catch {
    showToast(t('server.console.copyFailed'), 'error')
  }
}

const serverAddress = computed(() => {
  if (!server.value) return ''
  const host = server.value.node_fqdn?.trim()
  if (!host) return `:${server.value.port}`
  return `${host}:${server.value.port}`
})

// ── Mods ──────────────────────────────────────────────────────────────────────
const modsStore = useModsStore()
const modsTab = ref<'installed' | 'browse' | 'modtale'>('installed')
const modSearch = ref('')
let modSearchTimer: ReturnType<typeof setTimeout>

const safeSearchResults = computed<CFMod[]>(() =>
  Array.isArray(modsStore.searchResults) ? modsStore.searchResults : []
)

function loadBrowse() {
  if (safeSearchResults.value.length === 0 && !modsStore.loadingSearch) {
    modsStore.search(modSearch.value.trim())
  }
}

watch(activeTab, (tab) => {
  if (tab === 'mods') {
    modsStore.fetchInstalled(serverId.value)
    if (modsTab.value === 'browse') loadBrowse()
  }
})

watch(modsTab, (tab) => {
  if (tab === 'browse') loadBrowse()
})

function onModSearchInput() {
  clearTimeout(modSearchTimer)
  modSearchTimer = setTimeout(() => {
    modsStore.search(modSearch.value.trim())
  }, 400)
}

async function installMod(mod: CFMod) {
  const file = mod.latestFiles[0]
  if (!file) return
  try {
    await modsStore.install(serverId.value, {
      filename: file.fileName,
      display_name: mod.name,
      version: file.displayName,
      download_url: file.downloadUrl,
      cf_mod_id: mod.id,
      cf_file_id: file.id,
    })
    showToast(t('server.mods.installedToast', { name: mod.name }))
  } catch {
    showToast(modsStore.error ?? t('server.mods.installFailed'), 'error')
  }
}

async function removeMod(filename: string, displayName: string, dir: 'mods' | 'plugins' = 'mods') {
  try {
    await modsStore.remove(serverId.value, filename, dir)
    showToast(t('server.mods.removedToast', { name: displayName }))
  } catch {
    showToast(modsStore.error ?? t('server.mods.removeFailed'), 'error')
  }
}

function isInstalled(cfModId: number): boolean {
  return modsStore.installed.some(m => m.cf_mod_id === cfModId)
}

// ── Game Control ─────────────────────────────────────────────────────────────
const gameCmdStore = useGameCommandsStore()
const authStore = useAuthStore()
const activeCmdCategory = ref<string | null>(null)
const commandParams = ref<Record<string, Record<string, string>>>({})
const showAddCommand = ref(false)
// Example for the params placeholder; JSON, so not translated.
const PARAMS_EXAMPLE = '[{"name":"player","type":"string","required":true,"placeholder":"Player name"}]'
const newCommand = ref({
  category: '',
  name: '',
  description: '',
  command_template: '',
  icon: 'terminal',
  params: '[]',
  sort_order: 0,
  min_role: 'user',
})

watch(activeTab, async (tab) => {
  if (tab === 'game-control') {
    await gameCmdStore.fetchCommands(serverId.value)
    // Auto-select first category
    if (!activeCmdCategory.value && gameCmdStore.categories.length > 0) {
      activeCmdCategory.value = gameCmdStore.categories[0] ?? null
    }
  }
})

const activeCategoryCommands = computed(() => {
  if (!activeCmdCategory.value) return []
  return gameCmdStore.grouped[activeCmdCategory.value] ?? []
})

const roleWeights: Record<string, number> = { user: 1, moderator: 2, admin: 3, owner: 4 }

function userCanExecute(cmd: GameCommand): boolean {
  const userRole = authStore.user?.role ?? 'user'
  return (roleWeights[userRole] ?? 0) >= (roleWeights[cmd.min_role] ?? 0)
}

function roleBadgeColor(role: string): string {
  switch (role) {
    case 'admin': return 'bg-red-500/10 text-red-400'
    case 'moderator': return 'bg-yellow-500/10 text-yellow-400'
    case 'owner': return 'bg-purple-500/10 text-purple-400'
    default: return 'bg-green-500/10 text-green-400'
  }
}

function getParamValues(cmdId: string): Record<string, string> {
  if (!commandParams.value[cmdId]) {
    commandParams.value[cmdId] = {}
  }
  return commandParams.value[cmdId]
}

function hasRequiredParams(cmd: GameCommand): boolean {
  const params = Array.isArray(cmd.params) ? cmd.params : []
  if (params.length === 0) return true
  const values = getParamValues(cmd.id)
  return params.every(p => !p.required || (values[p.name]?.trim() ?? '') !== '')
}

async function executeCmd(cmd: GameCommand) {
  const params = getParamValues(cmd.id)
  await gameCmdStore.executeCommand(serverId.value, cmd, params)
  if (gameCmdStore.lastResult) {
    showToast(t('server.gameControl.sentToast', { command: gameCmdStore.lastResult.command }))
  }
  if (gameCmdStore.error) {
    showToast(gameCmdStore.error, 'error')
  }
}

async function addCustomCommand() {
  let parsedParams: CommandParam[] = []
  try {
    parsedParams = JSON.parse(newCommand.value.params)
  } catch {
    showToast(t('server.gameControl.invalidParams'), 'error')
    return
  }
  await gameCmdStore.createCommand(serverId.value, {
    ...newCommand.value,
    params: parsedParams,
  })
  if (!gameCmdStore.error) {
    showToast(t('server.gameControl.added'))
    showAddCommand.value = false
    newCommand.value = { category: '', name: '', description: '', command_template: '', icon: 'terminal', params: '[]', sort_order: 0, min_role: 'user' }
  } else {
    showToast(gameCmdStore.error, 'error')
  }
}

async function deleteCmd(cmdId: string) {
  await gameCmdStore.deleteCommand(serverId.value, cmdId)
  if (!gameCmdStore.error) showToast(t('server.gameControl.removed'))
}

watch(activeTab, (tab) => {
  if (tab === 'game-control') {
    modsStore.fetchInstalled(serverId.value)
  }
})

// ── Databases ────────────────────────────────────────────────────────────────
// Each server has at most one MariaDB database; name/user/password are
// auto-generated server-side. The API returns a single object under `database`.
interface ServerDatabase {
  id: string
  server_id: string
  db_name: string
  db_user: string
  db_password: string
  host: string
  port: number
  created_at: string
}

const database = ref<ServerDatabase | null>(null)
const dbLoading = ref(false)
const creatingDb = ref(false)

async function fetchDatabases() {
  dbLoading.value = true
  try {
    const data = await api.get<{ database: ServerDatabase }>(`/servers/${serverId.value}/database`)
    database.value = data.database ?? null
  } catch {
    // 404 => no database provisioned yet
    database.value = null
  } finally {
    dbLoading.value = false
  }
}

async function createDatabase() {
  creatingDb.value = true
  try {
    const data = await api.post<{ database: ServerDatabase }>(`/servers/${serverId.value}/database`, {})
    database.value = data.database
    showToast(t('server.databases.created'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? t('server.databases.createFailed'), 'error')
  } finally {
    creatingDb.value = false
  }
}

async function deleteDatabase() {
  if (!confirm(t('server.databases.deleteConfirm'))) return
  try {
    await api.delete(`/servers/${serverId.value}/database`)
    database.value = null
    showToast(t('server.databases.deleted'))
  } catch { showToast(t('server.databases.deleteFailed'), 'error') }
}

const rotatingDb = ref(false)
const rotatedCreds = ref<ServerDatabase | null>(null)

// ── Server invitations (shared access) ──────────────────────────────────────
const inviteForm = reactive({ email: '', role: 'viewer' })
const invitingEmail = ref(false)

async function loadServerInvitations() {
  try {
    await invitationsStore.fetchServerInvitations(serverId.value)
  } catch { /* surfaced via store */ }
}

async function createServerInvitation() {
  if (!inviteForm.email) return
  invitingEmail.value = true
  try {
    await invitationsStore.createInvitation(serverId.value, inviteForm.email, inviteForm.role)
    inviteForm.email = ''
    showToast(t('server.settings.access.sent'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? t('server.settings.access.inviteFailed'), 'error')
  } finally {
    invitingEmail.value = false
  }
}

async function revokeServerInvitation(invitationId: string) {
  if (!confirm(t('server.settings.access.revokeConfirm'))) return
  try {
    await invitationsStore.revokeInvitation(serverId.value, invitationId)
    showToast(t('server.settings.access.revoked'))
  } catch {
    showToast(t('server.settings.access.revokeFailed'), 'error')
  }
}

async function rotateDatabasePassword() {
  if (!database.value) return
  if (!confirm(t('server.databases.rotateConfirm'))) return
  rotatingDb.value = true
  try {
    const res = await api.post<{ database: ServerDatabase }>(
      `/servers/${serverId.value}/database/reset-password`,
      {},
    )
    database.value = res.database
    rotatedCreds.value = res.database
    showToast(t('server.databases.rotated'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? t('server.databases.rotateFailed'), 'error')
  } finally {
    rotatingDb.value = false
  }
}

// ── File Browser ─────────────────────────────────────────────────────────────
interface FileEntry {
  name: string
  size: number
  is_dir: boolean
  modified: string
}

const filePath = ref('/')
const fileEntries = ref<FileEntry[]>([])
const filesLoading = ref(false)
const filesError = ref('')
const editingFile = ref<{ path: string; content: string; name: string } | null>(null)
const editContent = ref('')
const savingFile = ref(false)
const newFolderName = ref('')
const showNewFolder = ref(false)
const creatingFolder = ref(false)
const deletingPath = ref('')

const breadcrumbs = computed(() => {
  const parts = filePath.value.split('/').filter(Boolean)
  const crumbs = [{ label: t('server.files.root'), path: '/' }]
  let current = ''
  for (const part of parts) {
    current += '/' + part
    crumbs.push({ label: part, path: current })
  }
  return crumbs
})

async function fetchFiles(path?: string) {
  if (path !== undefined) filePath.value = path
  filesLoading.value = true
  filesError.value = ''
  editingFile.value = null
  try {
    const data = await api.get<{ entries: FileEntry[]; path: string }>(`/servers/${serverId.value}/files`, { path: filePath.value })
    fileEntries.value = data.entries ?? []
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    filesError.value = e.data?.error ?? e.message ?? t('server.files.loadFailed')
  } finally {
    filesLoading.value = false
  }
}

function navigateToDir(name: string) {
  const newPath = filePath.value === '/' ? '/' + name : filePath.value + '/' + name
  fetchFiles(newPath)
}

async function openFile(entry: FileEntry) {
  if (entry.is_dir) {
    navigateToDir(entry.name)
    return
  }
  // Only open text files (< 1MB)
  if (entry.size > 1_048_576) {
    showToast(t('server.files.tooLarge'), 'error')
    return
  }
  const ext = entry.name.split('.').pop()?.toLowerCase() ?? ''
  if (ext === 'jar') {
    showToast(t('server.files.binary'), 'error')
    return
  }
  try {
    const fpath = filePath.value === '/' ? '/' + entry.name : filePath.value + '/' + entry.name
    const data = await api.get<{ content: string }>(`/servers/${serverId.value}/files/content`, { path: fpath })
    editingFile.value = { path: fpath, content: data.content, name: entry.name }
    editContent.value = data.content
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? t('server.files.readFailed'), 'error')
  }
}

async function saveFile() {
  if (!editingFile.value) return
  savingFile.value = true
  try {
    await api.put(`/servers/${serverId.value}/files/content`, { path: editingFile.value.path, content: editContent.value })
    showToast(t('server.files.saved'))
    editingFile.value.content = editContent.value
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? t('server.files.saveFailed'), 'error')
  } finally {
    savingFile.value = false
  }
}

async function deleteFileOrDir(entry: FileEntry) {
  const fpath = filePath.value === '/' ? '/' + entry.name : filePath.value + '/' + entry.name
  deletingPath.value = entry.name
  try {
    await api.delete(`/servers/${serverId.value}/files`, { path: fpath })
    showToast(t('server.files.deleted', { name: entry.name }))
    fetchFiles()
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? t('server.files.deleteFailed'), 'error')
  } finally {
    deletingPath.value = ''
  }
}

async function createFolder() {
  if (!newFolderName.value.trim()) return
  creatingFolder.value = true
  try {
    const fpath = filePath.value === '/' ? '/' + newFolderName.value.trim() : filePath.value + '/' + newFolderName.value.trim()
    await api.post(`/servers/${serverId.value}/files/directory`, { path: fpath })
    showToast(t('server.files.folderCreated'))
    newFolderName.value = ''
    showNewFolder.value = false
    fetchFiles()
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? t('server.files.folderFailed'), 'error')
  } finally {
    creatingFolder.value = false
  }
}

const uploadingFile = ref(false)
const extractingPath = ref('')
const archivingPath = ref('')
const autoExtractZip = ref(true)
const uploadProgress = ref<{ done: number; total: number; pct: number; name: string } | null>(null)

const fileInputRef = ref<HTMLInputElement | null>(null)
const folderInputRef = ref<HTMLInputElement | null>(null)

function triggerUpload() {
  fileInputRef.value?.click()
}

function triggerFolderUpload() {
  folderInputRef.value?.click()
}

// Upload a single file via XHR so we get upload progress events. The relative
// path (from webkitdirectory) is preserved in the multipart filename; the
// daemon recreates the directory tree underneath the current folder.
function uploadOne(file: File, relName: string, onProgress: (loaded: number) => void): Promise<void> {
  return new Promise((resolve, reject) => {
    const config = useRuntimeConfig()
    const fd = new FormData()
    fd.append('file', file, relName)
    const xhr = new XMLHttpRequest()
    xhr.open('POST', `${config.public.apiBase}/servers/${serverId.value}/files/upload?path=${encodeURIComponent(filePath.value)}`)
    if (authStore.accessToken) xhr.setRequestHeader('Authorization', `Bearer ${authStore.accessToken}`)
    xhr.withCredentials = true
    xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(e.loaded) }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve()
      } else {
        let msg = 'Upload failed'
        try { msg = JSON.parse(xhr.responseText)?.error ?? msg } catch { /* non-JSON */ }
        reject(new Error(msg))
      }
    }
    xhr.onerror = () => reject(new Error('Upload failed'))
    xhr.send(fd)
  })
}

async function handleFileUpload(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = '' // reset so re-selecting the same file/folder fires change
  if (files.length === 0) return
  await runUpload(files)
}

async function runUpload(files: File[]) {
  uploadingFile.value = true
  const totalBytes = files.reduce((s, f) => s + f.size, 0)
  const loadedPer = new Array(files.length).fill(0)
  let done = 0
  let failed = 0
  uploadProgress.value = { done: 0, total: files.length, pct: 0, name: files[0]?.name ?? '' }

  const recompute = (name?: string) => {
    const loaded = loadedPer.reduce((s: number, n: number) => s + n, 0)
    uploadProgress.value = {
      done,
      total: files.length,
      pct: totalBytes > 0 ? Math.round((loaded / totalBytes) * 100) : 0,
      name: name ?? uploadProgress.value?.name ?? '',
    }
  }

  let idx = 0
  const worker = async () => {
    while (idx < files.length) {
      const i = idx++
      const file = files[i]
      if (!file) continue
      const relName = (file as unknown as { webkitRelativePath?: string }).webkitRelativePath || file.name
      recompute(relName)
      try {
        await uploadOne(file, relName, (loaded) => { loadedPer[i] = loaded; recompute() })
        loadedPer[i] = file.size
      } catch {
        failed++
      } finally {
        done++
        recompute()
      }
    }
  }

  try {
    const CONCURRENCY = 3
    await Promise.all(Array.from({ length: Math.min(CONCURRENCY, files.length) }, worker))

    const single = files.length === 1 ? files[0] : undefined
    if (failed > 0) {
      showToast(t('server.files.uploadFailedSome', { failed, total: files.length }), 'error')
    } else {
      showToast(single ? t('server.files.uploadedOne', { name: single.name }) : t('server.files.uploadedMany', { n: files.length }))
    }

    // Auto-extract when a single .zip was uploaded and the option is enabled.
    if (failed === 0 && autoExtractZip.value && single && single.name.toLowerCase().endsWith('.zip')) {
      const name = single.name
      const fpath = filePath.value === '/' ? '/' + name : filePath.value + '/' + name
      try {
        await api.post(`/servers/${serverId.value}/files/extract`, { path: fpath })
        showToast(t('server.files.extracted', { name }))
      } catch (err: unknown) {
        const e = err as { data?: { error?: string } }
        showToast(e.data?.error ?? t('server.files.extractFailed'), 'error')
      }
    }

    fetchFiles()
  } finally {
    uploadingFile.value = false
    uploadProgress.value = null
  }
}

async function downloadFile(entry: FileEntry) {
  const fpath = filePath.value === '/' ? '/' + entry.name : filePath.value + '/' + entry.name
  try {
    const config = useRuntimeConfig()
    const resp = await $fetch.raw(`${config.public.apiBase}/servers/${serverId.value}/files/download?path=${encodeURIComponent(fpath)}`, {
      method: 'GET',
      headers: authStore.accessToken ? { Authorization: `Bearer ${authStore.accessToken}` } : {},
      credentials: 'include',
      responseType: 'blob',
    })
    const blob = resp._data as Blob
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = entry.name
    a.click()
    URL.revokeObjectURL(url)
  } catch {
    showToast(t('server.files.downloadFailed'), 'error')
  }
}

async function extractFile(entry: FileEntry) {
  const fpath = filePath.value === '/' ? '/' + entry.name : filePath.value + '/' + entry.name
  extractingPath.value = entry.name
  try {
    await api.post(`/servers/${serverId.value}/files/extract`, { path: fpath })
    showToast(t('server.files.extracted', { name: entry.name }))
    fetchFiles()
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? t('server.files.extractFailed'), 'error')
  } finally {
    extractingPath.value = ''
  }
}

async function archiveEntry(entry: FileEntry) {
  const fpath = filePath.value === '/' ? '/' + entry.name : filePath.value + '/' + entry.name
  archivingPath.value = entry.name
  try {
    await api.post(`/servers/${serverId.value}/files/archive`, { path: fpath })
    showToast(t('server.files.archived', { name: entry.name }))
    fetchFiles()
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? t('server.files.archiveFailed'), 'error')
  } finally {
    archivingPath.value = ''
  }
}

function formatFileSize(bytes: number): string {
  if (bytes === 0) return '\u2014'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`
}

watch(activeTab, (tab) => {
  if (tab === 'files') {
    fetchFiles('/')
  }
})

// Placeholder active mods for sidebar
const sidebarMods = computed(() => {
  if (!Array.isArray(modsStore.installed) || modsStore.installed.length === 0) {
    return [
      { name: t('server.console.noModsInstalled'), version: '' },
    ]
  }
  return modsStore.installed.slice(0, 5).map(m => ({
    name: m.display_name || m.filename,
    version: m.version || '',
  }))
})
</script>

<template>
  <div class="p-6 space-y-5">
    <NuxtLink to="/servers"
      class="inline-flex items-center gap-1.5 text-tp-muted hover:text-tp-text text-sm transition-colors">
      <ArrowLeft class="w-4 h-4" />
      {{ t('server.page.backToServers') }}
    </NuxtLink>

    <!-- Loading -->
    <div v-if="serversStore.loading && !server" class="space-y-4">
      <div class="bg-tp-surface rounded-xl p-6 animate-pulse">
        <div class="h-7 bg-tp-surface2 rounded w-48 mb-3" />
        <div class="h-4 bg-tp-surface2 rounded w-32" />
      </div>
    </div>

    <!-- Not found -->
    <div v-else-if="!server && !serversStore.loading"
      class="bg-tp-surface rounded-xl p-12 text-center">
      <p class="text-tp-muted">{{ t('server.page.notFound') }}</p>
      <NuxtLink to="/servers">
        <UiButton variant="secondary" size="sm" class="mt-4">{{ t('server.page.goBack') }}</UiButton>
      </NuxtLink>
    </div>

    <template v-else-if="server">
      <!-- Header card -->
      <div class="bg-tp-surface rounded-xl p-5">
        <div class="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-4">
          <div>
            <div class="flex items-center gap-3 mb-1">
              <h2 class="text-tp-text font-display font-bold text-3xl">{{ server.name }}</h2>
              <ServerStatusBadge :status="server.status" />
            </div>
            <p class="text-tp-outline text-sm font-mono">{{ displayId }} &middot; {{ t('server.page.port', { port: server.port }) }} &middot; {{ formatHytaleVersion(server.hytale_version) }}<template v-if="server.installed_build"> &middot; {{ t('server.page.build', { build: server.installed_build }) }}</template></p>
          </div>
          <div class="flex items-center gap-2 flex-wrap">
            <UiButton variant="secondary" size="sm" :disabled="!canStart || actionLoading.start"
              :loading="actionLoading.start" @click="runAction('start')">
              <Play class="w-3.5 h-3.5" /> {{ t('actions.start') }}
            </UiButton>
            <UiButton variant="secondary" size="sm" :disabled="!canStop || actionLoading.stop"
              :loading="actionLoading.stop" @click="runAction('stop')">
              <Square class="w-3.5 h-3.5" /> {{ t('actions.stop') }}
            </UiButton>
            <UiButton variant="secondary" size="sm" :disabled="!canRestart || actionLoading.restart"
              :loading="actionLoading.restart" @click="runAction('restart')">
              <RotateCcw class="w-3.5 h-3.5" /> {{ t('actions.restart') }}
            </UiButton>
            <UiButton variant="danger" size="sm" :disabled="!canKill || actionLoading.kill"
              :loading="actionLoading.kill" @click="runAction('kill')">
              <Zap class="w-3.5 h-3.5" /> {{ t('actions.kill') }}
            </UiButton>
            <button
              v-if="server?.status === 'stopped' && isAdmin"
              @click="openMigrateModal"
              class="flex items-center gap-2 px-3 py-2 rounded-xl text-sm font-medium bg-tp-surface3 text-tp-text hover:bg-tp-surface-highest transition-colors"
            >
              <span class="material-symbols-outlined text-base">swap_horiz</span>
              {{ t('server.page.migrate') }}
            </button>
          </div>
        </div>
      </div>

      <!-- Reinstall notice: Hytale exit codes 8 / 11 -->
      <div v-if="reinstallExitCode !== null"
        class="bg-tp-error/10 rounded-xl p-4 flex flex-col sm:flex-row sm:items-center gap-4" role="alert">
        <span class="material-symbols-outlined text-tp-error text-2xl shrink-0">system_update</span>
        <div class="flex-1 min-w-0">
          <p class="text-tp-text text-sm font-semibold">
            {{ t('server.reinstall.title', { code: reinstallExitCode }) }}
          </p>
          <p v-if="reinstallExitCode === 8" class="text-tp-muted text-xs mt-0.5">
            {{ t('server.reinstall.code8') }}
          </p>
          <i18n-t v-else keypath="server.reinstall.code11" tag="p" scope="global" class="text-tp-muted text-xs mt-0.5">
            <template #flag><span class="font-mono">--allow-install-dir</span></template>
          </i18n-t>
          <p class="text-tp-muted text-xs mt-0.5">{{ t('server.reinstall.binariesOnly') }}</p>
        </div>
        <UiButton variant="primary" size="sm" class="shrink-0"
          :disabled="!canStart || reinstalling" :loading="reinstalling || server.status === 'installing'"
          @click="reinstallServer">
          <RotateCcw class="w-3.5 h-3.5" /> {{ server.status === 'installing' ? t('server.reinstall.reinstalling') : t('server.reinstall.reinstall') }}
        </UiButton>
      </div>

      <!-- Hytale update: installed build vs. newest of the channel -->
      <div v-if="updateInfo && (updateInfo.available || updateInfo.running || updateInfo.blocked)"
        class="rounded-xl p-4 flex flex-col sm:flex-row sm:items-center gap-4"
        :class="updateInfo.blocked ? 'bg-tp-surface2' : 'bg-tp-primary/10'" role="status">
        <span class="material-symbols-outlined text-2xl shrink-0"
          :class="updateInfo.blocked ? 'text-tp-muted' : 'text-tp-primary'">system_update_alt</span>
        <div class="flex-1 min-w-0">
          <p class="text-tp-text text-sm font-semibold">
            <template v-if="updateInfo.running">{{ t('server.update.updating', { version: updateInfo.latest }) }}</template>
            <template v-else-if="updateInfo.blocked">{{ t('server.update.blocked') }}</template>
            <template v-else>{{ t('server.update.available', { version: updateInfo.latest }) }}</template>
          </p>
          <p class="text-tp-muted text-xs mt-0.5">
            {{ t('server.update.channelInfo', { channel: patchlineLabel, installed: updateInfo.installed ?? t('server.update.unknown') }) }}
            <template v-if="updateInfo.running"> · {{ t('server.update.followProgress') }}</template>
          </p>
          <p v-if="updateInfo.blocked" class="text-tp-muted text-xs mt-0.5">{{ updateInfo.reason }}</p>
          <p v-else-if="!updateInfo.running" class="text-tp-muted text-xs mt-0.5">
            {{ t('server.update.backupFirst') }}
          </p>
        </div>
        <div v-if="!updateInfo.blocked && !updateInfo.running" class="flex items-center gap-2 shrink-0">
          <label v-if="server.status === 'running'" class="text-tp-muted text-xs flex items-center gap-1.5"
            :title="t('server.update.warnTitle')">
            {{ t('server.update.warn') }}
            <input v-model.number="updateWarnMinutes" type="number" min="0" max="15"
              class="w-14 bg-tp-surface2 text-tp-text rounded-lg px-2 py-1 text-xs focus:outline-none focus:ring-2 focus:ring-tp-primary/50" />
            {{ t('server.update.minutes') }}
          </label>
          <UiButton variant="primary" size="sm" :loading="updateStarting"
            :disabled="updateStarting || !['running', 'stopped', 'crashed'].includes(server.status)"
            @click="startUpdate">
            <Download class="w-3.5 h-3.5" /> {{ t('server.update.updateNow') }}
          </UiButton>
        </div>
      </div>

      <!-- Stat cards row -->
      <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <div class="bg-tp-surface2 rounded-xl p-4">
          <div class="flex items-center justify-between mb-2">
            <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('server.stats.cpuUsage') }}</span>
            <Info class="w-3.5 h-3.5 text-tp-outline" />
          </div>
          <p class="text-tp-text font-display font-bold text-2xl">
            {{ metrics ? `${metrics.cpu.usage_percent.toFixed(1)}%` : '\u2014' }}
          </p>
          <p class="text-tp-muted text-xs mt-1">
            {{ server.cpu_limit ? t('server.stats.cpuLimit', { limit: server.cpu_limit }) : t('server.stats.noLimitSet') }}
          </p>
        </div>

        <div class="bg-tp-surface2 rounded-xl p-4">
          <div class="flex items-center justify-between mb-2">
            <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('server.stats.memory') }}</span>
            <Info class="w-3.5 h-3.5 text-tp-outline" />
          </div>
          <p class="text-tp-text font-display font-bold text-2xl">
            {{ metrics ? `${metrics.memory.used_mb} MB` : '\u2014' }}
          </p>
          <p class="text-tp-muted text-xs mt-1">
            {{ metrics && metrics.memory.limit_mb > 0 ? t('server.stats.allocated', { mb: metrics.memory.limit_mb }) : (server.ram_limit_mb ? t('server.stats.allocated', { mb: server.ram_limit_mb }) : t('server.stats.noLimit')) }}
          </p>
        </div>

        <div class="bg-tp-surface2 rounded-xl p-4">
          <div class="flex items-center justify-between mb-2">
            <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('server.stats.players') }}</span>
            <Info class="w-3.5 h-3.5 text-tp-outline" />
          </div>
          <p class="text-tp-text font-display font-bold text-2xl">
            {{ onlinePlayers ?? '—' }}
          </p>
          <p class="text-tp-muted text-xs mt-1">{{ t('server.stats.playersOnline') }}</p>
        </div>

        <div class="bg-tp-surface2 rounded-xl p-4">
          <div class="flex items-center justify-between mb-2">
            <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('server.stats.uptime') }}</span>
            <Info class="w-3.5 h-3.5 text-tp-outline" />
          </div>
          <p class="text-tp-text font-display font-bold text-2xl">
            {{ metrics ? formatUptime(metrics.uptime_s) : '\u2014' }}
          </p>
          <p class="text-tp-muted text-xs mt-1">
            {{ metrics?.note ?? t('server.stats.liveFromDaemon') }}
          </p>
        </div>
      </div>

      <!-- Tabs -->
      <div class="border-b border-tp-border">
        <nav class="flex gap-1 overflow-x-auto scrollbar-none">
          <button
            v-for="tab in tabs" :key="tab.id"
            :class="[
              'flex items-center gap-2 px-4 py-2.5 text-sm font-medium whitespace-nowrap border-b-2 transition-colors',
              activeTab === tab.id
                ? 'border-tp-primary text-tp-primary'
                : 'border-transparent text-tp-muted hover:text-tp-text hover:border-tp-border',
            ]"
            @click="activeTab = tab.id"
          >
            <component :is="tab.icon" class="w-4 h-4" />
            {{ t(tab.label) }}
          </button>
        </nav>
      </div>

      <!-- ── Overview Tab ─────────────────────────────────────────────────── -->
      <div v-if="activeTab === 'overview'" class="space-y-5">
        <!-- RAM progress bar -->
        <div v-if="metrics && metrics.memory.limit_mb > 0"
          class="bg-tp-surface rounded-xl p-4">
          <div class="flex items-center justify-between mb-2">
            <span class="text-tp-text text-sm font-medium">{{ t('server.overview.memoryUsage') }}</span>
            <span class="text-tp-outline text-xs">
              {{ metrics.memory.used_mb }} / {{ metrics.memory.limit_mb }} MB
            </span>
          </div>
          <div class="h-2 bg-tp-surface-highest rounded-full overflow-hidden">
            <div
              class="h-full rounded-full transition-all duration-500 progress-fill"
              :class="(metrics.memory.used_mb / metrics.memory.limit_mb) > 0.85 ? 'bg-tp-danger !bg-none' : ''"
              :style="{ width: `${Math.min(100, (metrics.memory.used_mb / metrics.memory.limit_mb) * 100).toFixed(1)}%` }"
            />
          </div>
        </div>

        <ServerPerformanceCharts :server-id="serverId" />

        <!-- Server info table -->
        <div class="bg-tp-surface rounded-xl overflow-hidden">
          <div class="px-5 py-3 border-b border-tp-border">
            <h3 class="text-tp-text font-display font-semibold text-sm">{{ t('server.overview.serverInfo') }}</h3>
          </div>
          <div class="divide-y divide-tp-border">
            <div v-for="row in [
              { label: t('server.overview.nodeId'), value: server.node_id, mono: true },
              { label: t('server.overview.port'), value: server.port, mono: true },
              { label: t('server.overview.version'), value: server.hytale_version, mono: false },
              { label: t('server.overview.autoRestart'), value: server.auto_restart ? t('server.shared.enabled') : t('server.shared.disabled'), mono: false },
              { label: t('server.overview.activeWorld'), value: server.active_world || '\u2014', mono: false },
              { label: t('server.overview.cpuLimit'), value: server.cpu_limit ? `${server.cpu_limit}%` : t('server.shared.unlimited'), mono: false },
              { label: t('server.overview.ramLimit'), value: server.ram_limit_mb ? `${server.ram_limit_mb} MB` : t('server.shared.unlimited'), mono: false },
              { label: t('server.overview.created'), value: new Date(server.created_at).toLocaleString(), mono: false },
            ]" :key="row.label" class="flex items-center px-5 py-3">
              <span class="text-tp-outline text-sm w-40 shrink-0">{{ row.label }}</span>
              <span :class="['text-tp-text text-sm truncate', row.mono ? 'font-mono text-xs' : '']">{{ row.value }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- ── Console Tab ───────────────────────────────────────────────────── -->
      <div v-else-if="activeTab === 'console'" class="flex gap-5">
        <!-- Main console area -->
        <div class="flex-1 space-y-3 min-w-0">
          <!-- Terminal header -->
          <div class="flex items-center gap-2 px-1">
            <span class="w-2 h-2 rounded-full bg-tp-tertiary animate-pulse" />
            <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('server.console.terminalOutput') }}</span>
            <span
              v-if="streamLive"
              class="ml-auto inline-flex items-center gap-1.5 rounded-full bg-tp-tertiary/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-widest text-tp-tertiary"
              :title="t('server.console.liveTitle')"
            >
              <span class="w-1.5 h-1.5 rounded-full bg-tp-tertiary" /> {{ t('server.console.live') }}
            </span>
            <button
              v-else-if="streamPolling"
              type="button"
              class="ml-auto inline-flex items-center gap-1.5 rounded-full bg-tp-warning/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-widest text-tp-warning hover:bg-tp-warning/20 transition-colors"
              :title="t('server.console.pollingTitle')"
              @click="stream.restart()"
            >
              <span class="w-1.5 h-1.5 rounded-full bg-tp-warning" /> {{ t('server.console.polling') }}
            </button>
            <span
              v-else
              class="ml-auto inline-flex items-center gap-1.5 rounded-full bg-tp-surface2 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-widest text-tp-outline"
            >
              <Loader2 class="w-3 h-3 animate-spin" /> {{ t('server.console.connecting') }}
            </span>
          </div>

          <!-- Log viewer -->
          <div
            ref="consoleEl"
            class="bg-tp-surface-lowest rounded-xl h-[420px] overflow-y-auto p-4 font-mono text-xs space-y-0.5"
            @scroll="onConsoleScroll"
          >
            <div v-if="logs.length === 0" class="text-tp-muted py-4 text-center">
              {{ t('server.console.noLogs') }}
            </div>
            <div v-for="line in logs" :key="line.id" class="flex gap-2 leading-5">
              <span class="text-tp-outline shrink-0">{{ formatLogTime(line.logged_at) }}</span>
              <span :class="['shrink-0 w-12', logLevelColor(line.level)]">
                {{ line.level?.toUpperCase().slice(0, 6) }}
              </span>
              <!-- eslint-disable-next-line vue/no-v-html -- renderLogMessage HTML-escapes the line first and only adds http(s) links without quotes -->
              <span :class="logLevelColor(line.level)" class="break-all" v-html="renderLogMessage(line.message)" />
            </div>
          </div>

          <!-- Command input -->
          <div class="flex gap-2">
            <div class="relative flex-1">
              <ChevronRight class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-tp-muted pointer-events-none" />
              <input
                v-model="consoleInput"
                type="text"
                :placeholder="t('server.console.placeholder')"
                :disabled="server.status !== 'running'"
                class="w-full bg-tp-surface2 text-tp-text rounded-xl pl-9 pr-4 py-2 text-sm font-mono placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50 transition-colors disabled:opacity-50"
                @keydown="handleConsoleKey"
              />
            </div>
            <UiButton
              variant="primary"
              size="sm"
              :loading="sendingCmd"
              :disabled="!consoleInput.trim() || server.status !== 'running'"
              @click="sendCommand"
            >
              <Send class="w-3.5 h-3.5" />
              {{ t('server.console.send') }}
            </UiButton>
            <UiButton
              variant="secondary"
              size="sm"
              :disabled="server.status !== 'running'"
              :title="t('server.console.hytaleLoginTitle')"
              @click="triggerHytaleAuth"
            >
              {{ t('server.console.hytaleLogin') }}
            </UiButton>
          </div>
          <p v-if="server.status !== 'running'" class="text-tp-muted text-xs">
            {{ t('server.console.mustBeRunning') }}
          </p>
        </div>

        <!-- Right sidebar -->
        <div class="w-[300px] shrink-0 space-y-4 hidden xl:block">
          <!-- Quick Connect -->
          <div class="bg-tp-surface2 rounded-xl p-4 space-y-4">
            <h4 class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('server.console.quickConnect') }}</h4>

            <!-- Server Address -->
            <div>
              <label class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline block mb-1.5">{{ t('server.console.serverAddress') }}</label>
              <div class="flex items-center gap-2 bg-tp-surface-lowest rounded-lg px-3 py-2">
                <span class="text-tp-tertiary text-xs font-mono truncate flex-1">{{ serverAddress }}</span>
                <button
                  class="text-tp-outline hover:text-tp-text transition-colors shrink-0"
                  :title="t('server.console.copyAddress')"
                  :aria-label="t('server.console.copyAddress')"
                  @click="copyToClipboard(serverAddress, 'address')"
                >
                  <Copy v-if="copiedField !== 'address'" class="w-3.5 h-3.5" />
                  <span v-else class="text-tp-tertiary text-[10px] font-semibold">{{ t('server.console.ok') }}</span>
                </button>
              </div>
            </div>

          </div>

          <!-- Active Mods -->
          <div class="bg-tp-surface2 rounded-xl p-4">
            <h4 class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-3">{{ t('server.console.activeMods') }}</h4>
            <div class="space-y-2">
              <div v-for="(mod, i) in sidebarMods" :key="i"
                class="flex items-center gap-2.5 py-1.5">
                <div class="w-7 h-7 rounded-lg bg-tp-surface-lowest flex items-center justify-center shrink-0">
                  <Package class="w-3.5 h-3.5 text-tp-muted" />
                </div>
                <div class="min-w-0">
                  <p class="text-tp-text text-xs font-medium truncate">{{ mod.name }}</p>
                  <p v-if="mod.version" class="text-tp-outline text-[10px] truncate">{{ mod.version }}</p>
                </div>
              </div>
            </div>
            <button
              class="mt-3 text-tp-tertiary text-xs hover:text-tp-text transition-colors"
              @click="activeTab = 'mods'"
            >
              {{ t('server.console.manageMods') }}
            </button>
          </div>
        </div>
      </div>

      <!-- ── Mods Tab ──────────────────────────────────────────────────────── -->
      <div v-else-if="activeTab === 'mods'" class="space-y-4">
        <!-- Sub-tabs -->
        <div class="flex gap-1 bg-tp-surface2 rounded-xl p-1 w-fit">
          <button
            v-for="sub in [{ id: 'installed', label: t('server.mods.installed') }, { id: 'browse', label: 'CurseForge' }, { id: 'modtale', label: 'Modtale' }]"
            :key="sub.id"
            :class="[
              'px-4 py-1.5 rounded-lg text-sm font-medium transition-colors',
              modsTab === sub.id ? 'bg-tp-surface text-tp-text shadow-sm' : 'text-tp-muted hover:text-tp-text',
            ]"
            @click="modsTab = (sub.id as 'installed' | 'browse' | 'modtale')"
          >
            {{ sub.label }}
          </button>
        </div>

        <!-- Installed mods -->
        <div v-if="modsTab === 'installed'">
          <div v-if="modsStore.loadingInstalled" class="space-y-2">
            <div v-for="i in 3" :key="i" class="h-14 bg-tp-surface2 rounded-xl animate-pulse" />
          </div>
          <div v-else-if="!Array.isArray(modsStore.installed) || modsStore.installed.length === 0"
            class="bg-tp-surface rounded-xl p-12 text-center">
            <Package class="w-8 h-8 text-tp-muted mx-auto mb-3" />
            <p class="text-tp-text font-semibold text-sm mb-1">{{ t('server.mods.emptyTitle') }}</p>
            <p class="text-tp-muted text-xs">{{ t('server.mods.emptyHint') }}</p>
          </div>
          <div v-else class="bg-tp-surface rounded-xl overflow-hidden">
            <div class="divide-y divide-tp-border">
              <div v-for="mod in modsStore.installed" :key="mod.id"
                class="flex items-center gap-4 px-5 py-3">
                <div class="w-8 h-8 rounded-xl bg-tp-surface2 flex items-center justify-center shrink-0">
                  <Package class="w-4 h-4 text-tp-muted" />
                </div>
                <div class="flex-1 min-w-0">
                  <p class="text-tp-text text-sm font-medium truncate">{{ mod.display_name }}</p>
                  <p class="text-tp-muted text-xs">{{ mod.version }}<template v-if="mod.source === 'modtale'"> &middot; Modtale</template><template v-else-if="mod.source === 'curseforge' || mod.cf_mod_id"> &middot; CurseForge</template></p>
                </div>
                <UiButton
                  variant="danger"
                  size="sm"
                  :loading="modsStore.removingFilename === mod.filename"
                  :title="t('server.mods.remove')"
                  :aria-label="t('server.mods.remove')"
                  @click="removeMod(mod.filename, mod.display_name, mod.mod_dir || 'mods')"
                >
                  <Trash2 class="w-3.5 h-3.5" />
                </UiButton>
              </div>
            </div>
          </div>
        </div>

        <!-- Browse Modtale -->
        <ModsModtaleBrowser
          v-else-if="modsTab === 'modtale'"
          :server-id="serverId"
          @toast="(msg, type) => showToast(msg, type)"
        />

        <!-- Browse CurseForge -->
        <div v-else class="space-y-3">
          <div v-if="modsStore.error && !modsStore.loadingSearch"
            class="flex items-start gap-3 bg-tp-error/10 text-tp-error rounded-xl px-4 py-3 text-sm">
            <span class="font-medium shrink-0">{{ t('server.mods.cfUnavailable') }}</span>
            <span class="opacity-80">{{ modsStore.error }}</span>
          </div>
          <div class="relative">
            <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-tp-muted pointer-events-none" />
            <input
              v-model="modSearch"
              type="text"
              :placeholder="t('server.mods.searchPlaceholder')"
              class="w-full bg-tp-surface2 text-tp-text rounded-xl pl-10 pr-4 py-2.5 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50 transition-colors"
              @input="onModSearchInput"
            />
          </div>

          <div v-if="modsStore.loadingSearch" class="space-y-2">
            <div v-for="i in 6" :key="i" class="h-20 bg-tp-surface2 rounded-xl animate-pulse" />
          </div>
          <div v-else-if="safeSearchResults.length === 0 && modSearch.trim()"
            class="bg-tp-surface rounded-xl p-8 text-center">
            <p class="text-tp-muted text-sm">{{ t('server.mods.noResults', { query: modSearch }) }}</p>
          </div>
          <div v-else class="bg-tp-surface rounded-xl overflow-hidden">
            <div class="px-5 py-2.5 border-b border-tp-border">
              <p class="text-tp-outline text-xs">{{ modSearch.trim() ? t('server.mods.results', safeSearchResults.length) : t('server.mods.popular') }}</p>
            </div>
            <div class="divide-y divide-tp-border">
              <div v-for="mod in safeSearchResults" :key="mod.id"
                class="flex items-start gap-4 px-5 py-4">
                <img
                  v-if="mod.logo"
                  :src="mod.logo.thumbnailUrl"
                  :alt="mod.name"
                  class="w-12 h-12 rounded-xl object-cover shrink-0 bg-tp-surface2"
                />
                <div v-else
                  class="w-12 h-12 rounded-xl bg-tp-surface2 flex items-center justify-center shrink-0">
                  <Package class="w-6 h-6 text-tp-muted" />
                </div>
                <div class="flex-1 min-w-0">
                  <div class="flex items-start justify-between gap-2">
                    <p class="text-tp-text text-sm font-semibold truncate">{{ mod.name }}</p>
                    <a
                      :href="httpsHref(mod.links.websiteUrl)"
                      target="_blank"
                      rel="noopener noreferrer"
                      :title="t('server.mods.openWebsite')"
                      :aria-label="t('server.mods.openWebsite')"
                      class="text-tp-muted hover:text-tp-text transition-colors shrink-0"
                    >
                      <ExternalLink class="w-3.5 h-3.5" />
                    </a>
                  </div>
                  <p class="text-tp-muted text-xs mt-0.5 line-clamp-2">{{ mod.summary }}</p>
                  <p class="text-tp-outline text-xs mt-1">{{ t('server.mods.downloads', { n: mod.downloadCount.toLocaleString() }) }}</p>
                </div>
                <UiButton
                  v-if="!isInstalled(mod.id)"
                  variant="primary"
                  size="sm"
                  :disabled="!mod.latestFiles.length"
                  :loading="modsStore.installingId === mod.id"
                  @click="installMod(mod)"
                >
                  <Download class="w-3.5 h-3.5" />
                  {{ t('server.mods.install') }}
                </UiButton>
                <span v-else class="text-tp-success text-xs font-medium shrink-0 mt-1">{{ t('server.mods.installedBadge') }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- ── Game Control Tab ─────────────────────────────────────────────── -->
      <div v-else-if="activeTab === 'game-control'" class="space-y-4">
        <!-- Header -->
        <div class="flex items-center justify-between">
          <div>
            <h3 class="text-tp-text font-display font-semibold text-lg">{{ t('server.gameControl.title') }}</h3>
            <p class="text-tp-muted text-sm">{{ t('server.gameControl.subtitle') }}</p>
          </div>
          <UiButton variant="secondary" size="sm" @click="showAddCommand = !showAddCommand">
            <Plus class="w-3.5 h-3.5" />
            {{ t('server.gameControl.customCommand') }}
          </UiButton>
        </div>

        <!-- Server must be running notice -->
        <div v-if="server.status !== 'running'"
          class="flex items-center gap-3 bg-tp-warning/5 border border-tp-warning/20 text-tp-warning rounded-xl px-4 py-3 text-sm">
          {{ t('server.gameControl.mustBeRunning') }}
        </div>

        <!-- Add custom command form -->
        <div v-if="showAddCommand" class="bg-tp-surface rounded-xl p-5 space-y-3">
          <h4 class="text-tp-text font-display font-semibold text-sm">{{ t('server.gameControl.addTitle') }}</h4>
          <div class="grid grid-cols-2 gap-3">
            <input v-model="newCommand.name" :placeholder="t('server.gameControl.namePlaceholder')"
              class="bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50" />
            <input v-model="newCommand.category" :placeholder="t('server.gameControl.categoryPlaceholder')"
              class="bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50" />
          </div>
          <input v-model="newCommand.command_template" :placeholder="t('server.gameControl.templatePlaceholder', { example: 'give {player} {item}' })"
            class="w-full bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm font-mono placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50" />
          <input v-model="newCommand.description" :placeholder="t('server.gameControl.descriptionPlaceholder')"
            class="w-full bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50" />
          <textarea v-model="newCommand.params" :placeholder="t('server.gameControl.paramsPlaceholder', { example: PARAMS_EXAMPLE })"
            class="w-full bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm font-mono placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50 h-16 resize-none" />
          <div class="flex items-center gap-3">
            <label class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('server.gameControl.minRole') }}</label>
            <select v-model="newCommand.min_role"
              class="bg-tp-surface2 text-tp-text rounded-xl px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-tp-primary/50">
              <option value="user">{{ t('server.gameControl.roles.user') }}</option>
              <option value="moderator">{{ t('server.gameControl.roles.moderator') }}</option>
              <option value="admin">{{ t('server.gameControl.roles.admin') }}</option>
              <option value="owner">{{ t('server.gameControl.roles.owner') }}</option>
            </select>
          </div>
          <div class="flex gap-2">
            <UiButton variant="primary" size="sm" @click="addCustomCommand">{{ t('server.gameControl.addCommand') }}</UiButton>
            <UiButton variant="secondary" size="sm" @click="showAddCommand = false">{{ t('actions.cancel') }}</UiButton>
          </div>
        </div>

        <!-- Loading -->
        <div v-if="gameCmdStore.loading" class="space-y-3">
          <div v-for="i in 3" :key="i" class="h-24 bg-tp-surface2 rounded-xl animate-pulse" />
        </div>

        <!-- Category sub-tabs + command list -->
        <template v-else>
          <!-- Category tabs -->
          <div v-if="gameCmdStore.categories.length > 0" class="flex gap-1 bg-tp-surface2 rounded-xl p-1 overflow-x-auto">
            <button
              v-for="cat in gameCmdStore.categories" :key="cat"
              :class="[
                'px-4 py-1.5 rounded-lg text-sm font-medium transition-colors whitespace-nowrap',
                activeCmdCategory === cat
                  ? 'bg-tp-surface text-tp-text shadow-sm'
                  : 'text-tp-muted hover:text-tp-text',
              ]"
              @click="activeCmdCategory = cat"
            >
              {{ cat }}
              <span class="ml-1.5 text-xs opacity-60">{{ gameCmdStore.grouped[cat]?.length }}</span>
            </button>
          </div>

          <!-- Commands for active category -->
          <div v-if="activeCmdCategory && activeCategoryCommands.length > 0"
            class="bg-tp-surface rounded-xl overflow-hidden">
            <div class="divide-y divide-tp-border">
              <div v-for="cmd in activeCategoryCommands" :key="cmd.id"
                class="px-5 py-4"
                :class="{ 'opacity-50': !userCanExecute(cmd) }">
                <div class="flex items-start justify-between gap-4">
                  <div class="flex-1 min-w-0">
                    <div class="flex items-center gap-2 mb-1">
                      <p class="text-tp-text text-sm font-medium">{{ cmd.name }}</p>
                      <code class="text-[10px] bg-tp-surface2 text-tp-muted px-1.5 py-0.5 rounded font-mono">{{ cmd.command_template }}</code>
                      <span
                        :class="['text-[10px] px-1.5 py-0.5 rounded font-medium capitalize', roleBadgeColor(cmd.min_role)]"
                      >{{ t(`roles.${cmd.min_role}`) }}</span>
                    </div>
                    <p v-if="cmd.description" class="text-tp-muted text-xs">{{ cmd.description }}</p>

                    <!-- Insufficient permission notice -->
                    <p v-if="!userCanExecute(cmd)" class="text-tp-error text-xs mt-1.5">
                      {{ t('server.gameControl.requiresRole', { role: t(`roles.${cmd.min_role}`) }) }}
                    </p>

                    <!-- Parameter inputs -->
                    <div v-if="userCanExecute(cmd) && Array.isArray(cmd.params) && cmd.params.length > 0"
                      class="flex flex-wrap gap-2 mt-2.5">
                      <input
                        v-for="param in cmd.params"
                        :key="param.name"
                        v-model="getParamValues(cmd.id)[param.name]"
                        :placeholder="param.placeholder || param.name"
                        :required="param.required"
                        :type="param.type === 'number' ? 'number' : 'text'"
                        class="bg-tp-surface2 text-tp-text rounded-lg px-2.5 py-1.5 text-xs placeholder:text-tp-muted focus:outline-none focus:ring-1 focus:ring-tp-primary/50 w-32"
                      />
                    </div>
                  </div>

                  <div class="flex items-center gap-1.5 shrink-0">
                    <UiButton
                      variant="primary"
                      size="sm"
                      :disabled="server.status !== 'running' || !hasRequiredParams(cmd) || !userCanExecute(cmd)"
                      :loading="gameCmdStore.executing === cmd.id"
                      @click="executeCmd(cmd)"
                    >
                      <Play class="w-3 h-3" />
                      {{ t('server.gameControl.run') }}
                    </UiButton>
                    <UiButton
                      v-if="!cmd.is_default"
                      variant="danger"
                      size="sm"
                      :title="t('server.gameControl.deleteCommand')"
                      :aria-label="t('server.gameControl.deleteCommand')"
                      @click="deleteCmd(cmd.id)"
                    >
                      <Trash2 class="w-3 h-3" />
                    </UiButton>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- Empty state -->
          <div v-if="gameCmdStore.categories.length === 0 && !gameCmdStore.loading"
            class="bg-tp-surface rounded-xl p-12 text-center">
            <Gamepad2 class="w-8 h-8 text-tp-muted mx-auto mb-3" />
            <p class="text-tp-text font-semibold text-sm mb-1">{{ t('server.gameControl.emptyTitle') }}</p>
            <p class="text-tp-muted text-xs">{{ t('server.gameControl.emptyHint') }}</p>
          </div>
        </template>

        <!-- Last executed feedback -->
        <div v-if="gameCmdStore.lastResult"
          class="flex items-center gap-2 bg-tp-success/10 text-tp-success rounded-xl px-4 py-2.5 text-sm font-mono">
          <ChevronRight class="w-3.5 h-3.5 shrink-0" />
          {{ gameCmdStore.lastResult.command }}
        </div>
      </div>

      <!-- ── Files Tab ────────────────────────────────────────────────────── -->
      <div v-else-if="activeTab === 'files'" class="space-y-3">
        <!-- File editor mode -->
        <template v-if="editingFile">
          <div class="bg-tp-surface rounded-xl overflow-hidden">
            <div class="flex items-center justify-between px-4 py-2.5 border-b border-tp-border">
              <div class="flex items-center gap-2 text-sm">
                <FileText class="w-4 h-4 text-tp-muted" />
                <span class="text-tp-text font-medium">{{ editingFile.name }}</span>
                <span class="text-tp-outline text-xs">{{ editingFile.path }}</span>
              </div>
              <div class="flex items-center gap-2">
                <UiButton variant="primary" size="sm" :loading="savingFile" @click="saveFile">
                  <Save class="w-3.5 h-3.5" /> {{ t('actions.save') }}
                </UiButton>
                <UiButton variant="secondary" size="sm" @click="editingFile = null">
                  <X class="w-3.5 h-3.5" /> {{ t('actions.close') }}
                </UiButton>
              </div>
            </div>
            <textarea
              v-model="editContent"
              class="w-full bg-tp-surface-lowest text-tp-tertiary font-mono text-xs p-4 focus:outline-none resize-none"
              style="min-height: 500px; tab-size: 2;"
              spellcheck="false"
            />
          </div>
        </template>

        <!-- File browser mode -->
        <template v-else>
          <!-- Toolbar -->
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-1 text-sm overflow-x-auto">
              <button
                v-for="(crumb, i) in breadcrumbs" :key="crumb.path"
                class="flex items-center gap-1 text-tp-muted hover:text-tp-text transition-colors whitespace-nowrap"
                @click="fetchFiles(crumb.path)"
              >
                <span v-if="i > 0" class="text-tp-border mx-0.5">/</span>
                <FolderOpen v-if="i === 0" class="w-3.5 h-3.5" />
                <span :class="i === breadcrumbs.length - 1 ? 'text-tp-text font-medium' : ''">{{ crumb.label }}</span>
              </button>
            </div>
            <div class="flex items-center gap-2">
              <UiButton v-if="filePath !== '/'" variant="secondary" size="sm" @click="fetchFiles(filePath.split('/').slice(0, -1).join('/') || '/')">
                <ArrowUpLeft class="w-3.5 h-3.5" /> {{ t('server.files.up') }}
              </UiButton>
              <UiButton variant="secondary" size="sm" :loading="uploadingFile" @click="triggerUpload">
                <Upload class="w-3.5 h-3.5" /> {{ t('actions.upload') }}
              </UiButton>
              <input ref="fileInputRef" type="file" multiple class="hidden" @change="handleFileUpload" />
              <UiButton variant="secondary" size="sm" :loading="uploadingFile" @click="triggerFolderUpload">
                <Upload class="w-3.5 h-3.5" /> {{ t('server.files.folder') }}
              </UiButton>
              <input ref="folderInputRef" type="file" webkitdirectory class="hidden" @change="handleFileUpload" />
              <label class="flex items-center gap-1.5 text-xs text-tp-muted cursor-pointer select-none">
                <input v-model="autoExtractZip" type="checkbox" class="accent-tp-primary" />
                {{ t('server.files.autoExtract') }}
              </label>
              <UiButton variant="secondary" size="sm" @click="showNewFolder = !showNewFolder">
                <FolderPlus class="w-3.5 h-3.5" /> {{ t('server.files.newFolder') }}
              </UiButton>
              <UiButton variant="secondary" size="sm" :title="t('actions.refresh')" :aria-label="t('actions.refresh')" @click="fetchFiles()">
                <RotateCcw class="w-3.5 h-3.5" />
              </UiButton>
            </div>
          </div>

          <!-- Upload progress -->
          <div v-if="uploadProgress" class="space-y-1">
            <div class="flex items-center justify-between text-xs text-tp-muted gap-2">
              <span class="truncate">{{ t('server.files.uploading', { done: uploadProgress.done, total: uploadProgress.total, name: uploadProgress.name }) }}</span>
              <span class="shrink-0">{{ uploadProgress.pct }}%</span>
            </div>
            <div class="h-1.5 bg-tp-surface2 rounded-full overflow-hidden">
              <div class="h-full bg-tp-primary transition-all" :style="{ width: uploadProgress.pct + '%' }" />
            </div>
          </div>

          <!-- New folder input -->
          <div v-if="showNewFolder" class="flex items-center gap-2">
            <input
              v-model="newFolderName"
              type="text"
              :placeholder="t('server.files.folderPlaceholder')"
              class="flex-1 bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50"
              @keydown.enter="createFolder"
            />
            <UiButton variant="primary" size="sm" :loading="creatingFolder" @click="createFolder">{{ t('actions.create') }}</UiButton>
            <UiButton variant="secondary" size="sm" @click="showNewFolder = false; newFolderName = ''">{{ t('actions.cancel') }}</UiButton>
          </div>

          <!-- Error -->
          <div v-if="filesError" class="bg-tp-error/10 text-tp-error rounded-xl px-4 py-3 text-sm">
            {{ filesError }}
          </div>

          <!-- Loading -->
          <div v-if="filesLoading" class="space-y-1">
            <div v-for="i in 6" :key="i" class="h-10 bg-tp-surface2 rounded-xl animate-pulse" />
          </div>

          <!-- File list -->
          <div v-else-if="fileEntries.length > 0" class="bg-tp-surface rounded-xl overflow-hidden">
            <!-- Header -->
            <div class="flex items-center px-4 py-2 border-b border-tp-border text-[10px] uppercase tracking-widest font-semibold text-tp-outline">
              <span class="flex-1">{{ t('server.files.name') }}</span>
              <span class="w-24 text-right">{{ t('server.files.size') }}</span>
              <span class="w-44 text-right">{{ t('server.files.modified') }}</span>
              <span class="w-32 text-right">{{ t('server.files.actions') }}</span>
            </div>
            <div class="divide-y divide-tp-border">
              <div
                v-for="entry in fileEntries" :key="entry.name"
                class="flex items-center px-4 py-2.5 hover:bg-tp-surface2/50 transition-colors group"
              >
                <div class="flex items-center gap-2.5 flex-1 min-w-0 cursor-pointer" @click="openFile(entry)">
                  <FolderOpen v-if="entry.is_dir" class="w-4 h-4 text-tp-primary shrink-0" />
                  <FileArchive v-else-if="['jar','zip','gz','tar'].includes(entry.name.split('.').pop()?.toLowerCase() ?? '')" class="w-4 h-4 text-yellow-400 shrink-0" />
                  <FileText v-else-if="['json','toml','yaml','yml','xml','properties','cfg','conf','ini','txt','md','log','csv'].includes(entry.name.split('.').pop()?.toLowerCase() ?? '')" class="w-4 h-4 text-tp-tertiary shrink-0" />
                  <File v-else class="w-4 h-4 text-tp-muted shrink-0" />
                  <span class="text-tp-text text-sm truncate" :class="entry.is_dir ? 'font-medium' : ''">{{ entry.name }}</span>
                </div>
                <span class="w-24 text-right text-tp-muted text-xs">{{ entry.is_dir ? '\u2014' : formatFileSize(entry.size) }}</span>
                <span class="w-44 text-right text-tp-muted text-xs">{{ entry.modified ? new Date(entry.modified).toLocaleString() : '\u2014' }}</span>
                <div class="w-32 flex justify-end gap-1">
                  <!-- Download (files only) -->
                  <button
                    v-if="!entry.is_dir"
                    class="p-1 text-tp-muted hover:text-tp-accent transition-colors opacity-0 group-hover:opacity-100"
                    :title="t('actions.download')"
                    @click.stop="downloadFile(entry)"
                  >
                    <Download class="w-3.5 h-3.5" />
                  </button>
                  <!-- Extract (zip files only) -->
                  <button
                    v-if="!entry.is_dir && entry.name.toLowerCase().endsWith('.zip')"
                    class="p-1 text-tp-muted hover:text-yellow-400 transition-colors opacity-0 group-hover:opacity-100"
                    :disabled="extractingPath === entry.name"
                    :title="t('server.files.extract')"
                    @click.stop="extractFile(entry)"
                  >
                    <PackageOpen class="w-3.5 h-3.5" />
                  </button>
                  <!-- Archive (directories only) -->
                  <button
                    v-if="entry.is_dir"
                    class="p-1 text-tp-muted hover:text-yellow-400 transition-colors opacity-0 group-hover:opacity-100"
                    :disabled="archivingPath === entry.name"
                    :title="t('server.files.archive')"
                    @click.stop="archiveEntry(entry)"
                  >
                    <FileArchive class="w-3.5 h-3.5" />
                  </button>
                  <!-- Delete -->
                  <button
                    class="p-1 text-tp-muted hover:text-tp-danger transition-colors opacity-0 group-hover:opacity-100"
                    :disabled="deletingPath === entry.name"
                    :title="t('actions.delete')"
                    @click.stop="deleteFileOrDir(entry)"
                  >
                    <Trash2 class="w-3.5 h-3.5" />
                  </button>
                </div>
              </div>
            </div>
          </div>

          <!-- Empty -->
          <div v-else-if="!filesLoading && !filesError" class="bg-tp-surface rounded-xl p-12 text-center">
            <FolderOpen class="w-8 h-8 text-tp-muted mx-auto mb-3" />
            <p class="text-tp-text font-semibold text-sm mb-1">{{ t('server.files.emptyTitle') }}</p>
            <p class="text-tp-muted text-xs">{{ t('server.files.emptyHint') }}</p>
          </div>
        </template>
      </div>

      <!-- ── Worlds tab ──────────────────────────────────────────────────── -->
      <div v-else-if="activeTab === 'worlds'" class="space-y-4">
        <div v-if="worldsStore.loading" class="bg-tp-surface rounded-xl p-8 text-center">
          <Loader2 class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
          <p class="text-tp-muted text-sm">{{ t('server.worlds.loading') }}</p>
        </div>
        <div v-else-if="worldsStore.worlds.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
          <Globe class="w-8 h-8 text-tp-muted mx-auto mb-3" />
          <p class="text-tp-text font-display font-semibold text-sm mb-1">{{ t('server.worlds.emptyTitle') }}</p>
          <i18n-t keypath="server.worlds.emptyHint" tag="p" scope="global" class="text-tp-muted text-xs">
            <template #path><code class="font-mono">universe/worlds</code></template>
          </i18n-t>
        </div>
        <template v-else>
          <i18n-t keypath="server.worlds.activateHint" tag="p" scope="global" class="text-tp-muted text-xs">
            <template #file><code class="font-mono">config.json</code></template>
          </i18n-t>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div v-for="w in worldsStore.worlds" :key="w.id"
              :class="['bg-tp-surface rounded-xl p-4 flex items-center justify-between', w.is_active ? 'ring-1 ring-tp-success/40' : '']">
              <div class="flex items-center gap-3">
                <Globe :class="['w-5 h-5', w.is_active ? 'text-tp-success' : 'text-tp-muted']" />
                <div>
                  <p class="text-tp-text text-sm font-medium">{{ w.name }}</p>
                  <p class="text-tp-muted text-xs">{{ w.is_active ? t('server.worlds.active') : t('server.worlds.inactive') }}</p>
                </div>
              </div>
              <div class="flex items-center gap-1">
                <button v-if="!w.is_active" class="p-1.5 rounded-xl text-tp-muted hover:text-tp-success hover:bg-tp-success/10 transition-colors text-xs"
                  :title="t('server.worlds.activateTitle')"
                  @click="worldsStore.setActive(serverId, w.id).then(r => showToast(r?.message ?? t('server.worlds.activated'))).catch(() => showToast(t('server.shared.failed'), 'error'))">
                  {{ t('server.worlds.activate') }}
                </button>
                <!-- Deleting the active world would leave the server booting into
                     a world that no longer exists; the API rejects it (409). -->
                <button class="p-1.5 rounded-xl text-tp-danger hover:bg-tp-danger/10 transition-colors disabled:opacity-40 disabled:hover:bg-transparent"
                  :disabled="w.is_active"
                  :title="w.is_active ? t('server.worlds.activateOtherFirst') : t('server.worlds.deleteTitle')"
                  :aria-label="t('server.worlds.deleteTitle')"
                  @click="confirmAction(t('server.worlds.deleteConfirm', { name: w.name })) && worldsStore.deleteWorld(serverId, w.id).then(() => showToast(t('server.worlds.deleted'))).catch(() => showToast(t('server.shared.failed'), 'error'))">
                  <Trash2 class="w-3.5 h-3.5" />
                </button>
              </div>
            </div>
          </div>
        </template>
      </div>

      <!-- ── Players tab ─────────────────────────────────────────────────── -->
      <div v-else-if="activeTab === 'players'" class="space-y-4">
        <div v-if="playersStore.loading" class="bg-tp-surface rounded-xl p-8 text-center">
          <Loader2 class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
          <p class="text-tp-muted text-sm">{{ t('server.players.loading') }}</p>
        </div>
        <div v-else-if="playersStore.players.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
          <Users class="w-8 h-8 text-tp-muted mx-auto mb-3" />
          <p class="text-tp-text font-display font-semibold text-sm mb-1">{{ t('server.players.emptyTitle') }}</p>
          <p class="text-tp-muted text-xs">{{ t('server.players.emptyHint') }}</p>
        </div>
        <div v-else class="bg-tp-surface rounded-xl overflow-hidden">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-tp-border">
                <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('server.players.player') }}</th>
                <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('server.players.status') }}</th>
                <th class="text-right px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">{{ t('server.players.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="p in playersStore.players" :key="p.id" class="border-b border-tp-border/50 hover:bg-tp-surface2/50">
                <td class="px-4 py-3">
                  <p class="text-tp-text font-medium">{{ p.username }}</p>
                  <p class="text-tp-muted text-xs font-mono">{{ p.hytale_uuid.substring(0, 8) }}...</p>
                </td>
                <td class="px-4 py-3">
                  <span v-if="p.is_banned" class="text-xs px-2 py-0.5 rounded-full bg-tp-error/10 text-tp-error">{{ t('server.players.banned') }}</span>
                  <span v-if="p.is_whitelisted" class="text-xs px-2 py-0.5 rounded-full bg-tp-success/15 text-tp-success ml-1">{{ t('server.players.whitelisted') }}</span>
                </td>
                <td class="px-4 py-3 text-right">
                  <button v-if="!p.is_banned" class="text-xs text-tp-danger hover:text-tp-danger/80 transition-colors" @click="playersStore.banPlayer(serverId, p.id, '').then(() => showToast(t('server.players.bannedToast')))">{{ t('server.players.ban') }}</button>
                  <button v-else class="text-xs text-tp-success hover:text-tp-success/80 transition-colors" @click="playersStore.unbanPlayer(serverId, p.id).then(() => showToast(t('server.players.unbannedToast')))">{{ t('server.players.unban') }}</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- ── Backups tab ─────────────────────────────────────────────────── -->
      <div v-else-if="activeTab === 'backups'" class="space-y-4">
        <div class="flex items-center justify-end">
          <button class="flex items-center gap-1.5 px-3 py-1.5 rounded-xl bg-tp-primary text-white text-xs font-medium hover:bg-tp-primary/90 transition-colors"
            @click="backupsStore.createBackup({ server_id: serverId }).then(() => showToast(t('server.backups.created'))).catch(() => showToast(t('server.shared.failed'), 'error'))">
            <Plus class="w-3.5 h-3.5" /> {{ t('server.backups.create') }}
          </button>
        </div>
        <div v-if="backupsStore.loading" class="bg-tp-surface rounded-xl p-8 text-center">
          <Loader2 class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
          <p class="text-tp-muted text-sm">{{ t('server.backups.loading') }}</p>
        </div>
        <div v-else-if="backupsStore.backups.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
          <DatabaseBackup class="w-8 h-8 text-tp-muted mx-auto mb-3" />
          <p class="text-tp-text font-display font-semibold text-sm mb-1">{{ t('server.backups.emptyTitle') }}</p>
          <p class="text-tp-muted text-xs">{{ t('server.backups.emptyHint') }}</p>
        </div>
        <div v-else class="space-y-2">
          <div v-for="b in backupsStore.backups" :key="b.id" class="bg-tp-surface rounded-xl p-4 flex items-center justify-between">
            <div class="flex items-center gap-3">
              <DatabaseBackup class="w-5 h-5 text-tp-muted" />
              <div>
                <p class="text-tp-text text-sm font-medium capitalize">{{ t('server.backups.typeBackup', { type: b.type }) }}</p>
                <p class="text-tp-muted text-xs">{{ new Date(b.created_at).toLocaleString() }} — {{ b.status }}<template v-if="b.upload_status"> · {{ b.destination_name ?? t('server.backups.offSite') }}: {{ b.upload_status }}</template></p>
              </div>
            </div>
            <div class="flex items-center gap-1">
              <button v-if="b.status === 'completed'" class="text-xs text-tp-accent hover:text-tp-primary transition-colors" @click="backupsStore.restoreBackup(b.id).then(() => showToast(t('server.backups.restoreInitiated')))">{{ t('server.backups.restore') }}</button>
              <button class="p-1.5 rounded-xl text-tp-danger hover:bg-tp-danger/10 transition-colors" :title="t('server.backups.deleteTitle')" :aria-label="t('server.backups.deleteTitle')"
                @click="confirmAction(t('server.backups.deleteConfirm')) && backupsStore.deleteBackup(b.id).then(() => showToast(t('server.backups.deleted')))">
                <Trash2 class="w-3.5 h-3.5" />
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- ── Schedules tab ──────────────────────────────────────────────── -->
      <ServerConfigEditor v-else-if="activeTab === 'config'" :server-id="serverId" :status="server.status" @toast="showToast" />

      <ServerTasksPanel v-else-if="activeTab === 'tasks'" :server-id="serverId" @toast="showToast" />

      <!-- ── Databases tab ──────────────────────────────────────────────── -->
      <div v-else-if="activeTab === 'databases'" class="space-y-4">
        <div class="flex items-center justify-between">
          <div>
            <h3 class="text-tp-text font-display font-semibold text-lg">{{ t('server.databases.title') }}</h3>
            <p class="text-tp-muted text-sm">{{ t('server.databases.subtitle') }}</p>
          </div>
        </div>

        <div v-if="dbLoading" class="bg-tp-surface rounded-xl p-8 text-center">
          <Loader2 class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
          <p class="text-tp-muted text-sm">{{ t('server.databases.loading') }}</p>
        </div>
        <div v-else-if="!database" class="bg-tp-surface rounded-xl p-12 text-center">
          <Database class="w-8 h-8 text-tp-muted mx-auto mb-3" />
          <p class="text-tp-text font-display font-semibold text-sm mb-1">{{ t('server.databases.emptyTitle') }}</p>
          <p class="text-tp-muted text-xs mb-4">{{ t('server.databases.emptyHint') }}</p>
          <UiButton variant="primary" size="sm" :disabled="creatingDb" @click="createDatabase">
            <Loader2 v-if="creatingDb" class="w-3.5 h-3.5 animate-spin" />
            <Plus v-else class="w-3.5 h-3.5" /> {{ t('server.databases.create') }}
          </UiButton>
        </div>
        <div v-else class="bg-tp-surface rounded-xl p-5">
          <div class="flex items-start justify-between">
            <div class="flex items-center gap-3">
              <div class="w-9 h-9 rounded-xl bg-tp-primary/10 flex items-center justify-center">
                <Database class="w-4 h-4 text-tp-primary" />
              </div>
              <div>
                <p class="text-tp-text font-semibold text-sm font-mono">{{ database.db_name }}</p>
                <p class="text-tp-muted text-xs font-mono">MariaDB | {{ database.host }}:{{ database.port }}</p>
              </div>
            </div>
            <div class="flex items-center gap-2">
              <button class="p-1.5 rounded-xl text-tp-warning hover:bg-tp-warning/10 transition-colors"
                :disabled="rotatingDb"
                :title="t('server.databases.rotateTitle')"
                :aria-label="t('server.databases.rotateTitle')"
                @click="rotateDatabasePassword">
                <RotateCcw class="w-3.5 h-3.5" :class="{ 'animate-spin': rotatingDb }" />
              </button>
              <button class="p-1.5 rounded-xl text-tp-danger hover:bg-tp-danger/10 transition-colors"
                :title="t('server.databases.deleteTitle')"
                :aria-label="t('server.databases.deleteTitle')"
                @click="deleteDatabase">
                <Trash2 class="w-3.5 h-3.5" />
              </button>
            </div>
          </div>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-2 mt-4">
            <div class="bg-tp-surface2 rounded-xl px-3 py-2 text-xs flex items-center gap-2">
              <span class="text-tp-outline w-20 shrink-0">{{ t('server.databases.host') }}</span>
              <code class="text-tp-text font-mono flex-1 break-all">{{ database.host }}</code>
            </div>
            <div class="bg-tp-surface2 rounded-xl px-3 py-2 text-xs flex items-center gap-2">
              <span class="text-tp-outline w-20 shrink-0">{{ t('server.databases.port') }}</span>
              <code class="text-tp-text font-mono flex-1">{{ database.port }}</code>
            </div>
            <div class="bg-tp-surface2 rounded-xl px-3 py-2 text-xs flex items-center gap-2">
              <span class="text-tp-outline w-20 shrink-0">{{ t('server.databases.database') }}</span>
              <code class="text-tp-text font-mono flex-1 break-all">{{ database.db_name }}</code>
            </div>
            <div class="bg-tp-surface2 rounded-xl px-3 py-2 text-xs flex items-center gap-2">
              <span class="text-tp-outline w-20 shrink-0">{{ t('server.databases.username') }}</span>
              <code class="text-tp-text font-mono flex-1 break-all">{{ database.db_user }}</code>
            </div>
            <div class="bg-tp-surface2 rounded-xl px-3 py-2 text-xs flex items-center gap-2 sm:col-span-2">
              <span class="text-tp-outline w-20 shrink-0">{{ t('server.databases.password') }}</span>
              <code class="text-tp-text font-mono flex-1 break-all">{{ database.db_password }}</code>
            </div>
          </div>
        </div>

        <!-- Rotated credentials modal — shown once, caller must copy -->
        <UiModal :open="!!rotatedCreds" :title="t('server.databases.newCredsTitle')" @close="rotatedCreds = null">
          <div v-if="rotatedCreds" class="space-y-3">
            <p class="text-tp-muted text-xs">{{ t('server.databases.copyNow') }}</p>
            <div class="space-y-1.5">
              <div v-for="[label, val] in [
                [t('server.databases.host'), rotatedCreds.host],
                [t('server.databases.port'), String(rotatedCreds.port)],
                [t('server.databases.database'), rotatedCreds.db_name],
                [t('server.databases.username'), rotatedCreds.db_user],
                [t('server.databases.password'), rotatedCreds.db_password],
              ]" :key="label" class="flex items-center gap-2 bg-tp-surface2 rounded-xl px-3 py-2 text-xs">
                <span class="text-tp-outline w-20 shrink-0">{{ label }}</span>
                <code class="text-tp-text font-mono flex-1 break-all">{{ val }}</code>
              </div>
            </div>
            <div class="flex justify-end pt-2">
              <UiButton variant="primary" size="md" @click="rotatedCreds = null">{{ t('server.databases.done') }}</UiButton>
            </div>
          </div>
        </UiModal>
      </div>

      <!-- ── Settings tab ────────────────────────────────────────────────── -->
      <div v-else-if="activeTab === 'settings'" class="space-y-4">
        <div class="bg-tp-surface rounded-xl overflow-hidden">
          <div class="px-5 py-3 border-b border-tp-border">
            <h3 class="text-tp-text font-display font-semibold text-sm">{{ t('server.settings.title') }}</h3>
          </div>
          <div class="divide-y divide-tp-border">
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">{{ t('server.settings.name') }}</span>
              <span class="text-tp-text text-sm font-medium">{{ server?.name }}</span>
            </div>
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">{{ t('server.settings.updateChannel') }}</span>
              <span class="text-tp-text text-sm">{{ formatHytaleVersion(server?.hytale_version) }}</span>
            </div>
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">{{ t('server.settings.port') }}</span>
              <span class="text-tp-text text-sm font-mono">{{ server?.port }}</span>
            </div>
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">{{ t('server.settings.ramLimit') }}</span>
              <span class="text-tp-text text-sm">{{ server?.ram_limit_mb ?? '\u2014' }} MB</span>
            </div>
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">{{ t('server.settings.cpuLimit') }}</span>
              <span class="text-tp-text text-sm">{{ server?.cpu_limit ? `${server.cpu_limit}%` : t('server.shared.unlimited') }}</span>
            </div>
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">{{ t('server.settings.autoRestart') }}</span>
              <span :class="['text-sm font-medium', server?.auto_restart ? 'text-tp-success' : 'text-tp-muted']">{{ server?.auto_restart ? t('server.shared.enabled') : t('server.shared.disabled') }}</span>
            </div>
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">{{ t('server.settings.activeWorld') }}</span>
              <span class="text-tp-text text-sm">{{ server?.active_world || '\u2014' }}</span>
            </div>
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">{{ t('server.settings.created') }}</span>
              <span class="text-tp-muted text-xs">{{ server?.created_at ? new Date(server.created_at).toLocaleString() : '\u2014' }}</span>
            </div>
          </div>
        </div>
        <!-- Launch settings -->
        <form class="bg-tp-surface rounded-xl overflow-hidden" @submit.prevent="saveLaunchSettings">
          <div class="px-5 py-3 border-b border-tp-border">
            <h3 class="text-tp-text font-display font-semibold text-sm">{{ t('server.settings.launch.title') }}</h3>
            <p class="text-tp-muted text-xs mt-0.5">{{ t('server.settings.launch.hint') }}</p>
          </div>
          <div class="px-5 py-4 space-y-4">
            <div v-if="authStore.isAdmin" class="max-w-xs">
              <UiInput v-model="launchForm.ram_limit_mb" type="number" :label="t('server.settings.launch.ramLabel')" placeholder="4096" />
              <p class="text-tp-muted text-xs mt-1">{{ t('server.settings.launch.ramHint') }}</p>
            </div>
            <div class="max-w-xs space-y-1">
              <label for="launch-hytale-version" class="block text-tp-text text-sm">{{ t('server.settings.updateChannel') }}</label>
              <select id="launch-hytale-version" v-model="launchForm.hytale_version"
                class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
                <option value="latest">{{ t('server.update.release') }}</option>
                <option value="pre-release">{{ t('server.update.preRelease') }}</option>
              </select>
              <p v-if="launchForm.hytale_version === 'pre-release' && normChannel(server?.hytale_version) !== 'pre-release'" class="text-tp-warning text-xs">
                {{ t('server.settings.launch.preReleaseWarn') }}
              </p>
              <p v-else-if="launchForm.hytale_version === 'latest' && normChannel(server?.hytale_version) === 'pre-release'" class="text-tp-warning text-xs">
                {{ t('server.settings.launch.releaseWarn') }}
              </p>
              <p v-else class="text-tp-muted text-xs">{{ t('server.settings.launch.channelHint') }}</p>
            </div>
            <div class="space-y-2">
              <label v-for="opt in LAUNCH_FLAG_OPTIONS" :key="opt.flag" class="flex items-start gap-3"
                :class="opt.adminOnly && !authStore.isAdmin ? 'opacity-60 cursor-not-allowed' : 'cursor-pointer'">
                <input v-model="launchForm.launch_flags" type="checkbox" :value="opt.flag" class="mt-1 accent-tp-primary"
                  :disabled="opt.adminOnly && !authStore.isAdmin">
                <span>
                  <span class="text-tp-text text-sm">{{ t(`server.settings.launch.flags.${opt.key}.label`) }}</span>
                  <span class="text-tp-muted text-xs font-mono ml-2">{{ opt.flag }}</span>
                  <span class="block text-tp-muted text-xs">{{ t(`server.settings.launch.flags.${opt.key}.hint`) }}</span>
                </span>
              </label>
            </div>
            <!-- Admins enable it; an owner only sees it (and can opt out) once enabled. -->
            <label v-if="authStore.isAdmin || server?.use_hytale_account" class="flex items-start gap-3 cursor-pointer">
              <input v-model="launchForm.use_hytale_account" type="checkbox" class="mt-1 accent-tp-primary"
                :disabled="!authStore.isAdmin && !launchForm.use_hytale_account">
              <span>
                <span class="text-tp-text text-sm">{{ t('server.settings.launch.hytaleAccount') }}</span>
                <span class="block text-tp-muted text-xs">
                  {{ t('server.settings.launch.hytaleAccountHint') }}
                </span>
              </span>
            </label>
            <div class="flex justify-end">
              <UiButton type="submit" :loading="launchSaving">{{ t('actions.save') }}</UiButton>
            </div>
          </div>
        </form>
        <!-- Network routing: admins edit, everyone sees the role once set -->
        <form v-if="isAdmin || server?.network_role" class="bg-tp-surface rounded-xl overflow-hidden"
          @submit.prevent="saveNetworkRole">
          <div class="px-5 py-3 border-b border-tp-border">
            <h3 class="text-tp-text font-display font-semibold text-sm">{{ t('server.settings.network.title') }}</h3>
            <p class="text-tp-muted text-xs mt-0.5">
              {{ t('server.settings.network.hint') }}
            </p>
          </div>
          <div class="px-5 py-4 space-y-4">
            <fieldset class="space-y-2" :disabled="!isAdmin">
              <legend class="sr-only">{{ t('server.settings.network.roleLegend') }}</legend>
              <label v-for="opt in NETWORK_ROLE_OPTIONS" :key="opt.value" class="flex items-start gap-3"
                :class="isAdmin ? 'cursor-pointer' : 'opacity-60 cursor-not-allowed'">
                <input v-model="networkRole" type="radio" name="network-role" :value="opt.value" class="mt-1 accent-tp-primary">
                <span>
                  <span class="text-tp-text text-sm">{{ t(`server.settings.network.roles.${opt.value}.label`) }}</span>
                  <span class="block text-tp-muted text-xs">{{ t(`server.settings.network.roles.${opt.value}.hint`, { play: '/play <gamemode>' }) }}</span>
                </span>
              </label>
            </fieldset>
            <div v-if="server?.network_role" class="flex items-center justify-between text-sm">
              <span class="text-tp-outline">{{ t('server.settings.network.requireReferral') }}</span>
              <span :class="['font-medium', server.require_referral ? 'text-tp-success' : 'text-tp-muted']">
                {{ server.require_referral ? t('actions.yes') : t('actions.no') }}
              </span>
            </div>
            <div v-if="isAdmin" class="flex flex-wrap justify-end gap-2">
              <UiButton type="button" variant="secondary" :loading="networkDistributing" @click="redistributeNetworkConfig">
                {{ t('server.settings.network.resend') }}
              </UiButton>
              <UiButton type="submit" :loading="networkSaving" :disabled="!networkRoleChanged">{{ t('actions.save') }}</UiButton>
            </div>
          </div>
        </form>
        <!-- Shared access / invitations -->
        <div class="bg-tp-surface rounded-xl overflow-hidden">
          <div class="px-5 py-3 border-b border-tp-border flex items-center justify-between">
            <div>
              <h3 class="text-tp-text font-display font-semibold text-sm">{{ t('server.settings.access.title') }}</h3>
              <p class="text-tp-muted text-xs mt-0.5">{{ t('server.settings.access.hint') }}</p>
            </div>
            <button
              class="p-1.5 rounded-xl text-tp-muted hover:bg-tp-surface2"
              :title="t('actions.refresh')"
              :aria-label="t('actions.refresh')"
              @click="loadServerInvitations()"
            >
              <RotateCcw class="w-3.5 h-3.5" />
            </button>
          </div>
          <div class="p-5 space-y-3">
            <div class="flex items-end gap-2">
              <div class="flex-1">
                <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">{{ t('server.settings.access.inviteeEmail') }}</label>
                <input
                  v-model="inviteForm.email"
                  type="email"
                  :placeholder="t('server.settings.access.emailPlaceholder')"
                  class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text"
                />
              </div>
              <div>
                <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">{{ t('server.settings.access.role') }}</label>
                <select v-model="inviteForm.role" class="bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
                  <option value="viewer">{{ t('server.settings.access.roles.viewer') }}</option>
                  <option value="moderator">{{ t('server.settings.access.roles.moderator') }}</option>
                  <option value="admin">{{ t('server.settings.access.roles.admin') }}</option>
                </select>
              </div>
              <button
                class="px-3 py-2 rounded-xl bg-tp-primary text-white text-sm font-medium disabled:opacity-50"
                :disabled="invitingEmail || !inviteForm.email"
                @click="createServerInvitation"
              >
                {{ invitingEmail ? t('server.settings.access.sending') : t('server.settings.access.invite') }}
              </button>
            </div>
            <p class="text-tp-muted text-xs">
              {{ t('server.settings.access.rolesHint') }}
            </p>

            <div v-if="invitationsStore.serverInvitations.length === 0" class="text-tp-muted text-xs py-3 text-center">
              {{ t('server.settings.access.noPending') }}
            </div>
            <div v-else class="divide-y divide-tp-border border border-tp-border rounded-xl">
              <div
                v-for="inv in invitationsStore.serverInvitations" :key="inv.id"
                class="flex items-center justify-between px-3 py-2"
              >
                <div class="text-sm">
                  <p class="text-tp-text font-medium">{{ inv.invitee_email }}</p>
                  <p class="text-tp-muted text-xs">
                    <span class="capitalize">{{ inv.role }}</span>
                    · <span class="capitalize">{{ inv.status }}</span>
                    · {{ new Date(inv.created_at).toLocaleDateString() }}
                  </p>
                </div>
                <button
                  v-if="inv.status === 'pending'"
                  class="text-tp-danger text-xs hover:underline"
                  @click="revokeServerInvitation(inv.id)"
                >
                  {{ t('server.settings.access.revoke') }}
                </button>
              </div>
            </div>
          </div>
        </div>

        <div class="bg-tp-surface rounded-xl border border-tp-danger/30 p-5">
          <h3 class="text-tp-danger font-display font-semibold text-sm mb-2">{{ t('server.settings.danger.title') }}</h3>
          <p class="text-tp-muted text-xs mb-3">{{ t('server.settings.danger.hint') }}</p>
          <button class="px-4 py-2 rounded-xl bg-tp-danger text-white text-xs font-medium hover:bg-tp-danger/90 transition-colors"
            @click="deleteServerAndLeave">
            {{ t('server.settings.danger.delete') }}
          </button>
        </div>
      </div>
    </template>

    <!-- Migrate to Node Modal -->
    <UiModal :open="showMigrateModal" :title="t('server.migrate.title')" size="sm" @close="showMigrateModal = false">
      <div class="space-y-4">
        <p class="text-tp-outline text-sm">{{ t('server.migrate.intro') }}</p>
        <div v-if="migrateError" class="bg-tp-error/10 rounded-xl px-3 py-2.5 text-tp-error text-sm">{{ migrateError }}</div>
        <div v-if="migrateNodes.length === 0 && !migrateError" class="text-tp-muted text-sm">{{ t('server.migrate.noNodes') }}</div>
        <div v-else class="space-y-2">
          <label
            v-for="n in migrateNodes"
            :key="n.id"
            class="flex items-center gap-3 p-3 rounded-xl cursor-pointer transition-colors"
            :class="migrateTargetId === n.id ? 'bg-tp-primary/10 border border-tp-primary/30' : 'bg-tp-surface3 hover:bg-tp-surface-highest'"
          >
            <input type="radio" class="sr-only" :value="n.id" v-model="migrateTargetId" />
            <span class="material-symbols-outlined text-tp-outline text-base">dns</span>
            <span class="text-tp-text text-sm font-medium">{{ n.name }}</span>
          </label>
        </div>
      </div>
      <template #footer>
        <UiButton variant="ghost" size="md" @click="showMigrateModal = false">{{ t('actions.cancel') }}</UiButton>
        <UiButton
          variant="primary"
          size="md"
          :loading="migrateLoading"
          :disabled="!migrateTargetId"
          @click="submitMigrate"
        >{{ t('server.page.migrate') }}</UiButton>
      </template>
    </UiModal>

    <!-- Hytale OAuth device-code prompt -->
    <UiModal :open="showHytaleAuthModal" :title="hytaleAuthCode ? t('server.hytaleAuth.titleLink') : t('server.hytaleAuth.titleAuth')" size="md" @close="hytaleAuthDismissed = true">
      <p class="text-tp-text text-sm mb-4">
        {{ t('server.hytaleAuth.intro') }}
      </p>

      <template v-if="hytaleAuthCode">
        <div class="bg-tp-surface3 rounded-xl px-4 py-4 mb-4">
          <div class="text-[10px] uppercase tracking-widest text-tp-outline mb-2">{{ t('server.hytaleAuth.yourCode') }}</div>
          <div class="flex items-center justify-between gap-3">
            <code class="font-mono text-tp-text text-2xl tracking-[0.3em]">{{ hytaleAuthCode }}</code>
            <button
              class="inline-flex items-center gap-1.5 text-xs text-tp-muted hover:text-tp-text transition-colors"
              @click="copyHytaleAuthCode"
            >
              <Copy class="w-3.5 h-3.5" /> {{ t('actions.copy') }}
            </button>
          </div>
        </div>

        <a
          :href="hytaleAuthUrl"
          target="_blank"
          rel="noopener"
          class="block w-full text-center bg-tp-primary text-tp-bg font-semibold rounded-xl py-3 mb-3 hover:bg-tp-primary/90 transition-colors"
        >{{ t('server.hytaleAuth.openVerify') }}</a>

        <i18n-t keypath="server.hytaleAuth.prefillHint" tag="p" scope="global" class="text-tp-muted text-xs mb-1">
          <template #url><span class="text-tp-text">oauth.accounts.hytale.com/oauth2/device/verify</span></template>
          <template #code><code class="font-mono text-tp-text">{{ hytaleAuthCode }}</code></template>
        </i18n-t>
        <p class="text-tp-outline text-xs">
          {{ t('server.hytaleAuth.signInHint') }}
        </p>
      </template>

      <template v-else>
        <ol class="list-decimal list-inside text-tp-muted text-sm space-y-2 mb-4">
          <li>{{ t('server.hytaleAuth.step1') }}</li>
          <li>{{ t('server.hytaleAuth.step2') }}</li>
          <li>{{ t('server.hytaleAuth.step3') }}</li>
        </ol>
        <a
          :href="hytaleAuthUrl"
          target="_blank"
          rel="noopener"
          class="block w-full text-center bg-tp-primary text-tp-bg font-semibold rounded-xl py-3 mb-4 hover:bg-tp-primary/90 transition-colors break-all"
        >{{ t('server.hytaleAuth.openAuth') }}</a>
        <p class="text-tp-outline text-xs">
          {{ t('server.hytaleAuth.url') }} <span class="break-all">{{ hytaleAuthUrl }}</span>
        </p>
      </template>

      <template #footer>
        <UiButton variant="ghost" @click="hytaleAuthDismissed = true">{{ t('server.hytaleAuth.dismiss') }}</UiButton>
      </template>
    </UiModal>

    <!-- Toast -->
    <Transition name="toast">
      <div v-if="toastMessage" :class="[
        'fixed bottom-6 right-6 z-50 flex items-center gap-3 px-4 py-3 rounded-xl shadow-lg text-sm font-medium',
        toastType === 'success'
          ? 'bg-tp-success/15 text-tp-success'
          : 'bg-tp-error/10 text-tp-error',
      ]">
        <div :class="['w-2 h-2 rounded-full', toastType === 'success' ? 'bg-tp-success' : 'bg-tp-danger']" />
        {{ toastMessage }}
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.toast-enter-active, .toast-leave-active { transition: opacity 0.25s ease, transform 0.25s ease; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateY(8px); }

.progress-fill {
  background: linear-gradient(90deg, #e8863b, #f2c14e);
}
</style>
