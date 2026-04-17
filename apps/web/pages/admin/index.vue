<script setup lang="ts">
import {
  Users, Network, Shield, RefreshCw, Trash2, UserCheck, UserX,
  ChevronDown, Power, PowerOff, Pause, Activity, Clock, Search,
} from 'lucide-vue-next'
import { useApi } from '~/composables/useApi'
import { useAuthStore } from '~/stores/auth'

definePageMeta({ title: 'Admin Panel', middleware: 'auth' })

const api = useApi()
const authStore = useAuthStore()

// ── Tabs ─────────────────────────────────────────────────────────────────────
const activeTab = ref<'users' | 'nodes' | 'logs'>('users')

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
        ]"
        :key="tab.key"
        :class="[
          'flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-all',
          activeTab === tab.key
            ? 'bg-tp-primary text-white shadow-sm'
            : 'text-tp-muted hover:text-tp-text hover:bg-tp-surface2',
        ]"
        @click="activeTab = tab.key as 'users' | 'nodes' | 'logs'"
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
  </div>
</template>

<style scoped>
.toast-enter-active, .toast-leave-active { transition: opacity 0.25s ease, transform 0.25s ease; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateY(8px); }
</style>
