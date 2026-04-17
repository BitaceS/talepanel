<script setup lang="ts">
import type { ActivityLog, Session } from '~/types'

definePageMeta({ title: 'Activity & Sessions', middleware: 'auth' })

const api = useApi()

const activeTab = ref<'activity' | 'sessions'>('activity')
const logs = ref<ActivityLog[]>([])
const sessions = ref<Session[]>([])
const loading = ref(false)

async function fetchActivity() {
  loading.value = true
  try {
    const data = await api.get<{ logs: ActivityLog[] }>('/auth/activity')
    logs.value = data.logs
  } catch { /* ignore */ }
  loading.value = false
}

async function fetchSessions() {
  loading.value = true
  try {
    const data = await api.get<{ sessions: Session[] }>('/auth/sessions')
    sessions.value = data.sessions
  } catch { /* ignore */ }
  loading.value = false
}

async function revokeSession(id: string) {
  await api.delete(`/auth/sessions/${id}`)
  sessions.value = sessions.value.filter(s => s.id !== id)
}

onMounted(() => {
  fetchActivity()
  fetchSessions()
})
</script>

<template>
  <div class="p-6 max-w-4xl">
    <h2 class="text-tp-text font-bold text-2xl mb-6">Activity & Sessions</h2>

    <div class="flex gap-2 mb-4">
      <button
        v-for="tab in ['activity', 'sessions'] as const" :key="tab"
        @click="activeTab = tab"
        :class="[
          'px-3 py-1.5 rounded-lg text-sm font-medium',
          activeTab === tab ? 'bg-tp-primary/10 text-tp-primary' : 'text-tp-muted hover:text-tp-text',
        ]"
      >
        {{ tab === 'activity' ? 'Recent Activity' : 'Active Sessions' }}
      </button>
    </div>

    <div v-if="activeTab === 'activity'" class="bg-tp-surface rounded-xl border border-tp-border overflow-hidden">
      <div v-if="loading" class="p-8 text-center text-tp-muted text-sm">Loading...</div>
      <div v-else-if="logs.length === 0" class="p-8 text-center text-tp-muted text-sm">No recent activity</div>
      <div v-else class="divide-y divide-tp-border">
        <div v-for="log in logs" :key="log.id" class="px-5 py-3 flex items-center gap-4">
          <div class="flex-1 min-w-0">
            <p class="text-tp-text text-sm">{{ log.action }}</p>
            <p class="text-tp-muted text-xs">{{ log.ip_address }} &middot; {{ new Date(log.created_at).toLocaleString() }}</p>
          </div>
        </div>
      </div>
    </div>

    <div v-if="activeTab === 'sessions'" class="bg-tp-surface rounded-xl border border-tp-border overflow-hidden">
      <div v-if="loading" class="p-8 text-center text-tp-muted text-sm">Loading...</div>
      <div v-else-if="sessions.length === 0" class="p-8 text-center text-tp-muted text-sm">No active sessions</div>
      <div v-else class="divide-y divide-tp-border">
        <div v-for="sess in sessions" :key="sess.id" class="px-5 py-3 flex items-center justify-between gap-4">
          <div class="flex-1 min-w-0">
            <p class="text-tp-text text-sm truncate">{{ sess.user_agent || 'Unknown device' }}</p>
            <p class="text-tp-muted text-xs">{{ sess.ip_address }} &middot; Created {{ new Date(sess.created_at).toLocaleString() }}</p>
          </div>
          <button
            @click="revokeSession(sess.id)"
            class="text-tp-danger text-xs font-medium hover:text-tp-danger/80"
          >
            Revoke
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
