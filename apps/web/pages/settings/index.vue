<script setup lang="ts">
import { User, Lock, Shield, Bell, Puzzle, KeyRound } from '@lucide/vue'
import { useAuthStore } from '~/stores/auth'
import { useModulesStore } from '~/stores/modules'
import { useApi } from '~/composables/useApi'

definePageMeta({ title: 'pages.settings', middleware: 'auth' })

const authStore = useAuthStore()
const modulesStore = useModulesStore()
const api = useApi()
const { t, locale } = useI18n()

type Section = 'profile' | 'security' | 'api-keys' | 'notifications' | 'modules'
const activeSection = ref<Section>('profile')

const isAdmin = computed(() => authStore.user?.role === 'admin' || authStore.user?.role === 'owner')

const sections = computed(() => {
  const base = [
    { id: 'profile' as Section,       label: t('settings.index.sections.profile'),       icon: User },
    { id: 'security' as Section,      label: t('settings.index.sections.security'),      icon: Lock },
    { id: 'api-keys' as Section,      label: t('settings.index.sections.apiKeys'),      icon: KeyRound },
    { id: 'notifications' as Section, label: t('settings.index.sections.notifications'), icon: Bell },
  ]
  if (isAdmin.value) {
    base.push({ id: 'modules' as Section, label: t('settings.index.sections.modules'), icon: Puzzle })
  }
  return base
})

// ── Toast ─────────────────────────────────────────────────────────────────────
const toast = ref('')
const toastType = ref<'success' | 'error'>('success')
let toastTimer: ReturnType<typeof setTimeout>

function showToast(msg: string, type: 'success' | 'error' = 'success') {
  toast.value = msg
  toastType.value = type
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toast.value = '' }, 3000)
}

// ── Change Password ───────────────────────────────────────────────────────────
const pwForm = reactive({ old_password: '', new_password: '', confirm: '' })
const pwLoading = ref(false)
const pwError = ref('')

