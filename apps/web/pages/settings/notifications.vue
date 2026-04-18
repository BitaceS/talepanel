<script setup lang="ts">
definePageMeta({ title: 'Notification Preferences', middleware: 'auth' })

interface NotificationPref {
  id: string
  user_id: string
  alert_type: string
  email: boolean
  discord: boolean
  telegram: boolean
}

interface NotificationsResponse {
  preferences: NotificationPref[]
}

const ALERT_TYPES: { key: string; label: string }[] = [
  { key: 'login_new_device', label: 'New device login' },
  { key: 'server_down',      label: 'Server offline' },
  { key: 'backup_failed',    label: 'Backup failed' },
  { key: 'alert_event',      label: 'Alert rule triggered' },
]

const api = useApi()
const toast = ref('')
const toastType = ref<'success' | 'error'>('success')

// Map keyed by alert_type for quick lookup
const prefsMap = reactive<Record<string, { email: boolean; discord: boolean; telegram: boolean }>>({})

function showToast(msg: string, type: 'success' | 'error' = 'success') {
  toast.value = msg
  toastType.value = type
  setTimeout(() => { toast.value = '' }, 3000)
}

onMounted(async () => {
  try {
    const data = await api.get<NotificationsResponse>('/auth/profile/notifications')
    for (const pref of data.preferences) {
      prefsMap[pref.alert_type] = {
        email: pref.email,
        discord: pref.discord,
        telegram: pref.telegram,
      }
    }
  } catch {
    showToast('Failed to load notification preferences', 'error')
  }
})

function getPref(alertType: string) {
  if (!prefsMap[alertType]) {
    prefsMap[alertType] = { email: false, discord: false, telegram: false }
  }
  return prefsMap[alertType]
}

async function onToggle(alertType: string) {
  const pref = getPref(alertType)
  try {
    await api.put('/auth/profile/notifications', {
      alert_type: alertType,
      email: pref.email,
      discord: pref.discord,
      telegram: pref.telegram,
    })
  } catch {
    showToast('Failed to save preference', 'error')
  }
}
</script>

<template>
  <div class="p-6 max-w-2xl">
    <h2 class="text-tp-text font-bold text-2xl mb-6">Notification Preferences</h2>

    <div class="bg-tp-surface rounded-xl border border-tp-border overflow-hidden">
      <div class="px-5 py-4 border-b border-tp-border">
        <h3 class="text-tp-text font-semibold text-sm">Notification Channels</h3>
      </div>

      <!-- Column headers -->
      <div class="grid grid-cols-4 gap-4 px-5 py-3 border-b border-tp-border">
        <span class="text-tp-muted text-xs font-medium uppercase tracking-wide">Event</span>
        <span class="text-tp-muted text-xs font-medium uppercase tracking-wide text-center">Email</span>
        <span class="text-tp-muted text-xs font-medium uppercase tracking-wide text-center">Discord</span>
        <span class="text-tp-muted text-xs font-medium uppercase tracking-wide text-center">Telegram</span>
      </div>

      <!-- Rows -->
      <div
        v-for="(type, index) in ALERT_TYPES"
        :key="type.key"
        :class="[
          'grid grid-cols-4 gap-4 items-center px-5 py-4',
          index < ALERT_TYPES.length - 1 ? 'border-b border-tp-border' : '',
        ]"
      >
        <span class="text-tp-text text-sm">{{ type.label }}</span>

        <!-- Email -->
        <div class="flex justify-center">
          <input
            type="checkbox"
            v-model="getPref(type.key).email"
            @change="onToggle(type.key)"
            class="w-4 h-4 rounded border border-tp-border bg-tp-surface2 accent-tp-primary cursor-pointer"
          />
        </div>

        <!-- Discord -->
        <div class="flex justify-center">
          <input
            type="checkbox"
            v-model="getPref(type.key).discord"
            @change="onToggle(type.key)"
            class="w-4 h-4 rounded border border-tp-border bg-tp-surface2 accent-tp-primary cursor-pointer"
          />
        </div>

        <!-- Telegram -->
        <div class="flex justify-center">
          <input
            type="checkbox"
            v-model="getPref(type.key).telegram"
            @change="onToggle(type.key)"
            class="w-4 h-4 rounded border border-tp-border bg-tp-surface2 accent-tp-primary cursor-pointer"
          />
        </div>
      </div>
    </div>

    <Transition name="toast">
      <div v-if="toast" :class="[
        'fixed bottom-6 right-6 z-50 flex items-center gap-3 px-4 py-3 rounded-xl border shadow-lg text-sm font-medium',
        toastType === 'success' ? 'bg-tp-success/10 border-tp-success/20 text-tp-success' : 'bg-tp-danger/10 border-tp-danger/20 text-tp-danger',
      ]">
        {{ toast }}
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.toast-enter-active, .toast-leave-active { transition: opacity 0.25s ease, transform 0.25s ease; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateY(8px); }
</style>
