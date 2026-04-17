<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { invoke } from '@tauri-apps/api/core'
import { Loader2 } from 'lucide-vue-next'

const router = useRouter()

const apiUrl = ref('https://193.46.81.100:8443')
const email = ref('')
const password = ref('')
const loading = ref(false)
const error = ref('')

async function connect() {
  error.value = ''
  loading.value = true
  try {
    const result = await invoke<{ success: boolean; username: string | null; error: string | null }>(
      'connect_to_panel',
      { req: { api_url: apiUrl.value, email: email.value, password: password.value } }
    )
    if (result.success) {
      router.push('/dashboard')
    } else {
      error.value = result.error || 'Login failed'
    }
  } catch (e: any) {
    error.value = e?.toString() || 'Connection failed'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="flex items-center justify-center h-screen bg-tp-bg">
    <div class="w-full max-w-sm">
      <div class="text-center mb-8">
        <h1 class="text-tp-primary font-bold text-3xl tracking-tight">TalePanel</h1>
        <p class="text-tp-muted text-sm mt-2">Connect to your server panel</p>
      </div>

      <form @submit.prevent="connect" class="bg-tp-surface rounded-xl border border-tp-border p-6 space-y-4">
        <div>
          <label class="block text-tp-muted text-xs font-medium mb-1.5">Panel URL</label>
          <input
            v-model="apiUrl"
            type="text"
            placeholder="https://panel.example.com"
            class="w-full bg-tp-surface2 border border-tp-border rounded-lg px-3 py-2 text-tp-text text-sm focus:outline-none focus:border-tp-primary transition-colors"
          />
        </div>

        <div>
          <label class="block text-tp-muted text-xs font-medium mb-1.5">Email</label>
          <input
            v-model="email"
            type="email"
            placeholder="admin@example.com"
            autocomplete="email"
            class="w-full bg-tp-surface2 border border-tp-border rounded-lg px-3 py-2 text-tp-text text-sm focus:outline-none focus:border-tp-primary transition-colors"
          />
        </div>

        <div>
          <label class="block text-tp-muted text-xs font-medium mb-1.5">Password</label>
          <input
            v-model="password"
            type="password"
            placeholder="Password"
            autocomplete="current-password"
            class="w-full bg-tp-surface2 border border-tp-border rounded-lg px-3 py-2 text-tp-text text-sm focus:outline-none focus:border-tp-primary transition-colors"
          />
        </div>

        <div v-if="error" class="bg-tp-danger/10 border border-tp-danger/30 rounded-lg px-3 py-2">
          <p class="text-tp-danger text-xs">{{ error }}</p>
        </div>

        <button
          type="submit"
          :disabled="loading || !email || !password"
          class="w-full bg-tp-primary hover:bg-tp-primary/90 disabled:opacity-50 disabled:cursor-not-allowed text-white font-medium text-sm rounded-lg px-4 py-2.5 flex items-center justify-center gap-2 transition-colors"
        >
          <Loader2 v-if="loading" class="w-4 h-4 animate-spin" />
          {{ loading ? 'Connecting...' : 'Connect' }}
        </button>
      </form>
    </div>
  </div>
</template>
