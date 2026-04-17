<script setup lang="ts">
import { useProfileStore } from '~/stores/profile'

definePageMeta({ title: 'Profile Settings', middleware: 'auth' })

const profileStore = useProfileStore()
const toast = ref('')
const toastType = ref<'success' | 'error'>('success')

const form = reactive({
  display_name: '',
  language: 'en',
  timezone: 'UTC',
})

onMounted(async () => {
  await profileStore.fetchProfile()
  if (profileStore.profile) {
    form.display_name = profileStore.profile.display_name || ''
    form.language = profileStore.profile.language || 'en'
    form.timezone = profileStore.profile.timezone || 'UTC'
  }
})

function showToast(msg: string, type: 'success' | 'error' = 'success') {
  toast.value = msg
  toastType.value = type
  setTimeout(() => { toast.value = '' }, 3000)
}

const saving = ref(false)
async function saveProfile() {
  saving.value = true
  try {
    await profileStore.updateProfile({
      display_name: form.display_name,
      language: form.language,
      timezone: form.timezone,
    })
    showToast('Profile updated')
  } catch {
    showToast('Failed to update profile', 'error')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="p-6 max-w-2xl">
    <h2 class="text-tp-text font-bold text-2xl mb-6">Profile Settings</h2>

    <div class="bg-tp-surface rounded-xl border border-tp-border overflow-hidden">
      <div class="px-5 py-4 border-b border-tp-border">
        <h3 class="text-tp-text font-semibold text-sm">Profile Information</h3>
      </div>
      <div class="p-5 space-y-4">
        <div>
          <label class="block text-sm text-tp-muted mb-1">Display Name</label>
          <input
            v-model="form.display_name"
            type="text"
            class="w-full bg-tp-surface2 border border-tp-border rounded-lg px-3 py-2 text-tp-text text-sm"
            placeholder="Your display name"
          />
        </div>
        <div>
          <label class="block text-sm text-tp-muted mb-1">Language</label>
          <select v-model="form.language" class="w-full bg-tp-surface2 border border-tp-border rounded-lg px-3 py-2 text-tp-text text-sm">
            <option value="en">English</option>
            <option value="de">Deutsch</option>
            <option value="fr">Francais</option>
            <option value="es">Espanol</option>
          </select>
        </div>
        <div>
          <label class="block text-sm text-tp-muted mb-1">Timezone</label>
          <select v-model="form.timezone" class="w-full bg-tp-surface2 border border-tp-border rounded-lg px-3 py-2 text-tp-text text-sm">
            <option value="UTC">UTC</option>
            <option value="America/New_York">America/New_York (EST)</option>
            <option value="America/Chicago">America/Chicago (CST)</option>
            <option value="America/Denver">America/Denver (MST)</option>
            <option value="America/Los_Angeles">America/Los_Angeles (PST)</option>
            <option value="Europe/London">Europe/London (GMT)</option>
            <option value="Europe/Berlin">Europe/Berlin (CET)</option>
            <option value="Asia/Tokyo">Asia/Tokyo (JST)</option>
          </select>
        </div>
        <div class="flex justify-end pt-2">
          <button
            @click="saveProfile"
            :disabled="saving"
            class="px-4 py-2 bg-tp-primary text-white rounded-lg text-sm font-medium hover:bg-tp-primary/90 disabled:opacity-50"
          >
            {{ saving ? 'Saving...' : 'Save Changes' }}
          </button>
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
