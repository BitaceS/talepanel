<script setup lang="ts">
import {
  Play, Square, RotateCcw, Zap,
  Cpu, MemoryStick, Clock, Users,
  ArrowLeft, Terminal, FolderOpen, Puzzle,
  Globe, DatabaseBackup, Settings,
  Send, ChevronRight, Search, Download, Trash2, Package, ExternalLink,
  Gamepad2, Loader2, Plus, Database,
  File, FileText, FileArchive, FolderPlus, ArrowUpLeft, Save, X, Pencil,
  Upload, PackageOpen, Copy, Info,
} from 'lucide-vue-next'
import { useServersStore } from '~/stores/servers'
import { useModsStore } from '~/stores/mods'
import { useGameCommandsStore } from '~/stores/gameCommands'
import { useWorldsStore } from '~/stores/worlds'
import { usePlayersStore } from '~/stores/players'
import { useBackupsStore } from '~/stores/backups'
import { useApi } from '~/composables/useApi'
import type { CFMod, CFModFile } from '~/types'
import type { GameCommand } from '~/stores/gameCommands'

definePageMeta({ middleware: 'auth' })

const route = useRoute()
const serversStore = useServersStore()
const api = useApi()

const worldsStore = useWorldsStore()
const playersStore = usePlayersStore()
const backupsStore = useBackupsStore()

const serverId = computed(() => route.params.id as string)

onMounted(async () => {
  await serversStore.fetchServer(serverId.value)
})

const server = computed(() => serversStore.currentServer)

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
type TabId = 'overview' | 'console' | 'game-control' | 'files' | 'mods' | 'worlds' | 'players' | 'backups' | 'databases' | 'settings'

interface Tab { id: TabId; label: string; icon: unknown }

const tabs: Tab[] = [
  { id: 'overview',  label: 'Overview',  icon: Cpu },
  { id: 'console',   label: 'Console',   icon: Terminal },
  { id: 'game-control', label: 'Game Control', icon: Gamepad2 },
  { id: 'files',     label: 'Files',     icon: FolderOpen },
  { id: 'mods',      label: 'Mods',      icon: Puzzle },
  { id: 'worlds',    label: 'Worlds',    icon: Globe },
  { id: 'players',   label: 'Players',   icon: Users },
  { id: 'databases', label: 'Databases', icon: Database },
  { id: 'backups',   label: 'Backups',   icon: DatabaseBackup },
  { id: 'settings',  label: 'Settings',  icon: Settings },
]

const activeTab = ref<TabId>('overview')

// ── Power Actions ─────────────────────────────────────────────────────────────
const actionLoading = reactive<Record<string, boolean>>({
  start: false, stop: false, restart: false, kill: false,
})

const toastMessage = ref('')
const toastType = ref<'success' | 'error'>('success')
let toastTimer: ReturnType<typeof setTimeout>

function showToast(message: string, type: 'success' | 'error' = 'success') {
  toastMessage.value = message
  toastType.value = type
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toastMessage.value = '' }, 3000)
}

async function runAction(action: 'start' | 'stop' | 'restart' | 'kill') {
  if (!server.value) return
  actionLoading[action] = true
  try {
    await serversStore[`${action}Server`](serverId.value)
    showToast(`Server ${action} command sent`)
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? e.message ?? `Failed to ${action} server`, 'error')
  } finally {
    actionLoading[action] = false
  }
}

const canStart   = computed(() => server.value?.status === 'stopped' || server.value?.status === 'crashed')
const canStop    = computed(() => server.value?.status === 'running' || server.value?.status === 'starting')
const canRestart = computed(() => server.value?.status === 'running')
const canKill    = computed(() => server.value?.status !== 'stopped' && server.value?.status !== 'installing')

// ── Metrics polling ───────────────────────────────────────────────────────────
interface Metrics {
  cpu: { usage_percent: number }
  memory: { used_mb: number; limit_mb: number }
  uptime_s: number
  note?: string
}

const metrics = ref<Metrics | null>(null)
let metricsTimer: ReturnType<typeof setInterval>

async function fetchMetrics() {
  if (!serverId.value || activeTab.value !== 'overview') return
  try {
    const data = await api.get<Metrics>(`/servers/${serverId.value}/metrics`)
    metrics.value = data
  } catch { /* silent */ }
}

watch(activeTab, (tab) => {
  if (tab === 'overview') {
    fetchMetrics()
    metricsTimer = setInterval(fetchMetrics, 5000)
  } else {
    clearInterval(metricsTimer)
  }
  if (tab === 'worlds') worldsStore.fetchWorlds(serverId.value)
  if (tab === 'players') playersStore.fetchPlayers(serverId.value)
  if (tab === 'databases') fetchDatabases()
  if (tab === 'backups') {
    backupsStore.fetchBackups(serverId.value)
    backupsStore.fetchSchedules(serverId.value)
  }
}, { immediate: true })

