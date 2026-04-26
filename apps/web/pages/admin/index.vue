<script setup lang="ts">
import {
  Users, Network, Shield, RefreshCw, Trash2, UserCheck, UserX,
  ChevronDown, Power, PowerOff, Pause, Activity, Clock, Search,
  Plug, Save, ExternalLink,
} from 'lucide-vue-next'
import { useApi } from '~/composables/useApi'
import { useAuthStore } from '~/stores/auth'

definePageMeta({ title: 'Admin Panel', middleware: 'auth' })

const api = useApi()
const authStore = useAuthStore()

// ── Tabs ─────────────────────────────────────────────────────────────────────
const activeTab = ref<'users' | 'nodes' | 'logs' | 'integrations'>('users')

// ── Integrations ─────────────────────────────────────────────────────────────
interface CurseForgeStatus { configured: boolean; preview: string }
const cfStatus = ref<CurseForgeStatus>({ configured: false, preview: '' })
const cfKeyInput = ref('')
const cfSaving = ref(false)
const cfLoading = ref(false)

async function fetchCurseForgeStatus() {
  cfLoading.value = true
  try {
    cfStatus.value = await api.get<CurseForgeStatus>('/admin/integrations/curseforge')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to load CurseForge status', 'error')
  } finally {
    cfLoading.value = false
  }
}

async function saveCurseForgeKey() {
  cfSaving.value = true
  try {
    await api.put('/admin/integrations/curseforge', { api_key: cfKeyInput.value })
    cfKeyInput.value = ''
    await fetchCurseForgeStatus()
    showToast('CurseForge API key saved')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to save key', 'error')
  } finally {
    cfSaving.value = false
  }
}

function onTabChange(key: 'users' | 'nodes' | 'logs' | 'integrations') {
  activeTab.value = key
  if (key === 'integrations') fetchCurseForgeStatus()
}

async function clearCurseForgeKey() {
  if (!confirm('Clear the stored CurseForge API key? Mod browsing will stop working until a new key is provided.')) return
  cfSaving.value = true
  try {
    await api.put('/admin/integrations/curseforge', { api_key: '' })
    await fetchCurseForgeStatus()
    showToast('CurseForge API key cleared')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to clear key', 'error')
  } finally {
    cfSaving.value = false
  }
}

// ── Toast ────────────────────────────────────────────────────────────────────
const toast = ref('')
const toastType = ref<'success' | 'error'>('success')
let toastTimer: ReturnType<typeof setTimeout>
function showToast(msg: string, type: 'success' | 'error' = 'success') {
  toast.value = msg
  toastType.value = type
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toast.value = '' }, 3000)
}

// ── Users ────────────────────────────────────────────────────────────────────
interface User {
  id: string
  email: string
  username: string
  role: string
  totp_enabled: boolean
  is_active: boolean
  created_at: string
  last_login_at?: string
}

const users = ref<User[]>([])
const usersLoading = ref(false)
const usersError = ref('')
const userSearch = ref('')

const filteredUsers = computed(() => {
  if (!userSearch.value) return users.value
  const q = userSearch.value.toLowerCase()
  return users.value.filter(u =>
    u.username.toLowerCase().includes(q) ||
    u.email.toLowerCase().includes(q) ||
    u.role.toLowerCase().includes(q)
  )
})

async function fetchUsers() {
  usersLoading.value = true
  usersError.value = ''
  try {
    const data = await api.get<{ users: User[] }>('/admin/users')
    users.value = data.users ?? []
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    usersError.value = e.data?.error ?? e.message ?? 'Failed to fetch users'
  } finally {
    usersLoading.value = false
  }
}

const roles = ['user', 'moderator', 'admin', 'owner']

async function changeRole(user: User, newRole: string) {
  try {
    await api.patch(`/admin/users/${user.id}/role`, { role: newRole })
    user.role = newRole
    showToast(`Role updated to ${newRole}`)
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to update role', 'error')
  }
}

async function toggleActive(user: User) {
  const newActive = !user.is_active
  try {
    await api.patch(`/admin/users/${user.id}/active`, { is_active: newActive })
    user.is_active = newActive
    showToast(newActive ? 'User activated' : 'User deactivated')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to toggle user', 'error')
  }
}

