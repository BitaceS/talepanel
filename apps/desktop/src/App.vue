<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { invoke } from '@tauri-apps/api/core'
import { Server, Monitor, LogOut, LayoutDashboard } from 'lucide-vue-next'

const router = useRouter()
const connected = ref(false)
const currentRoute = ref('')

router.afterEach((to) => {
  currentRoute.value = to.path
})

onMounted(async () => {
  try {
    connected.value = await invoke<boolean>('get_connection_status')
    if (!connected.value) {
      router.push('/connect')
    }
  } catch {
    router.push('/connect')
  }
})

async function handleDisconnect() {
  await invoke('disconnect')
  connected.value = false
  router.push('/connect')
}
</script>

<template>
  <div class="flex h-screen bg-tp-bg">
    <!-- Sidebar -->
    <aside v-if="connected" class="w-56 bg-tp-surface border-r border-tp-border flex flex-col">
      <div class="p-4 border-b border-tp-border">
        <h1 class="text-tp-primary font-bold text-lg tracking-tight">TalePanel</h1>
        <p class="text-tp-muted text-[10px] mt-0.5">Desktop Client</p>
      </div>

      <nav class="flex-1 p-2 space-y-1">
        <router-link
          v-for="item in [
            { to: '/dashboard', label: 'Dashboard', icon: LayoutDashboard },
            { to: '/servers', label: 'Servers', icon: Server },
          ]"
          :key="item.to"
          :to="item.to"
          :class="['flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors',
            currentRoute.startsWith(item.to) ? 'bg-tp-primary/10 text-tp-primary' : 'text-tp-muted hover:text-tp-text hover:bg-tp-surface2']"
        >
          <component :is="item.icon" class="w-4 h-4" />
          {{ item.label }}
        </router-link>
      </nav>

      <div class="p-2 border-t border-tp-border">
        <button
          class="flex items-center gap-3 px-3 py-2 rounded-lg text-sm text-tp-muted hover:text-tp-danger hover:bg-tp-danger/10 w-full transition-colors"
          @click="handleDisconnect"
        >
          <LogOut class="w-4 h-4" />
          Disconnect
        </button>
      </div>
    </aside>

    <!-- Main content -->
    <main class="flex-1 overflow-y-auto">
      <router-view />
    </main>
  </div>
</template>
