<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { invoke } from '@tauri-apps/api/core'
import { Play, Square, RotateCcw, RefreshCw } from 'lucide-vue-next'

interface Server {
  id: string
  name: string
  status: string
  port: number
  hytale_version: string
  ram_limit_mb: number | null
}

const router = useRouter()
const servers = ref<Server[]>([])
const loading = ref(false)
const actionLoading = ref<string | null>(null)

onMounted(() => fetchServers())

async function fetchServers() {
  loading.value = true
  try {
    servers.value = await invoke<Server[]>('get_servers')
  } catch { /* ignore */ }
  finally { loading.value = false }
}

async function serverAction(id: string, action: 'start' | 'stop' | 'restart') {
  actionLoading.value = id
  try {
    await invoke(`${action}_server`, { id })
    setTimeout(fetchServers, 1000)
  } catch { /* ignore */ }
  finally { actionLoading.value = null }
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
      <h2 class="text-tp-text font-bold text-2xl">Servers</h2>
      <button
        :disabled="loading"
        class="flex items-center gap-2 px-3 py-1.5 bg-tp-surface2 border border-tp-border rounded-lg text-tp-muted hover:text-tp-text text-xs transition-colors"
        @click="fetchServers"
      >
        <RefreshCw :class="['w-3.5 h-3.5', loading ? 'animate-spin' : '']" />
        Refresh
      </button>
    </div>

    <div v-if="servers.length === 0" class="bg-tp-surface rounded-xl border border-tp-border p-12 text-center text-tp-muted text-sm">
      {{ loading ? 'Loading...' : 'No servers found' }}
    </div>

    <div v-else class="space-y-3">
      <div v-for="server in servers" :key="server.id"
        class="bg-tp-surface rounded-xl border border-tp-border p-4 flex items-center justify-between hover:border-tp-primary/30 transition-colors">
        <div class="flex items-center gap-4 cursor-pointer flex-1" @click="router.push(`/servers/${server.id}`)">
          <div :class="['w-3 h-3 rounded-full shrink-0', statusColor(server.status)]" />
          <div>
            <p class="text-tp-text font-semibold">{{ server.name }}</p>
            <p class="text-tp-muted text-xs">Port :{{ server.port }} | {{ server.hytale_version }} | {{ server.ram_limit_mb ?? '?' }} MB RAM</p>
          </div>
        </div>

        <div class="flex items-center gap-2">
          <button v-if="server.status !== 'running'"
            :disabled="actionLoading === server.id"
            class="p-2 rounded-lg bg-tp-success/10 text-tp-success hover:bg-tp-success/20 transition-colors"
            @click.stop="serverAction(server.id, 'start')"
            title="Start">
            <Play class="w-4 h-4" />
          </button>
          <button v-if="server.status === 'running'"
            :disabled="actionLoading === server.id"
            class="p-2 rounded-lg bg-tp-danger/10 text-tp-danger hover:bg-tp-danger/20 transition-colors"
            @click.stop="serverAction(server.id, 'stop')"
            title="Stop">
            <Square class="w-4 h-4" />
          </button>
          <button v-if="server.status === 'running'"
            :disabled="actionLoading === server.id"
            class="p-2 rounded-lg bg-tp-warning/10 text-tp-warning hover:bg-tp-warning/20 transition-colors"
            @click.stop="serverAction(server.id, 'restart')"
            title="Restart">
            <RotateCcw class="w-4 h-4" />
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