async function changePassword() {
  pwError.value = ''
  if (pwForm.new_password !== pwForm.confirm) {
    pwError.value = t('settings.password.mismatch')
    return
  }
  if (pwForm.new_password.length < 8) {
    pwError.value = t('settings.password.tooShort')
    return
  }
  pwLoading.value = true
  try {
    await api.patch('/auth/password', {
      old_password: pwForm.old_password,
      new_password: pwForm.new_password,
    })
    Object.assign(pwForm, { old_password: '', new_password: '', confirm: '' })
    showToast(t('settings.password.changed'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    pwError.value = e.data?.error ?? e.message ?? t('settings.password.changeFailed')
  } finally {
    pwLoading.value = false
  }
}

// ── 2FA ────────────────────────────────────────────────────────────────────────
// Real setup/disable flow lives in pages/settings/security.vue — this page
// only ships deep-links to it.
function setup2FA() {
  navigateTo('/settings/security')
}
function disable2FA() {
  navigateTo('/settings/security')
}

// ── Notifications ──────────────────────────────────────────────────────────────
// Backed by GET/PUT /auth/profile/notifications.  Each row corresponds to one
// alert_type and tracks email/discord/telegram channels; the UI currently
// exposes a single enable/disable toggle that we write to all three channels.
interface NotifPref {
  key: string
  label: string
  desc: string
  enabled: boolean
}

const notifPrefs = reactive<NotifPref[]>([
  { key: 'login_new_device', label: 'settings.index.notif.loginNewDevice.label', desc: 'settings.index.notif.loginNewDevice.desc', enabled: true },
  { key: 'crash',    label: 'settings.index.notif.crash.label',    desc: 'settings.index.notif.crash.desc',    enabled: true },
  { key: 'backup',   label: 'settings.index.notif.backup.label',   desc: 'settings.index.notif.backup.desc',   enabled: true },
  { key: 'resource', label: 'settings.index.notif.resource.label', desc: 'settings.index.notif.resource.desc', enabled: false },
  { key: 'ddos',     label: 'settings.index.notif.ddos.label',     desc: 'settings.index.notif.ddos.desc',     enabled: true },
  { key: 'player',   label: 'settings.index.notif.player.label',   desc: 'settings.index.notif.player.desc',   enabled: false },
])

interface BackendNotifPref {
  alert_type: string
  email: boolean
  discord: boolean
  telegram: boolean
}

async function saveNotifPrefs() {
  const writes = notifPrefs.map(p =>
    api.put('/auth/profile/notifications', {
      alert_type: p.key,
      email:      p.enabled,
      discord:    p.enabled,
      telegram:   p.enabled,
    }),
  )
  try {
    await Promise.all(writes)
    showToast(t('settings.notifications.saved'))
  } catch (err) {
    showToast((err as Error).message || t('settings.notifications.saveFailed'), 'error')
  }
}

async function loadNotifPrefs() {
  try {
    const res = await api.get<{ preferences: BackendNotifPref[] }>('/auth/profile/notifications')
    for (const backend of res.preferences ?? []) {
      const local = notifPrefs.find(p => p.key === backend.alert_type)
      if (local) local.enabled = backend.email || backend.discord || backend.telegram
    }
  } catch {
    // Backend may not have preferences yet — keep defaults.
  }
}

onMounted(() => {
  void loadNotifPrefs()
})
</script>

<template>
  <div class="p-6">
    <h2 class="text-tp-text font-display font-bold text-2xl mb-6">{{ t('nav.settings') }}</h2>

    <div class="flex flex-col sm:flex-row gap-6">
      <!-- Sidebar nav -->
      <nav class="sm:w-48 shrink-0 flex sm:flex-col gap-1">
        <button
          v-for="s in sections" :key="s.id"
          :class="[
            'flex items-center gap-2.5 px-3 py-2 rounded-xl text-sm font-medium w-full text-left transition-colors',
            activeSection === s.id
              ? 'bg-tp-primary/10 text-tp-primary'
              : 'text-tp-muted hover:text-tp-text hover:bg-tp-surface2',
          ]"
          @click="activeSection = s.id"
        >
          <component :is="s.icon" class="w-4 h-4 shrink-0" />
          {{ s.label }}
        </button>
      </nav>

      <!-- Content -->
      <div class="flex-1 min-w-0 space-y-5">

        <!-- ── Profile ─────────────────────────────────────────────────────── -->
        <template v-if="activeSection === 'profile'">
          <div class="bg-tp-surface rounded-xl overflow-hidden">
            <div class="px-5 py-3 border-b border-tp-border">
              <h3 class="text-tp-text font-display font-semibold text-sm">{{ t('settings.index.accountInfo') }}</h3>
            </div>
            <div class="divide-y divide-tp-border">
              <div v-for="row in [
                { label: t('settings.index.username'), value: authStore.user?.username },
                { label: t('settings.index.email'), value: authStore.user?.email },
                { label: t('settings.index.role'), value: authStore.user?.role ? t(`roles.${authStore.user.role}`) : undefined },
                { label: t('settings.index.memberSince'), value: authStore.user?.created_at ? new Date(authStore.user.created_at).toLocaleDateString(locale) : '—' },
                { label: '2FA', value: authStore.user?.totp_enabled ? t('settings.index.enabled') : t('settings.index.disabled') },
              ]" :key="row.label" class="flex items-center px-5 py-3">
                <span class="text-tp-outline text-sm w-36 shrink-0">{{ row.label }}</span>
                <span class="text-tp-text text-sm capitalize">{{ row.value ?? '—' }}</span>
              </div>
            </div>
          </div>
        </template>

        <!-- ── Security ───────────────────────────────────────────────────── -->
        <template v-else-if="activeSection === 'security'">
          <div class="bg-tp-surface rounded-xl overflow-hidden">
            <div class="px-5 py-4 border-b border-tp-border">
              <h3 class="text-tp-text font-display font-semibold text-sm">{{ t('settings.password.title') }}</h3>
              <p class="text-tp-muted text-xs mt-0.5">{{ t('settings.password.subtitle') }}</p>
            </div>
            <div class="p-5 space-y-4">
              <div v-if="pwError" class="bg-tp-error/10 rounded-xl px-3 py-2.5 text-tp-error text-sm">
                {{ pwError }}
              </div>
              <UiInput
                v-model="pwForm.old_password"
                type="password"
                :label="t('settings.password.current')"
                placeholder="••••••••"
              />
              <UiInput
                v-model="pwForm.new_password"
                type="password"
                :label="t('settings.password.new')"
                placeholder="••••••••"
              />
              <UiInput
                v-model="pwForm.confirm"
                type="password"
                :label="t('settings.password.confirm')"
                placeholder="••••••••"
              />
              <div class="flex justify-end pt-1">
                <UiButton
                  variant="primary"
                  size="md"
                  :loading="pwLoading"
                  :disabled="!pwForm.old_password || !pwForm.new_password || !pwForm.confirm"
                  @click="changePassword"
                >
                  <Lock class="w-4 h-4" />
                  {{ t('settings.password.title') }}
                </UiButton>
              </div>
            </div>
          </div>

          <div class="bg-tp-surface rounded-xl overflow-hidden">
            <div class="px-5 py-4 border-b border-tp-border">
              <h3 class="text-tp-text font-display font-semibold text-sm">{{ t('settings.twoFactor.title') }}</h3>
              <i18n-t keypath="settings.twoFactor.currently" tag="p" class="text-tp-muted text-xs mt-0.5" scope="global">
                <template #state>
                  <span :class="authStore.user?.totp_enabled ? 'text-tp-success' : 'text-tp-muted'">
                    {{ authStore.user?.totp_enabled ? t('settings.twoFactor.stateEnabled') : t('settings.twoFactor.stateDisabled') }}
                  </span>
                </template>
              </i18n-t>
            </div>
            <div class="px-5 py-4 flex items-center justify-between">
              <div class="flex items-center gap-3">
                <div :class="['w-9 h-9 rounded-xl flex items-center justify-center', authStore.user?.totp_enabled ? 'bg-tp-success/10' : 'bg-tp-surface2']">
                  <Shield :class="['w-4 h-4', authStore.user?.totp_enabled ? 'text-tp-success' : 'text-tp-muted']" />
                </div>
                <div>
                  <p class="text-tp-text text-sm font-medium">{{ t('settings.twoFactor.app') }}</p>
                  <p class="text-tp-muted text-xs">{{ t('settings.twoFactor.appHint') }}</p>
                </div>
              </div>
              <UiButton v-if="!authStore.user?.totp_enabled" variant="primary" size="sm" @click="setup2FA">
                <Shield class="w-3.5 h-3.5" /> {{ t('settings.twoFactor.enable') }}
              </UiButton>
              <UiButton v-else variant="danger" size="sm" @click="disable2FA">
                {{ t('settings.twoFactor.disable') }}
              </UiButton>
            </div>
          </div>
        </template>

        <!-- ── Notifications ──────────────────────────────────────────────── -->
        <!-- ── API keys ───────────────────────────────────────────────────── -->
        <template v-else-if="activeSection === 'api-keys'">
          <SettingsApiKeysPanel @toast="showToast" />
        </template>

        <template v-else-if="activeSection === 'notifications'">
          <div class="bg-tp-surface rounded-xl overflow-hidden">
            <div class="px-5 py-4 border-b border-tp-border">
              <h3 class="text-tp-text font-display font-semibold text-sm">{{ t('pages.notifications') }}</h3>
              <p class="text-tp-muted text-xs mt-0.5">{{ t('settings.index.notifSubtitle') }}</p>
            </div>
            <div class="divide-y divide-tp-border">
              <label v-for="pref in notifPrefs" :key="pref.key" class="flex items-center justify-between px-5 py-4 cursor-pointer hover:bg-tp-surface2/50 transition-colors">
                <div>
                  <p class="text-tp-text text-sm font-medium">{{ t(pref.label) }}</p>
                  <p class="text-tp-muted text-xs mt-0.5">{{ t(pref.desc) }}</p>
                </div>
                <input type="checkbox" v-model="pref.enabled" @change="saveNotifPrefs"
                  class="w-4 h-4 rounded bg-tp-surface2 border border-tp-border text-tp-primary focus:ring-tp-primary/50" />
              </label>
            </div>
          </div>
        </template>

        <!-- ── Modules ─────────────────────────────────────────────────────── -->
        <template v-else-if="activeSection === 'modules'">
          <!-- Deployment profile selector -->
          <div class="bg-tp-surface rounded-xl overflow-hidden mb-4">
            <div class="px-5 py-4 border-b border-tp-border">
              <h3 class="text-tp-text font-display font-semibold text-sm">{{ t('settings.index.profileTitle') }}</h3>
              <p class="text-tp-muted text-xs mt-0.5">{{ t('settings.index.profileHint') }}</p>
            </div>
            <div class="px-5 py-4 flex gap-2">
              <button type="button"
                @click="modulesStore.applyProfile('solo')"
                :class="['px-3 py-1.5 rounded-lg text-sm font-medium border transition-colors',
                  modulesStore.profile === 'solo'
                    ? 'bg-tp-primary/15 border-tp-primary text-tp-primary'
                    : 'bg-tp-surface2 border-tp-border text-tp-muted hover:text-tp-text']">
                {{ t('settings.index.profileSolo') }}
              </button>
              <button type="button"
                @click="modulesStore.applyProfile('hoster')"
                :class="['px-3 py-1.5 rounded-lg text-sm font-medium border transition-colors',
                  modulesStore.profile === 'hoster'
                    ? 'bg-tp-primary/15 border-tp-primary text-tp-primary'
                    : 'bg-tp-surface2 border-tp-border text-tp-muted hover:text-tp-text']">
                {{ t('settings.index.profileHoster') }}
              </button>
            </div>
          </div>

          <div class="bg-tp-surface rounded-xl overflow-hidden">
            <div class="px-5 py-4 border-b border-tp-border">
              <h3 class="text-tp-text font-display font-semibold text-sm">{{ t('settings.index.modulesTitle') }}</h3>
              <p class="text-tp-muted text-xs mt-0.5">{{ t('settings.index.modulesHint') }}</p>
            </div>
            <div class="divide-y divide-tp-border">
              <label v-for="mod in modulesStore.modules" :key="mod.id"
                class="flex items-center justify-between px-5 py-4 cursor-pointer hover:bg-tp-surface2/50 transition-colors">
                <div class="flex-1 min-w-0">
                  <div class="flex items-center gap-2">
                    <p class="text-tp-text text-sm font-medium">{{ mod.label }}</p>
                    <span v-if="mod.hosterOnly" class="text-xs px-1.5 py-0.5 rounded-full bg-tp-warning/10 text-tp-warning">{{ t('settings.index.hosterBadge') }}</span>
                  </div>
                  <p class="text-tp-muted text-xs mt-0.5">{{ mod.description }}</p>
                </div>
                <input type="checkbox" :checked="mod.enabled" @change="modulesStore.toggle(mod.id, !mod.enabled)"
                  class="w-4 h-4 rounded bg-tp-surface2 border border-tp-border text-tp-primary focus:ring-tp-primary/50 shrink-0 ml-4" />
              </label>
            </div>
          </div>
        </template>

      </div>
    </div>

    <!-- Toast -->
    <Transition name="toast">
      <div v-if="toast" :class="[
        'fixed bottom-6 right-6 z-50 flex items-center gap-3 px-4 py-3 rounded-xl shadow-lg text-sm font-medium',
        toastType === 'success' ? 'bg-tp-success/15 text-tp-success' : 'bg-tp-error/10 text-tp-error',
      ]">
        <div :class="['w-2 h-2 rounded-full', toastType === 'success' ? 'bg-tp-success' : 'bg-tp-danger']" />
        {{ toast }}
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.toast-enter-active, .toast-leave-active { transition: opacity 0.25s ease, transform 0.25s ease; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateY(8px); }
</style>
