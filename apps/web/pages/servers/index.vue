<script setup lang="ts">
import { useServersStore } from '~/stores/servers'
import type { Server as ServerType } from '~/types'

definePageMeta({
  title: 'Servers',
  middleware: 'auth',
})

const serversStore = useServersStore()

onMounted(() => {
  serversStore.fetchServers()
})

// Modal state
const showCreateModal = ref(false)
const createLoading = ref(false)
const createError = ref('')

const createForm = reactive({
  name: '',
  hytale_version: 'latest',
  ram_limit_mb: null as number | null,
  cpu_limit: null as number | null,
  auto_restart: false,
})

async function submitCreate() {
  if (!createForm.name.trim()) return
  createLoading.value = true
  createError.value = ''
  try {
    await serversStore.createServer({
      name: createForm.name.trim(),
      hytale_version: createForm.hytale_version || 'latest',
      ram_limit_mb: createForm.ram_limit_mb ? parseInt(String(createForm.ram_limit_mb), 10) : undefined,
      cpu_limit: createForm.cpu_limit ? parseInt(String(createForm.cpu_limit), 10) : undefined,
      auto_restart: createForm.auto_restart,
    })
    showCreateModal.value = false
    resetForm()
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    createError.value = e.data?.error ?? e.message ?? 'Failed to create server'
  } finally {
    createLoading.value = false
  }
}

function resetForm() {
  createForm.name = ''
  createForm.hytale_version = 'latest'
  createForm.ram_limit_mb = null
  createForm.cpu_limit = null
  createForm.auto_restart = false
  createError.value = ''
}

function openModal() {
  resetForm()
  showCreateModal.value = true
}

// Filtering
const searchQuery = ref('')
const statusFilter = ref<'all' | ServerType['status']>('all')

const statusOptions: { value: 'all' | ServerType['status']; label: string }[] = [
  { value: 'all',       label: 'All Status' },
  { value: 'running',   label: 'Running' },
  { value: 'stopped',   label: 'Stopped' },
  { value: 'crashed',   label: 'Crashed' },
  { value: 'starting',  label: 'Starting' },
  { value: 'stopping',  label: 'Stopping' },
  { value: 'installing',label: 'Installing' },
]

const filteredServers = computed(() => {
  let list: ServerType[] = Array.isArray(serversStore.servers) ? serversStore.servers : []

  if (statusFilter.value !== 'all') {
    list = list.filter((s) => s.status === statusFilter.value)
  }

  if (searchQuery.value.trim()) {
    const q = searchQuery.value.trim().toLowerCase()
    list = list.filter((s) => s.name.toLowerCase().includes(q))
  }

  return list
})

// Pagination
const page = ref(1)
const perPage = 10
const totalPages = computed(() => Math.max(1, Math.ceil(filteredServers.value.length / perPage)))
const paginatedServers = computed(() => {
  const start = (page.value - 1) * perPage
  return filteredServers.value.slice(start, start + perPage)
})

function statusBadgeClass(status: string) {
  switch (status) {
    case 'running': return 'bg-tp-tertiary/20 text-tp-tertiary'
    case 'starting':
    case 'stopping': return 'bg-tp-accent/20 text-tp-accent'
    case 'crashed': return 'bg-tp-error/20 text-tp-error'
    default: return 'bg-tp-surface3 text-tp-muted'
  }
}

function statusLabel(status: string) {
  switch (status) {
    case 'running': return 'Online'
    case 'stopped': return 'Offline'
    case 'starting': return 'Starting'
    case 'stopping': return 'Stopping'
    case 'crashed': return 'Crashed'
    case 'installing': return 'Installing'
    default: return status
  }
}

function ramPercent(server: ServerType): number {
  if (!server.ram_limit_mb) return 0
  return Math.min(100, Math.round(Math.random() * 80))
}
</script>

