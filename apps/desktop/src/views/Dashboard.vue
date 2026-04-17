<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { Server as ServerIcon, Cpu, Activity, RefreshCw } from 'lucide-vue-next'

interface Server {
  id: string
  name: string
  status: string
  port: number
  hytale_version: string
  ram_limit_mb: number | null
}

interface Node {
  id: string
  name: string
  fqdn: string
  status: string
  total_cpu: number
  total_ram_mb: number
}

const servers = ref<Server[]>([])
const nodes = ref<Node[]>([])
const loading = ref(false)

const runningCount = computed(() => servers.value.filter(s => s.status === 'running').length)

onMounted(() => fetchAll())

async function fetchAll() {
  loading.value = true
  try {
    const [s, n] = await Promise.allSettled([
      invoke<Server[]>('get_servers'),
      invoke<Node[]>('get_nodes'),
    ])
    if (s.status === 'fulfilled') servers.value = s.value
    if (n.status === 'fulfilled') nodes.value = n.value
  } finally {
    loading.value = false
  }
}

function statusColor(status: string) {
  switch (status) {
    case 'running': return 'bg-tp-success'
    case 'crashed': return 'bg-tp-danger'
    case 'starting': case 'stopping': return 'bg-tp-warning'
    default: return 'bg-tp-muted'
  }
}
</script>

<template>
  <div class="p-6 space-y-5">
    <div class="flex items-center justify-between">
      <h2 class="text-tp-text font-bold text-2xl">Dashboard</h2>
      <button
        :disabled="loading"
        class="flex items-center gap-2 px-3 py-1.5 bg-tp-surface2 border border-tp-border rounded-lg text-tp-muted hover:text-tp-text text-xs transition-colors"
        @click="fetchAll"
      >
        <RefreshCw :class="['w-3.5 h-3.5', loading ? 'animate-spin' : '']" />
        Refresh
      </button>
    </div>

    <!-- Stat cards -->
    <div class="grid grid-cols-3 gap-4">
      <div v-for="stat in [
        { label: 'Total Servers', value: servers.length, icon: ServerIcon, color: 'text-tp-primary', bg: 'bg-tp-primary/10' },
        { label: 'Running', value: runningCount, icon: Activity, color: 'text-tp-success', bg: 'bg-tp-success/10' },
        { label: 'Nodes Online', value: nodes.filter(n => n.status === 'online').length, icon: Cpu, color: 'text-tp-warning', bg: 'bg-tp-warning/10' },
      ]" :key="stat.label"
        class="bg-tp-surface rounded-xl border border-tp-border p-4 flex items-center gap-4">
        <div :class="['w-10 h-10 rounded-lg flex items-center justify-center shrink-0', stat.bg]">
          <component :is="stat.icon" :class="['w-5 h-5', stat.color]" />
        </div>
        <div>
          <p class="text-tp-text font-bold text-2xl leading-none">{{ stat.value }}</p>
          <p class="text-tp-muted text-xs mt-1">{{ stat.label }}</p>
        </div>
      </div>
    </div>

    <!-- Server list -->
    <div class="bg-tp-surface rounded-xl border border-tp-border overflow-hidden">
      <div class="px-5 py-3 border-b border-tp-border">
        <h3 class="text-tp-text font-semibold text-sm">Servers</h3>
      </div>
      <div v-if="servers.length === 0" class="p-8 text-center text-tp-muted text-sm">
        {{ loading ? 'Loading...' : 'No servers found' }}
      </div>
      <table v-else class="w-full text-sm">
        <thead>
          <tr class="border-b border-tp-border">
            <th class="text-left px-4 py-2 text-tp-muted font-medium text-xs uppercase">Name</th>
            <th class="text-left px-4 py-2 text-tp-muted font-medium text-xs uppercase">Status</th>
            <th class="text-left px-4 py-2 text-tp-muted font-medium text-xs uppercase">Port</th>
            <th class="text-left px-4 py-2 text-tp-muted font-medium text-xs uppercase">Version</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="server in servers" :key="server.id"
            class="border-b border-tp-border/50 hover:bg-tp-surface2/50 cursor-pointer"
            @click="$router.push(`/servers/${server.id}`)">
            <td class="px-4 py-3 text-tp-text font-medium">{{ server.name }}</td>
            <td class="px-4 py-3">
              <div class="flex items-center gap-2">
                <div :class="['w-2 h-2 rounded-full', statusColor(server.status)]" />
                <span class="text-tp-muted text-xs capitalize">{{ server.status }}</span>
              </div>
            </td>
            <td class="px-4 py-3 text-tp-muted font-mono text-xs">:{{ server.port }}</td>
            <td class="px-4 py-3 text-tp-muted text-xs">{{ server.hytale_version }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