async function deleteUser(user: User) {
  if (!confirm(`Delete user "${user.username}"? This cannot be undone.`)) return
  try {
    await api.delete(`/admin/users/${user.id}`)
    users.value = users.value.filter(u => u.id !== user.id)
    showToast('User deleted')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to delete user', 'error')
  }
}

// ── Permissions editor ────────────────────────────────────────────────────────
// 16 permission keys mirror the `role_permissions` seed in migration 008.
// Listed here so the modal renders even if the DB has never been queried;
// individual per-user overrides are layered on top via GET/PUT on
// /admin/users/:id/permissions.
const ALL_PERMISSIONS: { key: string; group: string; label: string }[] = [
  { key: 'admin.nodes',       group: 'Admin',    label: 'Manage nodes' },
  { key: 'admin.users',       group: 'Admin',    label: 'Manage users' },
  { key: 'server.create',     group: 'Servers',  label: 'Create servers' },
  { key: 'server.delete',     group: 'Servers',  label: 'Delete servers' },
  { key: 'server.start',      group: 'Servers',  label: 'Start servers' },
  { key: 'server.stop',       group: 'Servers',  label: 'Stop servers' },
  { key: 'server.console',    group: 'Servers',  label: 'Send console commands' },
  { key: 'server.files',      group: 'Servers',  label: 'Access file browser' },
  { key: 'backup.create',     group: 'Backups',  label: 'Create backups' },
  { key: 'backup.restore',    group: 'Backups',  label: 'Restore backups' },
  { key: 'database.view',     group: 'Database', label: 'View server databases' },
  { key: 'database.reset',    group: 'Database', label: 'Rotate database password' },
  { key: 'mod.install',       group: 'Mods',     label: 'Install mods' },
  { key: 'mod.remove',        group: 'Mods',     label: 'Remove mods' },
  { key: 'player.ban',        group: 'Players',  label: 'Ban / unban players' },
  { key: 'player.whitelist',  group: 'Players',  label: 'Manage whitelist' },
]

interface UserPermission { perm_key: string; granted: boolean }

const permUser = ref<User | null>(null)
const permOverrides = ref<Record<string, boolean>>({})  // null-ish = inherit from role
const permLoading = ref(false)
const permSaving = ref(false)

async function openPermissions(user: User) {
  permUser.value = user
  permOverrides.value = {}
  permLoading.value = true
  try {
    const res = await api.get<{ permissions: UserPermission[] }>(`/admin/users/${user.id}/permissions`)
    for (const p of res.permissions ?? []) {
      permOverrides.value[p.perm_key] = p.granted
    }
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? 'Failed to load permissions', 'error')
  } finally {
    permLoading.value = false
  }
}

function closePermissions() {
  permUser.value = null
  permOverrides.value = {}
}

function isOverridden(key: string): boolean {
  return Object.prototype.hasOwnProperty.call(permOverrides.value, key)
}

function permState(key: string): 'grant' | 'deny' | 'inherit' {
  if (!isOverridden(key)) return 'inherit'
  return permOverrides.value[key] ? 'grant' : 'deny'
}

function setPermState(key: string, state: 'grant' | 'deny' | 'inherit') {
  if (state === 'inherit') {
    delete permOverrides.value[key]
  } else {
    permOverrides.value[key] = state === 'grant'
  }
}

async function savePermissions() {
  if (!permUser.value) return
  permSaving.value = true
  try {
    const permissions = Object.entries(permOverrides.value).map(([perm_key, granted]) => ({ perm_key, granted }))
    await api.put(`/admin/users/${permUser.value.id}/permissions`, { permissions })
    showToast('Permissions saved')
    closePermissions()
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    showToast(e.data?.error ?? 'Failed to save permissions', 'error')
  } finally {
    permSaving.value = false
  }
}

const permGroups = computed(() => {
  const byGroup: Record<string, typeof ALL_PERMISSIONS> = {}
  for (const p of ALL_PERMISSIONS) {
    byGroup[p.group] = byGroup[p.group] ?? []
    byGroup[p.group].push(p)
  }
  return byGroup
})

// ── Nodes ────────────────────────────────────────────────────────────────────
interface Node {
  id: string
  name: string
  fqdn: string
  port: number
  location?: string
  total_cpu: number
  total_ram_mb: number
  total_disk_mb: number
  max_servers: number
  status: string
  last_heartbeat?: string
  created_at: string
}

