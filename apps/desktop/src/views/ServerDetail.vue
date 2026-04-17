<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { invoke } from '@tauri-apps/api/core'
import { ArrowLeft, Play, Square, RotateCcw, Loader2 } from 'lucide-vue-next'

interface Server {
  id: string
  name: string
  status: string
  port: number
  hytale_version: string
  auto_restart: boolean
  ram_limit_mb: number | null
  cpu_limit: number | null
}

const route = useRoute()
const router = useRouter()
const server = ref<Server | null>(null)
const loading = ref(false)
const actionLoading = ref(false)

onMounted(() => fetchServer())

async function fetchServer() {
  loading.value = true
  try {
    server.value = await invoke<Server>('get_server', { id: route.params.id as string })
  } catch { /* ignore */ }
  finally { loading.value = false }
}

async function serverAction(action: 'start' | 'stop' | 'restart') {
  if (!server.value) return
  actionLoading.value = true
  try {
    await invoke(`${action}_server`, { id: server.value.id })
    setTimeout(fetchServer, 1000)
  } catch { /* ignore */ }
  finally { actionLoading.value = false }
}

function statusColor(status: string) {
  switch (status) {
    case 'running': return 'text-tp-success'
    case 'crashed': return 'text-tp-danger'
    case 'starting': case 'stopping': return 'text-tp-warning'
    default: return 'text-tp-muted'
  }
}
</script>

<template>
  <div class="p-6 space-y-5">
    <div class="flex items-center gap-3">
      <button class="p-1.5 rounded-lg hover:bg-tp-surface2 text-tp-muted hover:text-tp-text transition-colors" @click="router.push('/servers')">
        <ArrowLeft class="w-5 h-5" />
      </button>
      <h2 class="text-tp-text font-bold text-2xl">{{ server?.name || 'Loading...' }}</h2>
    </div>

    <div v-if="loading && !server" class="flex items-center justify-center py-20">
      <Loader2 class="w-6 h-6 text-tp-primary animate-spin" />
    </div>

    <template v-if="server">
      <!-- Status & Actions -->
      <div class="bg-tp-surface rounded-xl border border-tp-border p-5">
        <div class="flex items-center justify-between">
          <div>
            <p class="text-tp-muted text-xs uppercase tracking-wider mb-1">Status</p>
            <p :class="['font-bold text-xl capitalize', statusColor(server.status)]">{{ server.status }}</p>
          </div>
          <div class="flex items-center gap-2">
            <button v-if="server.status !== 'running'"
              :disabled="actionLoading"
              class="flex items-center gap-2 px-4 py-2 rounded-lg bg-tp-success text-white text-sm font-medium hover:bg-tp-success/90 disabled:opacity-50 transition-colors"
              @click="serverAction('start')">
              <Play class="w-4 h-4" /> Start
            </button>
            <button v-if="server.status === 'running'"
              :disabled="actionLoading"
              class="flex items-center gap-2 px-4 py-2 rounded-lg bg-tp-danger text-white text-sm font-medium hover:bg-tp-danger/90 disabled:opacity-50 transition-colors"
              @click="serverAction('stop')">
              <Square class="w-4 h-4" /> Stop
            </button>
            <button v-if="server.status === 'running'"
              :disabled="actionLoading"
              class="flex items-center gap-2 px-4 py-2 rounded-lg bg-tp-warning text-black text-sm font-medium hover:bg-tp-warning/90 disabled:opacity-50 transition-colors"
              @click="serverAction('restart')">
              <RotateCcw class="w-4 h-4" /> Restart
            </button>
          </div>
        </div>
      </div>

      <!-- Server info -->
      <div class="bg-tp-surface rounded-xl border border-tp-border overflow-hidden">
        <div class="px-5 py-3 border-b border-tp-border">
          <h3 class="text-tp-text font-semibold text-sm">Server Information</h3>
        </div>
        <div class="grid grid-cols-2 gap-px bg-tp-border">
          <div v-for="info in [
            { label: 'ID', value: server.id },
            { label: 'Port', value: `:${server.port}` },
            { label: 'Version', value: server.hytale_version },
            { label: 'RAM Limit', value: server.ram_limit_mb ? `${server.ram_limit_mb} MB` : 'Unlimited' },
            { label: 'CPU Limit', value: server.cpu_limit ? `${server.cpu_limit}%` : 'Unlimited' },
            { label: 'Auto Restart', value: server.auto_restart ? 'Enabled' : 'Disabled' },
          ]" :key="info.label"
            class="bg-tp-surface px-4 py-3">
            <p class="text-tp-muted text-[10px] uppercase tracking-wider mb-0.5">{{ info.label }}</p>
            <p class="text-tp-text text-sm font-mono">{{ info.value }}</p>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>