onUnmounted(() => {
  clearInterval(metricsTimer)
  clearInterval(logsTimer)
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
let logsTimer: ReturnType<typeof setInterval>
let lastLogId = 0
const userScrolledUp = ref(false)

function onConsoleScroll() {
  const el = consoleEl.value
  if (!el) return
  // Consider "at bottom" if within 60px of the bottom
  userScrolledUp.value = el.scrollTop + el.clientHeight < el.scrollHeight - 60
}

async function fetchLogs() {
  if (!serverId.value || activeTab.value !== 'console') return
  try {
    const data = await api.get<{ logs: LogLine[] }>(`/servers/${serverId.value}/logs`)
    logs.value = data.logs ?? []
    if (logs.value.length > 0) {
      lastLogId = logs.value[logs.value.length - 1].id
    }
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

watch(activeTab, (tab) => {
  if (tab === 'console') {
    fetchLogs()
    logsTimer = setInterval(fetchLogs, 3000)
  } else {
    clearInterval(logsTimer)
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
    showToast(e.data?.error ?? 'Failed to send command', 'error')
  } finally {
    sendingCmd.value = false
  }
}

function handleConsoleKey(e: KeyboardEvent) {
  if (e.key === 'Enter') sendCommand()
}

function logLevelColor(level: string): string {
  switch (level?.toLowerCase()) {
    case 'error': case 'fatal': return 'text-tp-error'
    case 'warn':  case 'warning': return 'text-tp-warning'
    case 'info':  return 'text-tp-tertiary'
    case 'system': return 'text-tp-warning'
    case 'debug': return 'text-blue-400'
    default:      return 'text-tp-muted'
  }
}

function formatLogTime(iso: string): string {
  try {
    return new Date(iso).toLocaleTimeString('en-US', { hour12: false })
  } catch { return '' }
}

// ── Clipboard copy ───────────────────────────────────────────────────────────
const copiedField = ref('')
async function copyToClipboard(text: string, field: string) {
  try {
    await navigator.clipboard.writeText(text)
    copiedField.value = field
    setTimeout(() => { copiedField.value = '' }, 2000)
  } catch {
    showToast('Failed to copy', 'error')
  }
}

const serverAddress = computed(() => {
  if (!server.value) return ''
  return `${server.value.name.toLowerCase().replace(/\s+/g, '-')}.talepanel.io:${server.value.port}`
})

const sftpAddress = computed(() => {
  if (!server.value) return ''
  return `sftp://${server.value.name.toLowerCase().replace(/\s+/g, '-')}.talepanel.io:2022`
})

// ── Mods ──────────────────────────────────────────────────────────────────────
const modsStore = useModsStore()
const modsTab = ref<'installed' | 'browse'>('installed')
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
    showToast(`${mod.name} installed`)
  } catch {
    showToast(modsStore.error ?? 'Install failed', 'error')
  }
}

async function removeMod(filename: string, displayName: string) {
  try {
    await modsStore.remove(serverId.value, filename)
    showToast(`${displayName} removed`)
  } catch {
    showToast(modsStore.error ?? 'Remove failed', 'error')
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
      activeCmdCategory.value = gameCmdStore.categories[0]
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
  return params.every(p => !p.required || (values[p.name] && values[p.name].trim() !== ''))
}

async function executeCmd(cmd: GameCommand) {
  const params = getParamValues(cmd.id)
  await gameCmdStore.executeCommand(serverId.value, cmd, params)
  if (gameCmdStore.lastResult) {
    showToast(`Sent: ${gameCmdStore.lastResult.command}`)
  }
  if (gameCmdStore.error) {
    showToast(gameCmdStore.error, 'error')
  }
}

async function addCustomCommand() {
  let parsedParams: any[] = []
  try {
    parsedParams = JSON.parse(newCommand.value.params)
  } catch {
    showToast('Invalid params JSON', 'error')
    return
  }
  await gameCmdStore.createCommand(serverId.value, {
    ...newCommand.value,
    params: parsedParams as any,
  })
  if (!gameCmdStore.error) {
    showToast('Command added')
    showAddCommand.value = false
    newCommand.value = { category: '', name: '', description: '', command_template: '', icon: 'terminal', params: '[]', sort_order: 0, min_role: 'user' }
  } else {
    showToast(gameCmdStore.error, 'error')
  }
}

async function deleteCmd(cmdId: string) {
  await gameCmdStore.deleteCommand(serverId.value, cmdId)
  if (!gameCmdStore.error) showToast('Command removed')
}

// ── Plugin Commands (scan installed mods) ───────────────────────────────────
const pluginCommands = computed(() => {
  // Generate command suggestions based on installed mods
  return modsStore.installed.map(mod => ({
    modName: mod.display_name || mod.filename.replace(/\.[^.]+$/, ''),
    modId: mod.id,
    commands: [
      { name: `${mod.display_name || mod.filename.replace(/\.[^.]+$/, '')} Help`, template: `${mod.filename.replace(/\.[^.]+$/, '')} help`, description: `Show help for ${mod.display_name || mod.filename}` },
      { name: `${mod.display_name || mod.filename.replace(/\.[^.]+$/, '')} Reload`, template: `${mod.filename.replace(/\.[^.]+$/, '')} reload`, description: `Reload ${mod.display_name || mod.filename} config` },
    ]
  }))
})

watch(activeTab, (tab) => {
  if (tab === 'game-control') {
    modsStore.fetchInstalled(serverId.value)
  }
})

async function runPluginCommand(template: string) {
  try {
    await api.post(`/servers/${serverId.value}/game-commands/execute`, {
      command_template: template,
      params: {},
    })
    showToast(`Sent: ${template}`)
  } catch { showToast('Failed to send command', 'error') }
}

// ── Databases ────────────────────────────────────────────────────────────────
interface ServerDatabase {
  name: string
  type: string
  host: string
  port: number
  username: string
  status: string
  size: string | null
  connections: number
}

const databases = ref<ServerDatabase[]>([])
const dbLoading = ref(false)
const showCreateDbModal = ref(false)
const newDbForm = reactive({ name: '', type: 'mysql' })

async function fetchDatabases() {
  dbLoading.value = true
  try {
    const data = await api.get<{ databases: ServerDatabase[] }>(`/servers/${serverId.value}/database`)
    databases.value = data.databases ?? []
  } catch {
    databases.value = []
  } finally {
    dbLoading.value = false
  }
}

async function createDatabase() {
  try {
    const data = await api.post<{ database: ServerDatabase }>(`/servers/${serverId.value}/database`, newDbForm)
    databases.value.push(data.database)
    showCreateDbModal.value = false
    newDbForm.name = ''; newDbForm.type = 'mysql'
    showToast('Database created')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to create database', 'error')
  }
}

async function deleteDatabase(name: string) {
  try {
    await api.delete(`/servers/${serverId.value}/database`)
    databases.value = databases.value.filter(d => d.name !== name)
    showToast('Database deleted')
  } catch { showToast('Failed to delete database', 'error') }
}

const rotatingDb = ref('')
const rotatedCreds = ref<{ username: string; password: string; host: string; port: number; database: string } | null>(null)

async function rotateDatabasePassword(name: string) {
  if (!confirm(`Rotate password for database "${name}"?\n\nAny service using the old password will break until you update its connection string.`)) return
  rotatingDb.value = name
  try {
    const res = await api.post<{ credentials: { username: string; password: string; host: string; port: number; database: string } }>(
      `/servers/${serverId.value}/database/reset-password`,
      {},
    )
    rotatedCreds.value = res.credentials
    showToast('Password rotated — copy the new credentials now')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? 'Failed to rotate password', 'error')
  } finally {
    rotatingDb.value = ''
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
  const crumbs = [{ label: 'root', path: '/' }]
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
    filesError.value = e.data?.error ?? e.message ?? 'Failed to load files'
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
    showToast('File too large to edit (max 1 MB)', 'error')
    return
  }
  const ext = entry.name.split('.').pop()?.toLowerCase() ?? ''
  const textExts = ['json', 'toml', 'yaml', 'yml', 'properties', 'txt', 'cfg', 'conf', 'ini', 'log', 'xml', 'sh', 'bat', 'cmd', 'md', 'csv', 'env', 'jar']
  if (ext === 'jar') {
    showToast('Cannot edit binary files', 'error')
    return
  }
  try {
    const fpath = filePath.value === '/' ? '/' + entry.name : filePath.value + '/' + entry.name
    const data = await api.get<{ content: string }>(`/servers/${serverId.value}/files/content`, { path: fpath })
    editingFile.value = { path: fpath, content: data.content, name: entry.name }
    editContent.value = data.content
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to read file', 'error')
  }
}

async function saveFile() {
  if (!editingFile.value) return
  savingFile.value = true
  try {
    await api.put(`/servers/${serverId.value}/files/content`, { path: editingFile.value.path, content: editContent.value })
    showToast('File saved')
    editingFile.value.content = editContent.value
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to save file', 'error')
  } finally {
    savingFile.value = false
  }
}

async function deleteFileOrDir(entry: FileEntry) {
  const fpath = filePath.value === '/' ? '/' + entry.name : filePath.value + '/' + entry.name
  deletingPath.value = entry.name
  try {
    await api.delete(`/servers/${serverId.value}/files`, { path: fpath })
    showToast(`${entry.name} deleted`)
    fetchFiles()
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to delete', 'error')
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
    showToast('Folder created')
    newFolderName.value = ''
    showNewFolder.value = false
    fetchFiles()
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to create folder', 'error')
  } finally {
    creatingFolder.value = false
  }
}

const uploadingFile = ref(false)
const extractingPath = ref('')
const archivingPath = ref('')

const fileInputRef = ref<HTMLInputElement | null>(null)

function triggerUpload() {
  fileInputRef.value?.click()
}

async function handleFileUpload(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  uploadingFile.value = true
  try {
    const formData = new FormData()
    formData.append('file', file)
    const config = useRuntimeConfig()
    await $fetch(`${config.public.apiBase}/servers/${serverId.value}/files/upload?path=${encodeURIComponent(filePath.value)}`, {
      method: 'POST',
      body: formData,
      headers: authStore.accessToken ? { Authorization: `Bearer ${authStore.accessToken}` } : {},
      credentials: 'include',
    })
    showToast(`${file.name} uploaded`)
    fetchFiles()
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Upload failed', 'error')
  } finally {
    uploadingFile.value = false
    input.value = '' // reset input
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
    showToast('Download failed', 'error')
  }
}

async function extractFile(entry: FileEntry) {
  const fpath = filePath.value === '/' ? '/' + entry.name : filePath.value + '/' + entry.name
  extractingPath.value = entry.name
  try {
    await api.post(`/servers/${serverId.value}/files/extract`, { path: fpath })
    showToast(`${entry.name} extracted`)
    fetchFiles()
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Extract failed', 'error')
  } finally {
    extractingPath.value = ''
  }
}

async function archiveEntry(entry: FileEntry) {
  const fpath = filePath.value === '/' ? '/' + entry.name : filePath.value + '/' + entry.name
  archivingPath.value = entry.name
  try {
    await api.post(`/servers/${serverId.value}/files/archive`, { path: fpath })
    showToast(`${entry.name}.zip created`)
    fetchFiles()
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Archive failed', 'error')
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

function fileIcon(entry: FileEntry): string {
  if (entry.is_dir) return 'folder'
  const ext = entry.name.split('.').pop()?.toLowerCase() ?? ''
  if (['json', 'toml', 'yaml', 'yml', 'xml', 'properties', 'cfg', 'conf', 'ini'].includes(ext)) return 'config'
  if (['jar', 'zip', 'gz', 'tar'].includes(ext)) return 'archive'
  if (['log', 'txt', 'md'].includes(ext)) return 'text'
  return 'file'
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
      { name: 'No mods installed', version: '' },
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
      Back to Servers
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
      <p class="text-tp-muted">Server not found.</p>
      <NuxtLink to="/servers">
        <UiButton variant="secondary" size="sm" class="mt-4">Go back</UiButton>
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
            <p class="text-tp-outline text-sm font-mono">{{ displayId }} &middot; Port {{ server.port }} &middot; v{{ server.hytale_version }}</p>
          </div>
          <div class="flex items-center gap-2 flex-wrap">
            <UiButton variant="secondary" size="sm" :disabled="!canStart || actionLoading.start"
              :loading="actionLoading.start" @click="runAction('start')">
              <Play class="w-3.5 h-3.5" /> Start
            </UiButton>
            <UiButton variant="secondary" size="sm" :disabled="!canStop || actionLoading.stop"
              :loading="actionLoading.stop" @click="runAction('stop')">
              <Square class="w-3.5 h-3.5" /> Stop
            </UiButton>
            <UiButton variant="secondary" size="sm" :disabled="!canRestart || actionLoading.restart"
              :loading="actionLoading.restart" @click="runAction('restart')">
              <RotateCcw class="w-3.5 h-3.5" /> Restart
            </UiButton>
            <UiButton variant="danger" size="sm" :disabled="!canKill || actionLoading.kill"
              :loading="actionLoading.kill" @click="runAction('kill')">
              <Zap class="w-3.5 h-3.5" /> Kill
            </UiButton>
          </div>
        </div>
      </div>

      <!-- Stat cards row -->
      <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <div class="bg-tp-surface2 rounded-xl p-4">
          <div class="flex items-center justify-between mb-2">
            <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">CPU USAGE</span>
            <Info class="w-3.5 h-3.5 text-tp-outline" />
          </div>
          <p class="text-tp-text font-display font-bold text-2xl">
            {{ metrics ? `${metrics.cpu.usage_percent.toFixed(1)}%` : '\u2014' }}
          </p>
          <p class="text-tp-muted text-xs mt-1">
            {{ server.cpu_limit ? `Limit: ${server.cpu_limit}%` : 'No limit set' }}
          </p>
        </div>

        <div class="bg-tp-surface2 rounded-xl p-4">
          <div class="flex items-center justify-between mb-2">
            <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">MEMORY</span>
            <Info class="w-3.5 h-3.5 text-tp-outline" />
          </div>
          <p class="text-tp-text font-display font-bold text-2xl">
            {{ metrics ? `${metrics.memory.used_mb} MB` : '\u2014' }}
          </p>
          <p class="text-tp-muted text-xs mt-1">
            {{ metrics && metrics.memory.limit_mb > 0 ? `of ${metrics.memory.limit_mb} MB allocated` : (server.ram_limit_mb ? `of ${server.ram_limit_mb} MB allocated` : 'No limit') }}
          </p>
        </div>

        <div class="bg-tp-surface2 rounded-xl p-4">
          <div class="flex items-center justify-between mb-2">
            <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">PLAYERS</span>
            <Info class="w-3.5 h-3.5 text-tp-outline" />
          </div>
          <p class="text-tp-text font-display font-bold text-2xl">
            {{ playersStore.players.length }}<span class="text-tp-outline text-lg font-normal">/64</span>
          </p>
          <p class="text-tp-muted text-xs mt-1">Connected players</p>
        </div>

        <div class="bg-tp-surface2 rounded-xl p-4">
          <div class="flex items-center justify-between mb-2">
            <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">UPTIME</span>
            <Info class="w-3.5 h-3.5 text-tp-outline" />
          </div>
          <p class="text-tp-text font-display font-bold text-2xl">
            {{ metrics ? formatUptime(metrics.uptime_s) : '\u2014' }}
          </p>
          <p class="text-tp-muted text-xs mt-1">
            {{ metrics?.note ?? 'Live from daemon' }}
          </p>
        </div>
      </div>

      <!-- Tabs -->
      <div class="border-b border-tp-border">
        <nav class="flex gap-1 overflow-x-auto">
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
            {{ tab.label }}
          </button>
        </nav>
      </div>

      <!-- ── Overview Tab ─────────────────────────────────────────────────── -->
      <div v-if="activeTab === 'overview'" class="space-y-5">
        <!-- RAM progress bar -->
        <div v-if="metrics && metrics.memory.limit_mb > 0"
          class="bg-tp-surface rounded-xl p-4">
          <div class="flex items-center justify-between mb-2">
            <span class="text-tp-text text-sm font-medium">Memory Usage</span>
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

        <!-- Server info table -->
        <div class="bg-tp-surface rounded-xl overflow-hidden">
          <div class="px-5 py-3 border-b border-tp-border">
            <h3 class="text-tp-text font-display font-semibold text-sm">Server Information</h3>
          </div>
          <div class="divide-y divide-tp-border">
            <div v-for="row in [
              { label: 'Node ID', value: server.node_id, mono: true },
              { label: 'Port', value: server.port, mono: true },
              { label: 'Version', value: server.hytale_version },
              { label: 'Auto Restart', value: server.auto_restart ? 'Enabled' : 'Disabled' },
              { label: 'Active World', value: server.active_world || '\u2014' },
              { label: 'CPU Limit', value: server.cpu_limit ? `${server.cpu_limit}%` : 'Unlimited' },
              { label: 'RAM Limit', value: server.ram_limit_mb ? `${server.ram_limit_mb} MB` : 'Unlimited' },
              { label: 'Created', value: new Date(server.created_at).toLocaleString() },
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
            <span class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">LIVE TERMINAL OUTPUT</span>
          </div>

          <!-- Log viewer -->
          <div
            ref="consoleEl"
            class="bg-tp-surface-lowest rounded-xl h-[420px] overflow-y-auto p-4 font-mono text-xs space-y-0.5"
            @scroll="onConsoleScroll"
          >
            <div v-if="logs.length === 0" class="text-tp-muted py-4 text-center">
              No log output yet. Start the server to see console output and provisioning progress.
            </div>
            <div v-for="line in logs" :key="line.id" class="flex gap-2 leading-5">
              <span class="text-tp-outline shrink-0">{{ formatLogTime(line.logged_at) }}</span>
              <span :class="['shrink-0 w-12', logLevelColor(line.level)]">
                {{ line.level?.toUpperCase().slice(0, 6) }}
              </span>
              <span :class="logLevelColor(line.level)" class="break-all">{{ line.message }}</span>
            </div>
          </div>

          <!-- Command input -->
          <div class="flex gap-2">
            <div class="relative flex-1">
              <ChevronRight class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-tp-muted pointer-events-none" />
              <input
                v-model="consoleInput"
                type="text"
                placeholder="Enter server command..."
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
              Send
            </UiButton>
          </div>
          <p v-if="server.status !== 'running'" class="text-tp-muted text-xs">
            Server must be running to send commands.
          </p>
        </div>

        <!-- Right sidebar -->
        <div class="w-[300px] shrink-0 space-y-4 hidden xl:block">
          <!-- Quick Connect -->
          <div class="bg-tp-surface2 rounded-xl p-4 space-y-4">
            <h4 class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Quick Connect</h4>

            <!-- Server Address -->
            <div>
              <label class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline block mb-1.5">SERVER ADDRESS</label>
              <div class="flex items-center gap-2 bg-tp-surface-lowest rounded-lg px-3 py-2">
                <span class="text-tp-tertiary text-xs font-mono truncate flex-1">{{ serverAddress }}</span>
                <button
                  class="text-tp-outline hover:text-tp-text transition-colors shrink-0"
                  @click="copyToClipboard(serverAddress, 'address')"
                >
                  <Copy v-if="copiedField !== 'address'" class="w-3.5 h-3.5" />
                  <span v-else class="text-tp-tertiary text-[10px] font-semibold">OK</span>
                </button>
              </div>
            </div>

            <!-- SFTP Connection -->
            <div>
              <label class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline block mb-1.5">SFTP CONNECTION</label>
              <div class="flex items-center gap-2 bg-tp-surface-lowest rounded-lg px-3 py-2">
                <span class="text-tp-tertiary text-xs font-mono truncate flex-1">{{ sftpAddress }}</span>
                <button
                  class="text-tp-outline hover:text-tp-text transition-colors shrink-0"
                  @click="copyToClipboard(sftpAddress, 'sftp')"
                >
                  <Copy v-if="copiedField !== 'sftp'" class="w-3.5 h-3.5" />
                  <span v-else class="text-tp-tertiary text-[10px] font-semibold">OK</span>
                </button>
              </div>
            </div>
          </div>

          <!-- Active Mods -->
          <div class="bg-tp-surface2 rounded-xl p-4">
            <h4 class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-3">Active Mods</h4>
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
              Manage Mods &rarr;
            </button>
          </div>
        </div>
      </div>

      <!-- ── Mods Tab ──────────────────────────────────────────────────────── -->
      <div v-else-if="activeTab === 'mods'" class="space-y-4">
        <!-- Sub-tabs -->
        <div class="flex gap-1 bg-tp-surface2 rounded-xl p-1 w-fit">
          <button
            v-for="t in [{ id: 'installed', label: 'Installed' }, { id: 'browse', label: 'Browse CurseForge' }]"
            :key="t.id"
            :class="[
              'px-4 py-1.5 rounded-lg text-sm font-medium transition-colors',
              modsTab === t.id ? 'bg-tp-surface text-tp-text shadow-sm' : 'text-tp-muted hover:text-tp-text',
            ]"
            @click="modsTab = (t.id as 'installed' | 'browse')"
          >
            {{ t.label }}
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
            <p class="text-tp-text font-semibold text-sm mb-1">No mods installed</p>
            <p class="text-tp-muted text-xs">Switch to Browse to find and install mods from CurseForge.</p>
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
                  <p class="text-tp-muted text-xs">{{ mod.version }}</p>
                </div>
                <UiButton
                  variant="danger"
                  size="sm"
                  :loading="modsStore.removingFilename === mod.filename"
                  @click="removeMod(mod.filename, mod.display_name)"
                >
                  <Trash2 class="w-3.5 h-3.5" />
                </UiButton>
              </div>
            </div>
          </div>
        </div>

        <!-- Browse CurseForge -->
        <div v-else class="space-y-3">
          <div v-if="modsStore.error && !modsStore.loadingSearch"
            class="flex items-start gap-3 bg-tp-error/10 text-tp-error rounded-xl px-4 py-3 text-sm">
            <span class="font-medium shrink-0">CurseForge unavailable:</span>
            <span class="opacity-80">{{ modsStore.error }}</span>
          </div>
          <div class="relative">
            <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-tp-muted pointer-events-none" />
            <input
              v-model="modSearch"
              type="text"
              placeholder="Search mods on CurseForge..."
              class="w-full bg-tp-surface2 text-tp-text rounded-xl pl-10 pr-4 py-2.5 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50 transition-colors"
              @input="onModSearchInput"
            />
          </div>

          <div v-if="modsStore.loadingSearch" class="space-y-2">
            <div v-for="i in 6" :key="i" class="h-20 bg-tp-surface2 rounded-xl animate-pulse" />
          </div>
          <div v-else-if="safeSearchResults.length === 0 && modSearch.trim()"
            class="bg-tp-surface rounded-xl p-8 text-center">
            <p class="text-tp-muted text-sm">No results for "{{ modSearch }}"</p>
          </div>
          <div v-else class="bg-tp-surface rounded-xl overflow-hidden">
            <div class="px-5 py-2.5 border-b border-tp-border">
              <p class="text-tp-outline text-xs">{{ modSearch.trim() ? `${safeSearchResults.length} results` : 'Popular mods' }}</p>
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
                      :href="mod.links.websiteUrl"
                      target="_blank"
                      rel="noopener noreferrer"
                      class="text-tp-muted hover:text-tp-text transition-colors shrink-0"
                    >
                      <ExternalLink class="w-3.5 h-3.5" />
                    </a>
                  </div>
                  <p class="text-tp-muted text-xs mt-0.5 line-clamp-2">{{ mod.summary }}</p>
                  <p class="text-tp-outline text-xs mt-1">{{ mod.downloadCount.toLocaleString() }} downloads</p>
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
                  Install
                </UiButton>
                <span v-else class="text-tp-success text-xs font-medium shrink-0 mt-1">Installed</span>
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
            <h3 class="text-tp-text font-display font-semibold text-lg">Game Control</h3>
            <p class="text-tp-muted text-sm">Execute server and plugin commands with one click</p>
          </div>
          <UiButton variant="secondary" size="sm" @click="showAddCommand = !showAddCommand">
            <Plus class="w-3.5 h-3.5" />
            Custom Command
          </UiButton>
        </div>

        <!-- Server must be running notice -->
        <div v-if="server.status !== 'running'"
          class="flex items-center gap-3 bg-tp-warning/5 border border-tp-warning/20 text-tp-warning rounded-xl px-4 py-3 text-sm">
          Server must be running to execute commands.
        </div>

        <!-- Add custom command form -->
        <div v-if="showAddCommand" class="bg-tp-surface rounded-xl p-5 space-y-3">
          <h4 class="text-tp-text font-display font-semibold text-sm">Add Custom Command</h4>
          <div class="grid grid-cols-2 gap-3">
            <input v-model="newCommand.name" placeholder="Command name"
              class="bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50" />
            <input v-model="newCommand.category" placeholder="Category"
              class="bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50" />
          </div>
          <input v-model="newCommand.command_template" placeholder="Command template (e.g. give {player} {item})"
            class="w-full bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm font-mono placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50" />
          <input v-model="newCommand.description" placeholder="Description (optional)"
            class="w-full bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50" />
          <textarea v-model="newCommand.params" placeholder='Params JSON: [{"name":"player","type":"string","required":true,"placeholder":"Player name"}]'
            class="w-full bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm font-mono placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50 h-16 resize-none" />
          <div class="flex items-center gap-3">
            <label class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Min Role:</label>
            <select v-model="newCommand.min_role"
              class="bg-tp-surface2 text-tp-text rounded-xl px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-tp-primary/50">
              <option value="user">User</option>
              <option value="moderator">Moderator</option>
              <option value="admin">Admin</option>
              <option value="owner">Owner</option>
            </select>
          </div>
          <div class="flex gap-2">
            <UiButton variant="primary" size="sm" @click="addCustomCommand">Add Command</UiButton>
            <UiButton variant="secondary" size="sm" @click="showAddCommand = false">Cancel</UiButton>
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
                      >{{ cmd.min_role }}</span>
                    </div>
                    <p v-if="cmd.description" class="text-tp-muted text-xs">{{ cmd.description }}</p>

                    <!-- Insufficient permission notice -->
                    <p v-if="!userCanExecute(cmd)" class="text-tp-error text-xs mt-1.5">
                      Requires {{ cmd.min_role }} role or higher
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
                      Run
                    </UiButton>
                    <UiButton
                      v-if="!cmd.is_default"
                      variant="danger"
                      size="sm"
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
            <p class="text-tp-text font-semibold text-sm mb-1">No commands configured</p>
            <p class="text-tp-muted text-xs">Commands will be loaded when the server is created.</p>
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
                  <Save class="w-3.5 h-3.5" /> Save
                </UiButton>
                <UiButton variant="secondary" size="sm" @click="editingFile = null">
                  <X class="w-3.5 h-3.5" /> Close
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
                <ArrowUpLeft class="w-3.5 h-3.5" /> Up
              </UiButton>
              <UiButton variant="secondary" size="sm" :loading="uploadingFile" @click="triggerUpload">
                <Upload class="w-3.5 h-3.5" /> Upload
              </UiButton>
              <input ref="fileInputRef" type="file" class="hidden" @change="handleFileUpload" />
              <UiButton variant="secondary" size="sm" @click="showNewFolder = !showNewFolder">
                <FolderPlus class="w-3.5 h-3.5" /> New Folder
              </UiButton>
              <UiButton variant="secondary" size="sm" @click="fetchFiles()">
                <RotateCcw class="w-3.5 h-3.5" />
              </UiButton>
            </div>
          </div>

          <!-- New folder input -->
          <div v-if="showNewFolder" class="flex items-center gap-2">
            <input
              v-model="newFolderName"
              type="text"
              placeholder="Folder name..."
              class="flex-1 bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm placeholder:text-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50"
              @keydown.enter="createFolder"
            />
            <UiButton variant="primary" size="sm" :loading="creatingFolder" @click="createFolder">Create</UiButton>
            <UiButton variant="secondary" size="sm" @click="showNewFolder = false; newFolderName = ''">Cancel</UiButton>
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
              <span class="flex-1">Name</span>
              <span class="w-24 text-right">Size</span>
              <span class="w-44 text-right">Modified</span>
              <span class="w-32 text-right">Actions</span>
            </div>
            <div class="divide-y divide-tp-border">
              <div
                v-for="entry in fileEntries" :key="entry.name"
                class="flex items-center px-4 py-2.5 hover:bg-tp-surface2/50 transition-colors group"
              >
                <div class="flex items-center gap-2.5 flex-1 min-w-0 cursor-pointer" @click="openFile(entry)">
                  <FolderOpen v-if="entry.is_dir" class="w-4 h-4 text-tp-primary shrink-0" />
                  <FileArchive v-else-if="['jar','zip','gz','tar'].includes(entry.name.split('.').pop()?.toLowerCase() ?? '')" class="w-4 h-4 text-yellow-400 shrink-0" />
                  <FileText v-else-if="['json','toml','yaml','yml','xml','properties','cfg','conf','ini','txt','md','log','csv'].includes(entry.name.split('.').pop()?.toLowerCase() ?? '')" class="w-4 h-4 text-blue-400 shrink-0" />
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
                    title="Download"
                    @click.stop="downloadFile(entry)"
                  >
                    <Download class="w-3.5 h-3.5" />
                  </button>
                  <!-- Extract (zip files only) -->
                  <button
                    v-if="!entry.is_dir && entry.name.toLowerCase().endsWith('.zip')"
                    class="p-1 text-tp-muted hover:text-yellow-400 transition-colors opacity-0 group-hover:opacity-100"
                    :disabled="extractingPath === entry.name"
                    title="Extract"
                    @click.stop="extractFile(entry)"
                  >
                    <PackageOpen class="w-3.5 h-3.5" />
                  </button>
                  <!-- Archive (directories only) -->
                  <button
                    v-if="entry.is_dir"
                    class="p-1 text-tp-muted hover:text-yellow-400 transition-colors opacity-0 group-hover:opacity-100"
                    :disabled="archivingPath === entry.name"
                    title="Archive as .zip"
                    @click.stop="archiveEntry(entry)"
                  >
                    <FileArchive class="w-3.5 h-3.5" />
                  </button>
                  <!-- Delete -->
                  <button
                    class="p-1 text-tp-muted hover:text-tp-danger transition-colors opacity-0 group-hover:opacity-100"
                    :disabled="deletingPath === entry.name"
                    title="Delete"
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
            <p class="text-tp-text font-semibold text-sm mb-1">Empty directory</p>
            <p class="text-tp-muted text-xs">This folder has no files yet.</p>
          </div>
        </template>
      </div>

      <!-- ── Worlds tab ──────────────────────────────────────────────────── -->
      <div v-else-if="activeTab === 'worlds'" class="space-y-4">
        <div v-if="worldsStore.loading" class="bg-tp-surface rounded-xl p-8 text-center">
          <Loader2 class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
          <p class="text-tp-muted text-sm">Loading worlds...</p>
        </div>
        <div v-else-if="worldsStore.worlds.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
          <Globe class="w-8 h-8 text-tp-muted mx-auto mb-3" />
          <p class="text-tp-text font-display font-semibold text-sm mb-1">No worlds</p>
          <p class="text-tp-muted text-xs">Worlds will appear here when created.</p>
        </div>
        <div v-else class="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div v-for="w in worldsStore.worlds" :key="w.id"
            :class="['bg-tp-surface rounded-xl p-4 flex items-center justify-between', w.is_active ? 'ring-1 ring-tp-success/40' : '']">
            <div class="flex items-center gap-3">
              <Globe :class="['w-5 h-5', w.is_active ? 'text-tp-success' : 'text-tp-muted']" />
              <div>
                <p class="text-tp-text text-sm font-medium">{{ w.name }}</p>
                <p class="text-tp-muted text-xs">{{ w.is_active ? 'Active' : 'Inactive' }}</p>
              </div>
            </div>
            <div class="flex items-center gap-1">
              <button v-if="!w.is_active" class="p-1.5 rounded-xl text-tp-muted hover:text-tp-success hover:bg-tp-success/10 transition-colors text-xs" title="Set active"
                @click="worldsStore.setActive(serverId, w.id).then(() => showToast('Active world updated')).catch(() => showToast('Failed', 'error'))">
                Activate
              </button>
              <button class="p-1.5 rounded-xl text-tp-danger hover:bg-tp-danger/10 transition-colors" title="Delete"
                @click="confirm('Delete this world?') && worldsStore.deleteWorld(serverId, w.id).then(() => showToast('World deleted')).catch(() => showToast('Failed', 'error'))">
                <Trash2 class="w-3.5 h-3.5" />
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- ── Players tab ─────────────────────────────────────────────────── -->
      <div v-else-if="activeTab === 'players'" class="space-y-4">
        <div v-if="playersStore.loading" class="bg-tp-surface rounded-xl p-8 text-center">
          <Loader2 class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
          <p class="text-tp-muted text-sm">Loading players...</p>
        </div>
        <div v-else-if="playersStore.players.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
          <Users class="w-8 h-8 text-tp-muted mx-auto mb-3" />
          <p class="text-tp-text font-display font-semibold text-sm mb-1">No players</p>
          <p class="text-tp-muted text-xs">Players will appear here when they join.</p>
        </div>
        <div v-else class="bg-tp-surface rounded-xl overflow-hidden">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-tp-border">
                <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Player</th>
                <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Status</th>
                <th class="text-right px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="p in playersStore.players" :key="p.id" class="border-b border-tp-border/50 hover:bg-tp-surface2/50">
                <td class="px-4 py-3">
                  <p class="text-tp-text font-medium">{{ p.username }}</p>
                  <p class="text-tp-muted text-xs font-mono">{{ p.hytale_uuid.substring(0, 8) }}...</p>
                </td>
                <td class="px-4 py-3">
                  <span v-if="p.is_banned" class="text-xs px-2 py-0.5 rounded-full bg-tp-error/10 text-tp-error">Banned</span>
                  <span v-if="p.is_whitelisted" class="text-xs px-2 py-0.5 rounded-full bg-tp-success/15 text-tp-success ml-1">Whitelisted</span>
                </td>
                <td class="px-4 py-3 text-right">
                  <button v-if="!p.is_banned" class="text-xs text-tp-danger hover:text-tp-danger/80 transition-colors" @click="playersStore.banPlayer(serverId, p.id, '').then(() => showToast('Player banned'))">Ban</button>
                  <button v-else class="text-xs text-tp-success hover:text-tp-success/80 transition-colors" @click="playersStore.unbanPlayer(serverId, p.id).then(() => showToast('Player unbanned'))">Unban</button>
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
            @click="backupsStore.createBackup({ server_id: serverId }).then(() => showToast('Backup created')).catch(() => showToast('Failed', 'error'))">
            <Plus class="w-3.5 h-3.5" /> Create Backup
          </button>
        </div>
        <div v-if="backupsStore.loading" class="bg-tp-surface rounded-xl p-8 text-center">
          <Loader2 class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
          <p class="text-tp-muted text-sm">Loading backups...</p>
        </div>
        <div v-else-if="backupsStore.backups.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
          <DatabaseBackup class="w-8 h-8 text-tp-muted mx-auto mb-3" />
          <p class="text-tp-text font-display font-semibold text-sm mb-1">No backups</p>
          <p class="text-tp-muted text-xs">Create a backup to protect your server data.</p>
        </div>
        <div v-else class="space-y-2">
          <div v-for="b in backupsStore.backups" :key="b.id" class="bg-tp-surface rounded-xl p-4 flex items-center justify-between">
            <div class="flex items-center gap-3">
              <DatabaseBackup class="w-5 h-5 text-tp-muted" />
              <div>
                <p class="text-tp-text text-sm font-medium capitalize">{{ b.type }} backup</p>
                <p class="text-tp-muted text-xs">{{ new Date(b.created_at).toLocaleString() }} — {{ b.status }}</p>
              </div>
            </div>
            <div class="flex items-center gap-1">
              <button v-if="b.status === 'complete'" class="text-xs text-tp-accent hover:text-tp-primary transition-colors" @click="backupsStore.restoreBackup(b.id).then(() => showToast('Restore initiated'))">Restore</button>
              <button class="p-1.5 rounded-xl text-tp-danger hover:bg-tp-danger/10 transition-colors" @click="confirm('Delete backup?') && backupsStore.deleteBackup(b.id).then(() => showToast('Deleted'))">
                <Trash2 class="w-3.5 h-3.5" />
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- ── Databases tab ──────────────────────────────────────────────── -->
      <div v-else-if="activeTab === 'databases'" class="space-y-4">
        <div class="flex items-center justify-between">
          <div>
            <h3 class="text-tp-text font-display font-semibold text-lg">Databases</h3>
            <p class="text-tp-muted text-sm">Manage databases for this server's plugins and mods</p>
          </div>
          <UiButton variant="primary" size="sm" @click="showCreateDbModal = true">
            <Plus class="w-3.5 h-3.5" /> Create Database
          </UiButton>
        </div>

        <div v-if="dbLoading" class="bg-tp-surface rounded-xl p-8 text-center">
          <Loader2 class="w-6 h-6 text-tp-muted animate-spin mx-auto mb-2" />
          <p class="text-tp-muted text-sm">Loading databases...</p>
        </div>
        <div v-else-if="databases.length === 0" class="bg-tp-surface rounded-xl p-12 text-center">
          <Database class="w-8 h-8 text-tp-muted mx-auto mb-3" />
          <p class="text-tp-text font-display font-semibold text-sm mb-1">No databases</p>
          <p class="text-tp-muted text-xs">Create a database for your server's plugins to use.</p>
        </div>
        <div v-else class="space-y-3">
          <div v-for="db in databases" :key="db.name" class="bg-tp-surface rounded-xl p-5">
            <div class="flex items-start justify-between">
              <div class="flex items-center gap-3">
                <div class="w-9 h-9 rounded-xl bg-tp-primary/10 flex items-center justify-center">
                  <Database class="w-4 h-4 text-tp-primary" />
                </div>
                <div>
                  <p class="text-tp-text font-semibold text-sm">{{ db.name }}</p>
                  <p class="text-tp-muted text-xs font-mono">{{ db.type }} | {{ db.host }}:{{ db.port }}</p>
                </div>
              </div>
              <div class="flex items-center gap-2">
                <span :class="['text-xs font-medium px-2 py-0.5 rounded-full', db.status === 'active' ? 'text-tp-success bg-tp-success/15' : 'text-tp-muted bg-tp-surface2']">
                  {{ db.status }}
                </span>
                <button class="p-1.5 rounded-xl text-tp-warning hover:bg-tp-warning/10 transition-colors"
                  :disabled="rotatingDb === db.name"
                  :title="'Rotate password'"
                  @click="rotateDatabasePassword(db.name)">
                  <RotateCcw class="w-3.5 h-3.5" />
                </button>
                <button class="p-1.5 rounded-xl text-tp-danger hover:bg-tp-danger/10 transition-colors"
                  @click="confirm('Delete database ' + db.name + '?') && deleteDatabase(db.name)">
                  <Trash2 class="w-3.5 h-3.5" />
                </button>
              </div>
            </div>
            <div class="grid grid-cols-3 gap-2 mt-3">
              <div class="bg-tp-surface2 rounded-xl p-2 text-xs">
                <span class="text-tp-outline">User:</span>
                <span class="text-tp-text ml-1 font-mono">{{ db.username }}</span>
              </div>
              <div class="bg-tp-surface2 rounded-xl p-2 text-xs">
                <span class="text-tp-outline">Size:</span>
                <span class="text-tp-text ml-1">{{ db.size ?? '\u2014' }}</span>
              </div>
              <div class="bg-tp-surface2 rounded-xl p-2 text-xs">
                <span class="text-tp-outline">Connections:</span>
                <span class="text-tp-text ml-1">{{ db.connections ?? 0 }}</span>
              </div>
            </div>
          </div>
        </div>

        <!-- Rotated credentials modal — shown once, caller must copy -->
        <UiModal :open="!!rotatedCreds" title="New database credentials" @close="rotatedCreds = null">
          <div v-if="rotatedCreds" class="space-y-3">
            <p class="text-tp-muted text-xs">Copy these now — the password will not be shown again.</p>
            <div class="space-y-1.5">
              <div v-for="(val, label) in {
                Host: rotatedCreds.host,
                Port: String(rotatedCreds.port),
                Database: rotatedCreds.database,
                Username: rotatedCreds.username,
                Password: rotatedCreds.password,
              }" :key="label" class="flex items-center gap-2 bg-tp-surface2 rounded-xl px-3 py-2 text-xs">
                <span class="text-tp-outline w-20 shrink-0">{{ label }}</span>
                <code class="text-tp-text font-mono flex-1 break-all">{{ val }}</code>
              </div>
            </div>
            <div class="flex justify-end pt-2">
              <UiButton variant="primary" size="md" @click="rotatedCreds = null">Done</UiButton>
            </div>
          </div>
        </UiModal>

        <!-- Create DB Modal -->
        <UiModal :open="showCreateDbModal" title="Create Database" @close="showCreateDbModal = false">
          <div class="space-y-4">
            <UiInput v-model="newDbForm.name" label="Database Name" placeholder="my_plugin_db" />
            <div>
              <label class="block text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-1">Type</label>
              <select v-model="newDbForm.type" class="w-full bg-tp-surface2 border border-tp-border rounded-xl px-3 py-2 text-sm text-tp-text">
                <option value="mysql">MySQL</option>
                <option value="postgres">PostgreSQL</option>
                <option value="sqlite">SQLite</option>
              </select>
            </div>
            <div class="flex justify-end gap-2 pt-2">
              <UiButton variant="secondary" size="md" @click="showCreateDbModal = false">Cancel</UiButton>
              <UiButton variant="primary" size="md" :disabled="!newDbForm.name" @click="createDatabase">Create</UiButton>
            </div>
          </div>
        </UiModal>
      </div>

      <!-- ── Settings tab ────────────────────────────────────────────────── -->
      <div v-else-if="activeTab === 'settings'" class="space-y-4">
        <div class="bg-tp-surface rounded-xl overflow-hidden">
          <div class="px-5 py-3 border-b border-tp-border">
            <h3 class="text-tp-text font-display font-semibold text-sm">Server Settings</h3>
          </div>
          <div class="divide-y divide-tp-border">
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">Name</span>
              <span class="text-tp-text text-sm font-medium">{{ server?.name }}</span>
            </div>
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">Version</span>
              <span class="text-tp-text text-sm">{{ server?.hytale_version }}</span>
            </div>
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">Port</span>
              <span class="text-tp-text text-sm font-mono">{{ server?.port }}</span>
            </div>
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">RAM Limit</span>
              <span class="text-tp-text text-sm">{{ server?.ram_limit_mb ?? '\u2014' }} MB</span>
            </div>
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">CPU Limit</span>
              <span class="text-tp-text text-sm">{{ server?.cpu_limit ?? '\u2014' }} cores</span>
            </div>
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">Auto-Restart</span>
              <span :class="['text-sm font-medium', server?.auto_restart ? 'text-tp-success' : 'text-tp-muted']">{{ server?.auto_restart ? 'Enabled' : 'Disabled' }}</span>
            </div>
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">Active World</span>
              <span class="text-tp-text text-sm">{{ server?.active_world || '\u2014' }}</span>
            </div>
            <div class="flex items-center justify-between px-5 py-3">
              <span class="text-tp-outline text-sm">Created</span>
              <span class="text-tp-muted text-xs">{{ server?.created_at ? new Date(server.created_at).toLocaleString() : '\u2014' }}</span>
            </div>
          </div>
        </div>
        <div class="bg-tp-surface rounded-xl border border-tp-danger/30 p-5">
          <h3 class="text-tp-danger font-display font-semibold text-sm mb-2">Danger Zone</h3>
          <p class="text-tp-muted text-xs mb-3">Permanently delete this server and all associated data.</p>
          <button class="px-4 py-2 rounded-xl bg-tp-danger text-white text-xs font-medium hover:bg-tp-danger/90 transition-colors"
            @click="confirm('Delete this server? This cannot be undone.') && serversStore.deleteServer(serverId).then(() => navigateTo('/servers'))">
            Delete Server
          </button>
        </div>
      </div>
    </template>

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
  background: linear-gradient(90deg, #3b82f6, #89ceff);
}
</style>