<template>
  <div class="p-6 space-y-6">
    <!-- Page header -->
    <div>
      <div class="flex items-start justify-between mb-1">
        <div>
          <h2 class="text-tp-text font-display font-bold text-3xl">Fleet Overview</h2>
          <p class="text-tp-muted text-sm mt-1">Monitor and manage your high-performance Hytale clusters across global nodes.</p>
        </div>
        <div class="flex items-center gap-3">
          <!-- Quick stats -->
          <div class="bg-tp-surface2 rounded-xl px-5 py-3 text-center">
            <p class="text-tp-text font-display font-bold text-2xl tabular-nums">{{ serversStore.servers.length }}</p>
            <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold">Active Servers</p>
          </div>
          <div class="bg-tp-surface2 rounded-xl px-5 py-3 text-center">
            <p class="text-tp-text font-display font-bold text-2xl tabular-nums">—</p>
            <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold">Total RAM Load</p>
          </div>
        </div>
      </div>
    </div>

    <!-- Filter bar -->
    <div class="flex items-center justify-between gap-3">
      <div class="flex items-center gap-3">
        <!-- Search -->
        <div class="relative">
          <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-tp-outline text-lg pointer-events-none">search</span>
          <input
            v-model="searchQuery"
            type="text"
            placeholder="Search servers..."
            class="bg-tp-surface2 text-tp-text rounded-xl pl-10 pr-4 py-2.5 text-sm placeholder:text-tp-outline focus:outline-none focus:ring-2 focus:ring-tp-primary/50 focus:bg-tp-surface transition-all w-64"
          />
        </div>

        <!-- Status filter -->
        <select
          v-model="statusFilter"
          class="bg-tp-surface2 text-tp-text rounded-xl px-4 py-2.5 text-sm focus:outline-none focus:ring-2 focus:ring-tp-primary/50 transition-all"
        >
          <option v-for="opt in statusOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
        </select>
      </div>

      <UiButton variant="primary" size="md" @click="openModal">
        <span class="material-symbols-outlined text-base">add</span>
        New Server
      </UiButton>
    </div>

    <!-- Error -->
    <div v-if="serversStore.error" class="bg-tp-error/10 rounded-xl px-4 py-3 text-tp-error text-sm">
      {{ serversStore.error }}
    </div>

    <!-- Loading -->
    <div v-if="serversStore.loading" class="bg-tp-surface2 rounded-xl p-8 animate-pulse">
      <div v-for="i in 4" :key="i" class="h-16 bg-tp-surface3 rounded-xl mb-3 last:mb-0" />
    </div>

    <!-- Empty state -->
    <div
      v-else-if="filteredServers.length === 0"
      class="bg-tp-surface2 rounded-xl p-16 text-center"
    >
      <div class="w-16 h-16 bg-tp-surface3 rounded-2xl flex items-center justify-center mx-auto mb-4">
        <span class="material-symbols-outlined text-3xl text-tp-muted">dns</span>
      </div>
      <h4 class="text-tp-text font-display font-semibold text-lg mb-2">
        {{ searchQuery || statusFilter !== 'all' ? 'No servers match your filters' : 'No servers yet' }}
      </h4>
      <p class="text-tp-muted text-sm mb-6">
        {{ searchQuery || statusFilter !== 'all' ? 'Try adjusting your search or filters.' : 'Create your first Hytale server to get started.' }}
      </p>
      <UiButton v-if="!searchQuery && statusFilter === 'all'" variant="primary" size="md" @click="openModal">
        <span class="material-symbols-outlined text-base">add</span>
        Create your first server
      </UiButton>
    </div>

    <!-- Server table -->
    <div v-else class="bg-tp-surface2 rounded-xl overflow-hidden">
      <!-- Table header -->
      <div class="grid grid-cols-[auto_1fr_auto_auto_auto_auto_auto] gap-4 items-center px-5 py-3 border-b border-tp-border/30">
        <div class="w-12" />
        <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold">Server Identity</p>
        <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold w-20 text-center">Status</p>
        <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold w-20 text-center">Node ID</p>
        <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold w-20 text-center">Network</p>
        <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold w-36">Resources (RAM)</p>
        <p class="text-[10px] text-tp-outline uppercase tracking-widest font-semibold w-28 text-center">Quick Actions</p>
      </div>

      <!-- Server rows -->
      <NuxtLink
        v-for="server in paginatedServers"
        :key="server.id"
        :to="`/servers/${server.id}`"
        class="grid grid-cols-[auto_1fr_auto_auto_auto_auto_auto] gap-4 items-center px-5 py-4 hover:bg-tp-surface3/50 transition-colors cursor-pointer"
      >
        <!-- Server image placeholder -->
        <div class="w-12 h-12 rounded-xl bg-tp-surface3 flex items-center justify-center shrink-0">
          <span class="material-symbols-outlined text-tp-muted text-xl">dns</span>
        </div>

        <!-- Server info -->
        <div class="min-w-0">
          <p class="text-tp-text font-display font-semibold text-sm truncate">{{ server.name }}</p>
          <p class="text-tp-muted text-xs truncate">v{{ server.hytale_version }}</p>
        </div>

        <!-- Status badge -->
        <div class="w-20 flex justify-center">
          <span :class="['inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-semibold', statusBadgeClass(server.status)]">
            <span :class="['w-1.5 h-1.5 rounded-full', server.status === 'running' ? 'bg-tp-tertiary' : server.status === 'crashed' ? 'bg-tp-error' : 'bg-current']" />
            {{ statusLabel(server.status) }}
          </span>
        </div>

        <!-- Node ID -->
        <p class="text-tp-muted text-xs w-20 text-center truncate">{{ server.node_id?.slice(0, 8) ?? '—' }}</p>

        <!-- Network/Port -->
        <div class="w-20 flex items-center justify-center gap-1 text-tp-muted text-xs">
          <span class="material-symbols-outlined text-sm">settings_input_component</span>
          {{ server.port ?? '—' }}
        </div>

        <!-- RAM bar -->
        <div class="w-36 flex items-center gap-2">
          <div class="flex-1 h-1.5 bg-tp-surface-highest rounded-full overflow-hidden">
            <div class="h-full progress-fill rounded-full transition-all" :style="{ width: (server.ram_limit_mb ? ramPercent(server) : 0) + '%' }" />
          </div>
          <span class="text-tp-muted text-[10px] whitespace-nowrap tabular-nums">
            {{ server.ram_limit_mb ? `${server.ram_limit_mb} MB` : '—' }}
          </span>
        </div>

        <!-- Quick actions -->
        <div class="w-28 flex items-center justify-center gap-1" @click.prevent.stop>
          <button class="w-7 h-7 flex items-center justify-center rounded-lg text-tp-accent hover:bg-tp-surface3 transition-colors">
            <span class="material-symbols-outlined text-base">settings</span>
          </button>
          <button class="w-7 h-7 flex items-center justify-center rounded-lg text-tp-accent hover:bg-tp-surface3 transition-colors">
            <span class="material-symbols-outlined text-base">{{ server.status === 'running' ? 'stop' : 'play_arrow' }}</span>
          </button>
          <button class="w-7 h-7 flex items-center justify-center rounded-lg text-tp-accent hover:bg-tp-surface3 transition-colors">
            <span class="material-symbols-outlined text-base">more_vert</span>
          </button>
        </div>
      </NuxtLink>

      <!-- Pagination -->
      <div class="px-5 py-4 border-t border-tp-border/30 flex items-center justify-between">
        <p class="text-tp-muted text-xs">
          Showing {{ (page - 1) * perPage + 1 }} to {{ Math.min(page * perPage, filteredServers.length) }} of {{ filteredServers.length }} instances
        </p>
        <div class="flex items-center gap-2" v-if="totalPages > 1">
          <button
            class="w-8 h-8 flex items-center justify-center rounded-lg bg-tp-surface3 text-tp-muted hover:text-tp-text transition-colors disabled:opacity-30"
            :disabled="page <= 1"
            @click="page--"
          >
            <span class="material-symbols-outlined text-base">chevron_left</span>
          </button>
          <button
            v-for="p in totalPages"
            :key="p"
            :class="[
              'w-8 h-8 flex items-center justify-center rounded-lg text-xs font-semibold transition-colors',
              p === page ? 'bg-tp-primary text-tp-on-primary' : 'bg-tp-surface3 text-tp-muted hover:text-tp-text',
            ]"
            @click="page = p"
          >
            {{ p }}
          </button>
          <button
            class="w-8 h-8 flex items-center justify-center rounded-lg bg-tp-surface3 text-tp-muted hover:text-tp-text transition-colors disabled:opacity-30"
            :disabled="page >= totalPages"
            @click="page++"
          >
            <span class="material-symbols-outlined text-base">chevron_right</span>
          </button>
        </div>
      </div>
    </div>

    <!-- Bottom info cards -->
    <div class="grid grid-cols-1 md:grid-cols-2 gap-5">
      <div class="bg-tp-surface2 rounded-xl p-5">
        <div class="flex items-center gap-3 mb-2">
          <span class="material-symbols-outlined text-tp-accent">info</span>
          <h4 class="text-tp-text font-display font-semibold text-sm">System Maintenance</h4>
        </div>
        <p class="text-tp-muted text-xs">No scheduled maintenance at this time.</p>
      </div>
      <div class="bg-tp-surface2 rounded-xl p-5">
        <h4 class="text-tp-text font-display font-semibold text-sm mb-3">API Status</h4>
        <div class="flex items-center justify-between text-xs mb-1.5">
          <span class="text-tp-muted">Latency</span>
          <span class="text-tp-text font-mono">—</span>
        </div>
        <div class="flex items-center justify-between text-xs mb-3">
          <span class="text-tp-muted">Uptime (30d)</span>
          <span class="text-tp-text font-mono">—</span>
        </div>
        <a href="#" class="text-tp-accent text-xs hover:text-tp-primary transition-colors">View Incidents</a>
      </div>
    </div>

    <!-- Create Server Modal -->
    <UiModal
      :open="showCreateModal"
      title="Create New Server"
      size="md"
      @close="showCreateModal = false"
    >
      <form class="space-y-4" @submit.prevent="submitCreate">
        <div v-if="createError" class="bg-tp-error/10 rounded-xl px-3 py-2.5 text-tp-error text-sm">
          {{ createError }}
        </div>

        <UiInput v-model="createForm.name" label="Server Name" placeholder="My Hytale Server" :required="true" />
        <UiInput v-model="createForm.hytale_version" label="Hytale Version" placeholder="latest" />

        <div class="grid grid-cols-2 gap-3">
          <UiInput v-model="createForm.ram_limit_mb" type="number" label="RAM Limit (MB)" placeholder="e.g. 2048" />
          <UiInput v-model="createForm.cpu_limit" type="number" label="CPU Limit (%)" placeholder="e.g. 100" />
        </div>

        <div class="flex items-center justify-between bg-tp-surface rounded-xl px-4 py-3">
          <div>
            <p class="text-tp-text text-sm font-medium">Auto Restart</p>
            <p class="text-tp-muted text-xs mt-0.5">Automatically restart server on crash</p>
          </div>
          <button
            type="button"
            :class="[
              'relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full transition-colors duration-200',
              createForm.auto_restart ? 'bg-tp-primary' : 'bg-tp-surface-highest',
            ]"
            @click="createForm.auto_restart = !createForm.auto_restart"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow transition duration-200',
                createForm.auto_restart ? 'translate-x-5' : 'translate-x-0',
              ]"
            />
          </button>
        </div>
      </form>

      <template #footer>
        <UiButton variant="ghost" size="md" @click="showCreateModal = false">Cancel</UiButton>
        <UiButton variant="primary" size="md" :loading="createLoading" @click="submitCreate">Create Server</UiButton>
      </template>
    </UiModal>
  </div>
</template>