const nodes = ref<Node[]>([])
const nodesLoading = ref(false)
const nodesError = ref('')

async function fetchNodes() {
  nodesLoading.value = true
  nodesError.value = ''
  try {
    const data = await api.get<{ nodes: Node[] }>('/nodes')
    nodes.value = data.nodes ?? []
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    nodesError.value = e.data?.error ?? e.message ?? 'Failed to fetch nodes'
  } finally {
    nodesLoading.value = false
  }
}

async function setNodeStatus(node: Node, status: string) {
  try {
    await api.patch(`/admin/nodes/${node.id}/status`, { status })
    node.status = status
    showToast(`Node set to ${status}`)
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    showToast(e.data?.error ?? 'Failed to update node', 'error')
  }
}

// ── Activity Logs ────────────────────────────────────────────────────────────
interface ActivityLog {
  id: string
  user_id?: string
  server_id?: string
  action: string
  target_type: string
  target_id?: string
  ip_address: string
  payload?: unknown
  created_at: string
}

const activityLogs = ref<ActivityLog[]>([])
const logsLoading = ref(false)

async function fetchLogs() {
  logsLoading.value = true
  try {
    const data = await api.get<{ logs: ActivityLog[] }>('/admin/activity-logs')
    activityLogs.value = data.logs ?? []
  } catch {
    // silent
  } finally {
    logsLoading.value = false
  }
}

// ── Lifecycle ────────────────────────────────────────────────────────────────
onMounted(() => {
  fetchUsers()
  fetchNodes()
  fetchLogs()
})

// ── Helpers ──────────────────────────────────────────────────────────────────
function statusColor(status: string) {
  switch (status) {
    case 'online': return 'text-tp-success bg-tp-success/15'
    case 'offline': return 'text-tp-muted bg-tp-surface2'
    case 'draining': return 'text-tp-warning bg-tp-warning/15'
    default: return 'text-tp-muted bg-tp-surface2'
  }
}

function roleColor(role: string) {
  switch (role) {
    case 'owner': return 'text-amber-400 bg-amber-400/10 border-amber-400/20'
    case 'admin': return 'text-tp-primary bg-tp-primary/10 border-tp-primary/20'
    case 'moderator': return 'text-purple-400 bg-purple-400/10 border-purple-400/20'
    default: return 'text-tp-muted bg-tp-surface2 border-tp-border'
  }
}

