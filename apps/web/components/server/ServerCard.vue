<script setup lang="ts">
import type { Server } from '~/types'
import { useServersStore } from '~/stores/servers'

interface Props {
  server: Server
}

const props = defineProps<Props>()
const serversStore = useServersStore()
const router = useRouter()

const isRunning = computed(() => props.server.status === 'running')
const isStopped = computed(() => props.server.status === 'stopped' || props.server.status === 'crashed')
const isTransitioning = computed(() =>
  props.server.status === 'starting' || props.server.status === 'stopping' || props.server.status === 'installing'
)

const actionLoading = ref(false)

async function toggleServer(event: MouseEvent) {
  event.preventDefault()
  event.stopPropagation()

  if (actionLoading.value || isTransitioning.value) return

  actionLoading.value = true
  try {
    if (isRunning.value) {
      await serversStore.stopServer(props.server.id)
    } else if (isStopped.value) {
      await serversStore.startServer(props.server.id)
    }
  } finally {
    actionLoading.value = false
  }
}

function navigateToServer() {
  router.push(`/servers/${props.server.id}`)
}

// CPU and RAM display
const cpuDisplay = computed(() => '\u2014')
const ramDisplay = computed(() => {
  if (props.server.ram_limit_mb) {
    return `${props.server.ram_limit_mb} MB`
  }
  return '\u2014'
})

const statusBarColor = computed(() => {
  switch (props.server.status) {
    case 'running': return 'bg-tp-success'
    case 'crashed': return 'bg-tp-danger'
    case 'starting':
    case 'stopping': return 'bg-tp-warning'
    case 'installing': return 'bg-tp-primary'
    default: return 'bg-tp-surface-highest'
  }
})

const statusDotColor = computed(() => {
  switch (props.server.status) {
    case 'running': return 'bg-tp-success'
    case 'crashed': return 'bg-tp-danger'
    case 'starting':
    case 'stopping': return 'bg-tp-warning'
    case 'installing': return 'bg-tp-primary'
    default: return 'bg-tp-muted'
  }
})
</script>

<template>
  <div
    class="group bg-tp-surface2 rounded-xl hover:bg-tp-surface3 transition-all duration-200 cursor-pointer overflow-hidden"
    @click="navigateToServer"
  >
    <!-- Status bar top -->
    <div :class="['h-0.5 w-full transition-colors duration-300', statusBarColor]" />

    <div class="p-5">
      <!-- Header -->
      <div class="flex items-start justify-between gap-3 mb-4">
        <div class="flex items-center gap-3 min-w-0">
          <!-- Status dot -->
          <div class="shrink-0 relative">
            <div :class="['w-2.5 h-2.5 rounded-full', statusDotColor]" />
            <div
              v-if="server.status === 'running'"
              class="absolute inset-0 rounded-full bg-tp-success animate-pulse-slow opacity-60"
            />
          </div>

          <div class="min-w-0">
            <h3 class="text-tp-text font-display font-semibold text-sm truncate">{{ server.name }}</h3>
            <p class="text-tp-muted text-xs truncate mt-0.5">v{{ server.hytale_version }}</p>
          </div>
        </div>

        <ServerStatusBadge :status="server.status" size="sm" />
      </div>

      <!-- Resource info -->
      <div class="grid grid-cols-2 gap-3 mb-4">
        <div class="bg-tp-surface rounded-xl p-3 flex items-center gap-2">
          <span class="material-symbols-outlined text-tp-muted text-base shrink-0">memory</span>
          <div class="min-w-0">
            <p class="text-[10px] text-tp-outline uppercase tracking-wide">CPU</p>
            <p class="text-xs text-tp-text font-medium truncate">{{ cpuDisplay }}</p>
          </div>
        </div>
        <div class="bg-tp-surface rounded-xl p-3 flex items-center gap-2">
          <span class="material-symbols-outlined text-tp-muted text-base shrink-0">storage</span>
          <div class="min-w-0">
            <p class="text-[10px] text-tp-outline uppercase tracking-wide">RAM</p>
            <p class="text-xs text-tp-text font-medium truncate">{{ ramDisplay }}</p>
          </div>
        </div>
      </div>

      <!-- Footer -->
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-1.5 text-tp-muted text-xs">
          <span class="material-symbols-outlined text-sm">group</span>
          <span>0 players</span>
        </div>

        <!-- Quick action button -->
        <button
          :class="[
            'flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-semibold transition-all duration-150',
            isRunning
              ? 'bg-tp-danger/15 text-tp-danger hover:bg-tp-danger/25'
              : isStopped
              ? 'bg-tp-success/15 text-tp-success hover:bg-tp-success/25'
              : 'bg-tp-surface3 text-tp-muted cursor-not-allowed',
          ]"
          :disabled="isTransitioning || actionLoading"
          @click="toggleServer"
        >
          <svg
            v-if="actionLoading"
            class="w-3 h-3 animate-spin"
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
          </svg>
          <span v-else-if="isStopped" class="material-symbols-outlined text-sm">play_arrow</span>
          <span v-else-if="isRunning" class="material-symbols-outlined text-sm">stop</span>
          <span>{{ isRunning ? 'Stop' : isStopped ? 'Start' : server.status }}</span>
        </button>
      </div>
    </div>
  </div>
</template>