function formatMB(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${mb} MB`
}

function timeAgo(iso?: string): string {
  if (!iso) return 'Never'
  const diff = Date.now() - new Date(iso).getTime()
  const s = Math.floor(diff / 1000)
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString()
}

const isOwner = computed(() => authStore.user?.role === 'owner')
const isSelf = (id: string) => authStore.user?.id === id

// ── Create User Modal ────────────────────────────────────────────────────────
const showCreateUser = ref(false)
const createUserLoading = ref(false)
const createUserError = ref('')

const createUserForm = reactive({
  username: '',
  email: '',
  password: '',
  role: 'user',
})

function openCreateUser() {
  createUserForm.username = ''
  createUserForm.email = ''
  createUserForm.password = ''
  createUserForm.role = 'user'
  createUserError.value = ''
  showCreateUser.value = true
}

async function submitCreateUser() {
  if (!createUserForm.username || !createUserForm.email || !createUserForm.password) return
  createUserLoading.value = true
  createUserError.value = ''
  try {
    const data = await api.post<{ user: User }>('/admin/users', {
      username: createUserForm.username,
      email: createUserForm.email,
      password: createUserForm.password,
      role: createUserForm.role,
    })
    if (data.user) {
      users.value.push(data.user)
    }
    showCreateUser.value = false
    showToast('User created')
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    createUserError.value = e.data?.error ?? e.message ?? 'Failed to create user'
  } finally {
    createUserLoading.value = false
  }
}
</script>

<template>
  <div class="p-6 space-y-5">
    <!-- Header -->
    <div class="flex items-center gap-3">
      <Shield class="w-6 h-6 text-tp-primary" />
      <h2 class="text-tp-text font-display font-bold text-2xl">Admin Panel</h2>
    </div>

    <!-- Tabs -->
    <div class="flex gap-1 bg-tp-surface rounded-xl p-1 w-fit">
      <button
        v-for="tab in [
          { key: 'users', label: 'Users', icon: Users, count: users.length },
          { key: 'nodes', label: 'Nodes', icon: Network, count: nodes.length },
          { key: 'logs', label: 'Activity Logs', icon: Activity, count: activityLogs.length },
          { key: 'integrations', label: 'Integrations', icon: Plug, count: 0 },
        ]"
        :key="tab.key"
        :class="[
          'flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-all',
          activeTab === tab.key
            ? 'bg-tp-primary text-white shadow-sm'
            : 'text-tp-muted hover:text-tp-text hover:bg-tp-surface2',
        ]"
        @click="onTabChange(tab.key as 'users' | 'nodes' | 'logs' | 'integrations')"
      >
        <component :is="tab.icon" class="w-4 h-4" />
        {{ tab.label }}
        <span class="bg-white/20 text-xs px-1.5 py-0.5 rounded-full" v-if="activeTab === tab.key">
          {{ tab.count }}
        </span>
      </button>
    </div>

    <!-- ═══════════════════════════════════════════════════════════════════════
         USERS TAB
         ═══════════════════════════════════════════════════════════════════════ -->
    <div v-if="activeTab === 'users'" class="space-y-4">
      <!-- Search + refresh -->
      <div class="flex items-center gap-3">
        <div class="relative flex-1 max-w-sm">
          <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-tp-muted" />
          <input
            v-model="userSearch"
            type="text"
            placeholder="Search users..."
            class="w-full bg-tp-surface rounded-xl pl-9 pr-4 py-2 text-sm text-tp-text placeholder-tp-muted focus:outline-none focus:ring-2 focus:ring-tp-primary/50"
          />
        </div>
        <UiButton variant="secondary" size="sm" :loading="usersLoading" @click="fetchUsers">
          <RefreshCw class="w-3.5 h-3.5" />
          Refresh
        </UiButton>
        <UiButton variant="primary" size="sm" @click="openCreateUser">
          <span class="material-symbols-outlined text-base">person_add</span>
          Create User
        </UiButton>
      </div>

      <div v-if="usersError" class="bg-tp-error/10 rounded-xl px-4 py-3 text-tp-error text-sm">
        {{ usersError }}
      </div>

      <!-- Users table -->
      <div class="bg-tp-surface rounded-xl overflow-hidden">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-tp-border">
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">User</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Role</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Status</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Last Login</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Created</th>
              <th class="text-right px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="user in filteredUsers" :key="user.id" class="border-b border-tp-border/50 hover:bg-tp-surface2/50 transition-colors">
              <td class="px-4 py-3">
                <div class="flex items-center gap-3">
                  <div class="w-8 h-8 rounded-full bg-tp-primary flex items-center justify-center text-white text-xs font-semibold uppercase shrink-0">
                    {{ user.username.charAt(0) }}
                  </div>
                  <div>
                    <p class="text-tp-text font-medium">{{ user.username }}</p>
                    <p class="text-tp-muted text-xs">{{ user.email }}</p>
                  </div>
                </div>
              </td>
              <td class="px-4 py-3">
                <div class="relative inline-block">
                  <select
                    :value="user.role"
                    :disabled="isSelf(user.id)"
                    @change="changeRole(user, ($event.target as HTMLSelectElement).value)"
                    :class="[
                      'appearance-none cursor-pointer text-xs font-medium px-3 py-1 pr-7 rounded-full border',
                      roleColor(user.role),
                      isSelf(user.id) ? 'opacity-50 cursor-not-allowed' : '',
                    ]"
                  >
                    <option v-for="r in roles" :key="r" :value="r" :disabled="r === 'owner' && !isOwner">
                      {{ r }}
                    </option>
                  </select>
                  <ChevronDown class="absolute right-2 top-1/2 -translate-y-1/2 w-3 h-3 pointer-events-none" />
                </div>
              </td>
              <td class="px-4 py-3">
                <span :class="[
                  'text-xs font-medium px-2 py-0.5 rounded-full',
                  user.is_active ? 'text-tp-success bg-tp-success/15' : 'text-tp-error bg-tp-error/10',
                ]">
                  {{ user.is_active ? 'Active' : 'Disabled' }}
                </span>
              </td>
              <td class="px-4 py-3 text-tp-muted text-xs">{{ timeAgo(user.last_login_at) }}</td>
              <td class="px-4 py-3 text-tp-muted text-xs">{{ formatDate(user.created_at) }}</td>
              <td class="px-4 py-3">
                <div class="flex items-center justify-end gap-1">
                  <button
                    title="Edit permissions"
                    class="p-1.5 rounded-lg text-tp-primary hover:bg-tp-primary/10 transition-colors"
                    @click="openPermissions(user)"
                  >
                    <Shield class="w-4 h-4" />
                  </button>
                  <button
                    v-if="!isSelf(user.id)"
                    :title="user.is_active ? 'Deactivate' : 'Activate'"
                    class="p-1.5 rounded-lg hover:bg-tp-surface2 transition-colors"
                    :class="user.is_active ? 'text-tp-warning' : 'text-tp-success'"
                    @click="toggleActive(user)"
                  >
                    <UserX v-if="user.is_active" class="w-4 h-4" />
                    <UserCheck v-else class="w-4 h-4" />
                  </button>
                  <button
                    v-if="!isSelf(user.id)"
                    title="Delete user"
                    class="p-1.5 rounded-lg text-tp-danger hover:bg-tp-danger/10 transition-colors"
                    @click="deleteUser(user)"
                  >
                    <Trash2 class="w-4 h-4" />
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="filteredUsers.length === 0 && !usersLoading" class="p-8 text-center text-tp-muted text-sm">
          No users found
        </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════════════════════════════════
         NODES TAB
         ═══════════════════════════════════════════════════════════════════════ -->
    <div v-else-if="activeTab === 'nodes'" class="space-y-4">
      <div class="flex items-center justify-end">
        <UiButton variant="secondary" size="sm" :loading="nodesLoading" @click="fetchNodes">
          <RefreshCw class="w-3.5 h-3.5" />
          Refresh
        </UiButton>
      </div>

      <div v-if="nodesError" class="bg-tp-error/10 rounded-xl px-4 py-3 text-tp-error text-sm">
        {{ nodesError }}
      </div>

      <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <div v-for="node in nodes" :key="node.id"
          class="bg-tp-surface rounded-xl p-5 space-y-4">
          <!-- Header -->
          <div class="flex items-start justify-between gap-2">
            <div class="min-w-0">
              <p class="text-tp-text font-semibold truncate">{{ node.name }}</p>
              <p class="text-tp-muted text-xs font-mono mt-0.5 truncate">{{ node.fqdn }}:{{ node.port }}</p>
            </div>
            <span :class="['text-xs font-medium px-2 py-0.5 rounded-full shrink-0', statusColor(node.status)]">
              {{ node.status }}
            </span>
          </div>

          <!-- Specs -->
          <div class="grid grid-cols-3 gap-2">
            <div class="bg-tp-surface2 rounded-xl p-2 text-center">
              <p class="text-tp-text font-semibold text-sm">{{ node.total_cpu || '-' }}</p>
              <p class="text-tp-outline text-xs">CPU</p>
            </div>
            <div class="bg-tp-surface2 rounded-xl p-2 text-center">
              <p class="text-tp-text font-semibold text-sm">{{ node.total_ram_mb ? formatMB(node.total_ram_mb) : '-' }}</p>
              <p class="text-tp-outline text-xs">RAM</p>
            </div>
            <div class="bg-tp-surface2 rounded-xl p-2 text-center">
              <p class="text-tp-text font-semibold text-sm">{{ node.total_disk_mb ? formatMB(node.total_disk_mb) : '-' }}</p>
              <p class="text-tp-outline text-xs">Disk</p>
            </div>
          </div>

          <!-- Last heartbeat + location -->
          <div class="flex items-center justify-between text-xs text-tp-muted">
            <span class="flex items-center gap-1">
              <Clock class="w-3 h-3" />
              {{ timeAgo(node.last_heartbeat) }}
            </span>
            <span>{{ node.location || 'No location' }}</span>
          </div>

          <!-- Status controls -->
          <div class="flex gap-2 pt-1 border-t border-tp-border">
            <button
              class="flex-1 flex items-center justify-center gap-1.5 py-2 rounded-xl text-xs font-medium transition-colors"
              :class="node.status === 'online' ? 'bg-tp-success/20 text-tp-success' : 'bg-tp-surface2 text-tp-muted hover:bg-tp-success/10 hover:text-tp-success'"
              @click="setNodeStatus(node, 'online')"
            >
              <Power class="w-3.5 h-3.5" />
              Activate
            </button>
            <button
              class="flex-1 flex items-center justify-center gap-1.5 py-2 rounded-xl text-xs font-medium transition-colors"
              :class="node.status === 'draining' ? 'bg-tp-warning/20 text-tp-warning' : 'bg-tp-surface2 text-tp-muted hover:bg-tp-warning/10 hover:text-tp-warning'"
              @click="setNodeStatus(node, 'draining')"
            >
              <Pause class="w-3.5 h-3.5" />
              Drain
            </button>
            <button
              class="flex-1 flex items-center justify-center gap-1.5 py-2 rounded-xl text-xs font-medium transition-colors"
              :class="node.status === 'offline' ? 'bg-tp-danger/20 text-tp-danger' : 'bg-tp-surface2 text-tp-muted hover:bg-tp-danger/10 hover:text-tp-danger'"
              @click="setNodeStatus(node, 'offline')"
            >
              <PowerOff class="w-3.5 h-3.5" />
              Deactivate
            </button>
          </div>
        </div>
      </div>

      <div v-if="nodes.length === 0 && !nodesLoading" class="bg-tp-surface rounded-xl p-12 text-center">
        <Network class="w-10 h-10 text-tp-muted mx-auto mb-3" />
        <p class="text-tp-muted text-sm">No nodes registered</p>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════════════════════════════════
         ACTIVITY LOGS TAB
         ═══════════════════════════════════════════════════════════════════════ -->
    <div v-else-if="activeTab === 'logs'" class="space-y-4">
      <div class="flex items-center justify-end">
        <UiButton variant="secondary" size="sm" :loading="logsLoading" @click="fetchLogs">
          <RefreshCw class="w-3.5 h-3.5" />
          Refresh
        </UiButton>
      </div>

      <div class="bg-tp-surface rounded-xl overflow-hidden">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-tp-border">
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Time</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Action</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">Target</th>
              <th class="text-left px-4 py-3 text-[10px] uppercase tracking-widest font-semibold text-tp-outline">IP</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="log in activityLogs" :key="log.id" class="border-b border-tp-border/50 hover:bg-tp-surface2/50 transition-colors">
              <td class="px-4 py-3 text-tp-muted text-xs whitespace-nowrap">{{ formatDate(log.created_at) }}</td>
              <td class="px-4 py-3">
                <span class="text-tp-text font-medium">{{ log.action }}</span>
              </td>
              <td class="px-4 py-3 text-tp-muted text-xs">
                <span v-if="log.target_type">{{ log.target_type }}</span>
                <span v-if="log.target_id" class="font-mono ml-1">{{ log.target_id?.substring(0, 8) }}...</span>
              </td>
              <td class="px-4 py-3 text-tp-muted text-xs font-mono">{{ log.ip_address || '-' }}</td>
            </tr>
          </tbody>
        </table>
        <div v-if="activityLogs.length === 0 && !logsLoading" class="p-8 text-center text-tp-muted text-sm">
          No activity logs yet
        </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════════════════════════════════
         INTEGRATIONS TAB
         ═══════════════════════════════════════════════════════════════════════ -->
    <div v-else-if="activeTab === 'integrations'" class="space-y-4">
      <div class="bg-tp-surface rounded-2xl p-6 space-y-4">
        <div class="flex items-start justify-between gap-4">
          <div>
            <h3 class="text-tp-text font-display font-semibold text-lg flex items-center gap-2">
              <Plug class="w-4 h-4 text-tp-primary" />
              CurseForge
            </h3>
            <p class="text-tp-muted text-sm mt-1">
              Required for the in-panel mod browser. Get a key at
              <a href="https://console.curseforge.com/" target="_blank" rel="noopener"
                class="text-tp-primary hover:underline inline-flex items-center gap-1">
                console.curseforge.com <ExternalLink class="w-3 h-3" />
              </a>.
            </p>
          </div>
          <span
            :class="[
              'text-xs px-2 py-1 rounded-full font-medium whitespace-nowrap',
              cfStatus.configured
                ? 'bg-tp-success/15 text-tp-success'
                : 'bg-tp-warning/15 text-tp-warning',
            ]">
            {{ cfStatus.configured ? 'Configured' : 'Not configured' }}
          </span>
        </div>

        <div v-if="cfStatus.configured" class="text-tp-muted text-sm">
          Current key: <code class="font-mono text-tp-text">{{ cfStatus.preview || '••••' }}</code>
        </div>

        <div class="flex flex-col gap-2">
          <label class="text-xs font-semibold text-tp-muted uppercase tracking-wider">API key</label>
          <input
            v-model="cfKeyInput"
            type="password"
            autocomplete="off"
            placeholder="$2a$10$…"
            class="w-full bg-tp-surface2 text-tp-text rounded-xl px-4 py-2.5 text-sm font-mono placeholder:text-tp-outline focus:outline-none focus:ring-2 focus:ring-tp-primary/50 transition-all"
          />
        </div>

        <div class="flex items-center gap-2">
          <UiButton variant="primary" size="sm" :loading="cfSaving" :disabled="!cfKeyInput.trim()" @click="saveCurseForgeKey">
            <Save class="w-3.5 h-3.5" /> Save
          </UiButton>
          <UiButton v-if="cfStatus.configured" variant="danger" size="sm" :loading="cfSaving" @click="clearCurseForgeKey">
            <Trash2 class="w-3.5 h-3.5" /> Clear
          </UiButton>
          <UiButton variant="secondary" size="sm" :loading="cfLoading" @click="fetchCurseForgeStatus">
            <RefreshCw class="w-3.5 h-3.5" />
          </UiButton>
        </div>

        <p class="text-tp-outline text-xs">
          Stored AES-256-GCM encrypted in <code class="font-mono">app_settings</code>. Takes effect immediately —
          no API restart required. Clearing falls back to the <code class="font-mono">CURSEFORGE_API_KEY</code>
          env value (if set) on the next API restart.
        </p>
      </div>
    </div>

    <!-- Create User Modal -->
    <UiModal :open="showCreateUser" title="Create User" size="md" @close="showCreateUser = false">
      <form class="space-y-4" @submit.prevent="submitCreateUser">
        <div v-if="createUserError" class="bg-tp-error/10 rounded-xl px-3 py-2.5 text-tp-error text-sm">
          {{ createUserError }}
        </div>

        <div class="flex flex-col gap-1.5">
          <label class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Username</label>
          <div class="relative">
            <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-tp-outline text-lg">person</span>
            <input
              v-model="createUserForm.username"
              type="text"
              placeholder="newuser"
              required
              class="w-full bg-tp-surface text-tp-text rounded-xl pl-10 pr-4 py-2.5 text-sm placeholder:text-tp-outline focus:outline-none focus:bg-tp-surface-lowest focus:ring-2 focus:ring-tp-primary/50 transition-all"
            />
          </div>
        </div>

        <div class="flex flex-col gap-1.5">
          <label class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Email</label>
          <div class="relative">
            <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-tp-outline text-lg">alternate_email</span>
            <input
              v-model="createUserForm.email"
              type="email"
              placeholder="user@example.com"
              required
              class="w-full bg-tp-surface text-tp-text rounded-xl pl-10 pr-4 py-2.5 text-sm placeholder:text-tp-outline focus:outline-none focus:bg-tp-surface-lowest focus:ring-2 focus:ring-tp-primary/50 transition-all"
            />
          </div>
        </div>

        <div class="flex flex-col gap-1.5">
          <label class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Password</label>
          <div class="relative">
            <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-tp-outline text-lg">lock</span>
            <input
              v-model="createUserForm.password"
              type="password"
              placeholder="Min. 8 characters"
              required
              class="w-full bg-tp-surface text-tp-text rounded-xl pl-10 pr-4 py-2.5 text-sm placeholder:text-tp-outline focus:outline-none focus:bg-tp-surface-lowest focus:ring-2 focus:ring-tp-primary/50 transition-all"
            />
          </div>
        </div>

        <div class="flex flex-col gap-1.5">
          <label class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Role</label>
          <select
            v-model="createUserForm.role"
            class="bg-tp-surface text-tp-text rounded-xl px-4 py-2.5 text-sm focus:outline-none focus:ring-2 focus:ring-tp-primary/50 transition-all"
          >
            <option value="user">User</option>
            <option value="moderator">Moderator</option>
            <option value="admin">Admin</option>
            <option v-if="isOwner" value="owner">Owner</option>
          </select>
        </div>
      </form>

      <template #footer>
        <UiButton variant="ghost" size="md" @click="showCreateUser = false">Cancel</UiButton>
        <UiButton variant="primary" size="md" :loading="createUserLoading" @click="submitCreateUser">
          <span class="material-symbols-outlined text-base">person_add</span>
          Create User
        </UiButton>
      </template>
    </UiModal>

    <!-- Toast -->
    <Transition name="toast">
      <div v-if="toast" :class="[
        'fixed bottom-6 right-6 z-50 flex items-center gap-3 px-4 py-3 rounded-xl shadow-lg text-sm font-medium',
        toastType === 'success' ? 'bg-tp-success/15 text-tp-success' : 'bg-tp-error/10 text-tp-error',
      ]">
        <div :class="['w-2 h-2 rounded-full', toastType === 'success' ? 'bg-tp-success' : 'bg-tp-danger']" />
        {{ toast }}
      </div>
    </Transition>

    <!-- Permissions modal -->
    <div
      v-if="permUser"
      class="fixed inset-0 bg-black/50 flex items-center justify-center z-40 p-4"
      @click.self="closePermissions"
    >
      <div class="bg-tp-surface rounded-2xl w-full max-w-2xl max-h-[85vh] flex flex-col">
        <div class="px-5 py-4 border-b border-tp-border flex items-center justify-between">
          <div>
            <h3 class="text-tp-text font-display font-semibold text-base">
              Permissions — {{ permUser.username }}
            </h3>
            <p class="text-tp-muted text-xs mt-0.5">
              Role-default: <span class="capitalize">{{ permUser.role }}</span>.  Any override here wins over the role.
            </p>
          </div>
          <button class="text-tp-muted hover:text-tp-text" @click="closePermissions">✕</button>
        </div>

        <div v-if="permLoading" class="p-8 text-center text-tp-muted text-sm">Loading…</div>

        <div v-else class="flex-1 overflow-y-auto px-5 py-4 space-y-5">
          <div v-for="(items, group) in permGroups" :key="group">
            <p class="text-[10px] uppercase tracking-widest font-semibold text-tp-outline mb-2">{{ group }}</p>
            <div class="space-y-1">
              <div
                v-for="p in items" :key="p.key"
                class="flex items-center justify-between gap-3 rounded-xl bg-tp-surface2 px-3 py-2"
              >
                <div>
                  <p class="text-tp-text text-sm font-medium">{{ p.label }}</p>
                  <p class="text-tp-outline text-[10px] font-mono">{{ p.key }}</p>
                </div>
                <div class="flex items-center gap-1">
                  <button
                    v-for="opt in [
                      { state: 'inherit' as const, label: 'Inherit', cls: 'bg-tp-surface border border-tp-border text-tp-muted' },
                      { state: 'grant'   as const, label: 'Grant',   cls: 'bg-tp-success/15 text-tp-success border border-tp-success/30' },
                      { state: 'deny'    as const, label: 'Deny',    cls: 'bg-tp-danger/10 text-tp-danger border border-tp-danger/30' },
                    ]" :key="opt.state"
                    :class="[
                      'text-[10px] font-semibold uppercase px-2 py-1 rounded-md transition-opacity',
                      permState(p.key) === opt.state ? opt.cls : 'bg-transparent border border-tp-border text-tp-outline opacity-60 hover:opacity-100',
                    ]"
                    @click="setPermState(p.key, opt.state)"
                  >
                    {{ opt.label }}
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div class="px-5 py-3 border-t border-tp-border flex items-center justify-end gap-2">
          <button
            class="px-3 py-2 rounded-xl text-tp-muted text-sm hover:bg-tp-surface2"
            @click="closePermissions"
          >
            Cancel
          </button>
          <button
            class="px-4 py-2 rounded-xl bg-tp-primary text-white text-sm font-medium hover:opacity-90 disabled:opacity-50"
            :disabled="permSaving || permLoading"
            @click="savePermissions"
          >
            {{ permSaving ? 'Saving…' : 'Save permissions' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.toast-enter-active, .toast-leave-active { transition: opacity 0.25s ease, transform 0.25s ease; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateY(8px); }
</style>
