<script setup lang="ts">
import { KeyRound, Link2, RefreshCw, Shield, ShieldCheck } from '@lucide/vue'
import { useAuthStore } from '~/stores/auth'

definePageMeta({ title: 'pages.security', middleware: 'auth' })

const api = useApi()
const route = useRoute()
const authStore = useAuthStore()
const { t, locale } = useI18n()

// Set by the login flow after signing in with a recovery code.
const recoveryUsedNotice = ref(typeof route.query.recovery_used === 'string')

type User = { totp_enabled: boolean; email: string }
type RecoveryStatus = { enabled: boolean; remaining: number; total: number }

const me = ref<User | null>(null)
const busy = ref(false)
const toast = ref('')
const toastKind = ref<'ok' | 'error'>('ok')

function flash(msg: string, kind: 'ok' | 'error' = 'ok') {
  toast.value = msg
  toastKind.value = kind
  setTimeout(() => { toast.value = '' }, 3000)
}

function errorMessage(err: unknown, fallback: string): string {
  const e = err as { data?: { error?: string }; message?: string }
  return e.data?.error || e.message || fallback
}

onMounted(async () => {
  await refresh()
})

async function refresh() {
  const { user, two_factor_setup_required } = await api.get<{ user: User; two_factor_setup_required?: boolean }>('/auth/me')
  me.value = user
  authStore.twoFactorSetupRequired = !!two_factor_setup_required
  await loadRecoveryStatus()
}

// ── Recovery codes ────────────────────────────────────────────────────────
const recoveryStatus = ref<RecoveryStatus | null>(null)
// Plaintext codes, held only until the user dismisses them.
const shownCodes = ref<string[]>([])

async function loadRecoveryStatus() {
  if (!me.value?.totp_enabled) {
    recoveryStatus.value = null
    return
  }
  try {
    recoveryStatus.value = await api.get<RecoveryStatus>('/auth/2fa/recovery-codes/status')
  } catch {
    recoveryStatus.value = null
  }
}

const regenOpen = ref(false)
const regenPassword = ref('')
const regenCode = ref('')

const canRegenerate = computed(() => !!regenPassword.value && /^\d{6}$/.test(regenCode.value.trim()))

function closeRegen() {
  regenOpen.value = false
  regenPassword.value = ''
  regenCode.value = ''
}

async function regenerate() {
  busy.value = true
  try {
    const res = await api.post<{ recovery_codes: string[] }>('/auth/2fa/recovery-codes', {
      password: regenPassword.value,
      code: regenCode.value.trim(),
    })
    shownCodes.value = res.recovery_codes
    closeRegen()
    await loadRecoveryStatus()
    flash(t('settings.security.toast.codesRegenerated'))
  } catch (err) {
    flash(errorMessage(err, t('settings.security.toast.codesFailed')), 'error')
  } finally {
    busy.value = false
  }
}

function dismissCodes() {
  shownCodes.value = []
}

// ── Setup flow ────────────────────────────────────────────────────────────
const setupState = ref<'idle' | 'pending'>('idle')
const setupData = ref<{ otpauth_uri: string; qr_code_base64: string; secret: string } | null>(null)
const setupCode = ref('')

async function startSetup() {
  busy.value = true
  try {
    setupData.value = await api.post<typeof setupData.value>('/auth/totp/setup')
    setupState.value = 'pending'
  } catch (err) {
    flash(errorMessage(err, t('settings.security.toast.setupFailed')), 'error')
  } finally {
    busy.value = false
  }
}

async function confirmSetup() {
  busy.value = true
  try {
    const res = await api.post<{ recovery_codes?: string[] }>('/auth/totp/confirm', { code: setupCode.value })
    shownCodes.value = res.recovery_codes ?? []
    setupState.value = 'idle'
    setupData.value = null
    setupCode.value = ''
    await refresh()
    flash(t('settings.security.toast.enabled'))
  } catch (err) {
    flash(errorMessage(err, t('settings.security.toast.invalidCode')), 'error')
  } finally {
    busy.value = false
  }
}

function cancelSetup() {
  setupState.value = 'idle'
  setupData.value = null
  setupCode.value = ''
}

// ── Disable flow ──────────────────────────────────────────────────────────
const disablePassword = ref('')
const disableOpen = ref(false)

function closeDisable() {
  disableOpen.value = false
  disablePassword.value = ''
}

async function disable() {
  busy.value = true
  try {
    await api.post('/auth/totp/disable', { password: disablePassword.value })
    closeDisable()
    shownCodes.value = []
    await refresh()
    flash(t('settings.security.toast.disabled'))
  } catch (err) {
    flash(errorMessage(err, t('settings.security.toast.passwordIncorrect')), 'error')
  } finally {
    busy.value = false
  }
}

// ── Sessions (Plan 3 Task 5.7) ────────────────────────────────────────────
type Session = { id: string; created_at: string; ip_address: string; user_agent: string }
const sessions = ref<Session[]>([])

async function loadSessions() {
  const res = await api.get<{ sessions: Session[] }>('/auth/sessions')
  sessions.value = res.sessions || []
}

async function revokeSession(id: string) {
  if (!confirm(t('settings.security.revokeConfirm'))) return
  await api.delete(`/auth/sessions/${id}`)
  await loadSessions()
  flash(t('settings.security.toast.sessionRevoked'))
}

onMounted(() => loadSessions())

// ── Single sign-on ────────────────────────────────────────────────────────
// Connecting is the only way an existing account gets SSO: the API never
// links by a matching email, since registration does not prove the address.
const sso = ref<{ enabled: boolean, display_name?: string }>({ enabled: false })
const ssoLinked = ref(false)
const SSO_LINK_ERRORS = ['denied', 'state', 'domain', 'taken', 'inactive', 'disabled', 'failed']

async function loadSso() {
  try {
    sso.value = await api.get<{ enabled: boolean, display_name?: string }>('/auth/oidc')
    if (sso.value.enabled) {
      ssoLinked.value = (await api.get<{ linked: boolean }>('/auth/oidc/link')).linked
    }
  } catch {
    sso.value = { enabled: false }
  }
}

async function connectSso() {
  busy.value = true
  try {
    const { url } = await api.post<{ url: string }>('/auth/oidc/link')
    window.location.href = url
  } catch (err) {
    flash(errorMessage(err, t('auth.sso.errors.failed')), 'error')
    busy.value = false
  }
}

// The connect flow returns with ?sso=linked or ?sso_error=<code>.
function readSsoReturn() {
  const ok = route.query.sso === 'linked'
  const code = route.query.sso_error
  if (ok) flash(t('settings.security.sso.connectedToast'))
  if (typeof code === 'string') {
    flash(t(`auth.sso.errors.${SSO_LINK_ERRORS.includes(code) ? code : 'failed'}`), 'error')
  }
  if (ok || code) history.replaceState(history.state, '', window.location.pathname)
}

onMounted(() => {
  readSsoReturn()
  loadSso()
})
</script>

<template>
  <div class="max-w-2xl mx-auto py-8 space-y-6">
    <header>
      <h1 class="text-2xl font-display font-bold text-tp-text">{{ t('pages.security') }}</h1>
      <p class="text-sm text-tp-muted">{{ t('settings.security.subtitle') }}</p>
    </header>

    <!-- Toast -->
    <div
      v-if="toast"
      :class="[
        'rounded-xl px-4 py-2.5 text-sm',
        toastKind === 'ok' ? 'bg-tp-success/10 text-tp-success' : 'bg-tp-error/10 text-tp-error'
      ]"
    >
      {{ toast }}
    </div>

    <!-- Staff 2FA policy: set up 2FA before anything else works -->
    <div v-if="authStore.twoFactorSetupRequired" class="flex items-start gap-3 bg-tp-warning/10 rounded-xl p-4">
      <ShieldCheck class="w-4 h-4 text-tp-warning shrink-0 mt-0.5" />
      <div class="text-sm flex-1">
        <p class="text-tp-text font-semibold">{{ t('settings.security.requiredTitle') }}</p>
        <p class="text-tp-muted mt-0.5">
          {{ t('settings.security.requiredText') }}
        </p>
      </div>
    </div>

    <!-- Signed in with a recovery code -->
    <div v-if="recoveryUsedNotice" class="flex items-start gap-3 bg-tp-warning/10 rounded-xl p-4">
      <KeyRound class="w-4 h-4 text-tp-warning shrink-0 mt-0.5" />
      <div class="text-sm flex-1">
        <p class="text-tp-text font-semibold">{{ t('settings.security.recoveryUsedTitle') }}</p>
        <p class="text-tp-muted mt-0.5">
          {{ recoveryStatus ? t('settings.security.recoveryUsedLeft', { n: recoveryStatus.remaining }) : t('settings.security.recoveryUsed') }}
          {{ t('settings.security.recoveryUsedText') }}
        </p>
      </div>
      <button class="text-tp-muted hover:text-tp-text text-xs" @click="recoveryUsedNotice = false">{{ t('settings.security.dismiss') }}</button>
    </div>

    <!-- TOTP card -->
    <section class="bg-tp-surface rounded-xl overflow-hidden">
      <div class="px-5 py-4 border-b border-tp-border">
        <h2 class="text-tp-text font-display font-semibold text-sm">{{ t('auth.login.totpTitle') }}</h2>
        <p class="text-tp-muted text-xs mt-0.5">
          {{ t('settings.security.totpIntro') }}
        </p>
      </div>

      <div class="p-5 space-y-4">
        <!-- Freshly issued recovery codes (after enabling or regenerating) -->
        <AuthRecoveryCodes
          v-if="shownCodes.length"
          :codes="shownCodes"
          :email="me?.email"
          @done="dismissCodes"
        />

        <!-- Already enabled -->
        <template v-else-if="me?.totp_enabled">
          <div class="flex items-center gap-3">
            <div class="w-9 h-9 rounded-xl flex items-center justify-center bg-tp-success/10">
              <ShieldCheck class="w-4 h-4 text-tp-success" />
            </div>
            <p class="text-sm text-tp-text">
              <i18n-t keypath="settings.security.activeText" scope="global">
                <template #active>
                  <strong class="text-tp-success">{{ t('settings.security.active') }}</strong>
                </template>
              </i18n-t>
            </p>
          </div>

          <div class="bg-tp-surface2 rounded-xl p-4 flex flex-wrap items-center justify-between gap-3">
            <div class="flex items-center gap-3">
              <KeyRound class="w-4 h-4 text-tp-muted shrink-0" />
              <div>
                <p class="text-tp-text text-sm font-medium">{{ t('auth.recoveryCodes.label') }}</p>
                <p
                  v-if="recoveryStatus"
                  :class="[
                    'text-xs',
                    recoveryStatus.remaining <= 3 ? 'text-tp-warning' : 'text-tp-muted',
                  ]"
                >
                  {{ t('settings.security.unused', { remaining: recoveryStatus.remaining, total: recoveryStatus.total }) }}<template v-if="recoveryStatus.total === 0">{{ t('settings.security.unusedNone') }}</template>
                  <template v-else-if="recoveryStatus.remaining === 0">{{ t('settings.security.unusedZero') }}</template>
                  <template v-else-if="recoveryStatus.remaining <= 3">{{ t('settings.security.unusedLow') }}</template>
                </p>
              </div>
            </div>
            <UiButton variant="secondary" size="sm" :disabled="busy" @click="regenOpen = true">
              <RefreshCw class="w-3.5 h-3.5" /> {{ t('settings.security.generateNew') }}
            </UiButton>
          </div>

          <UiButton variant="danger" size="sm" :disabled="busy" @click="disableOpen = true">
            {{ t('settings.twoFactor.disable') }}
          </UiButton>
        </template>

        <!-- Not enabled, idle -->
        <template v-else-if="setupState === 'idle'">
          <p class="text-sm text-tp-muted">
            {{ t('settings.security.addApp') }}
          </p>
          <UiButton variant="primary" size="sm" :loading="busy" @click="startSetup">
            <Shield class="w-3.5 h-3.5" /> {{ t('settings.security.setup') }}
          </UiButton>
        </template>

        <!-- Setup in progress -->
        <div v-else-if="setupState === 'pending' && setupData" class="space-y-4">
          <p class="text-sm text-tp-text">
            {{ t('settings.security.scan') }}
          </p>
          <img
            :src="`data:image/png;base64,${setupData.qr_code_base64}`"
            :alt="t('settings.security.qrAlt')"
            class="w-48 h-48 rounded-xl bg-white p-2"
          />
          <details class="text-sm text-tp-muted">
            <summary class="cursor-pointer">{{ t('settings.security.cantScan') }}</summary>
            <code class="block mt-2 p-2 bg-tp-surface2 text-tp-text rounded-lg break-all font-mono">{{ setupData.secret }}</code>
          </details>

          <div class="flex items-end gap-3">
            <label class="flex-1 flex flex-col gap-1.5">
              <span class="text-xs font-semibold text-tp-muted uppercase tracking-wider">{{ t('settings.security.verificationCode') }}</span>
              <input
                v-model="setupCode"
                type="text"
                inputmode="numeric"
                autocomplete="one-time-code"
                pattern="[0-9]{6}"
                maxlength="6"
                placeholder="123456"
                class="w-full bg-tp-surface2 text-tp-text rounded-xl px-4 py-2.5 text-sm tracking-[0.3em] placeholder:text-tp-outline focus:outline-none focus:ring-2 focus:ring-tp-primary/50"
              />
            </label>
            <UiButton variant="primary" :loading="busy" :disabled="setupCode.length !== 6" @click="confirmSetup">
              {{ t('actions.confirm') }}
            </UiButton>
            <UiButton variant="ghost" :disabled="busy" @click="cancelSetup">
              {{ t('actions.cancel') }}
            </UiButton>
          </div>
        </div>
      </div>
    </section>

    <!-- Regenerate recovery codes -->
    <UiModal :open="regenOpen" :title="t('settings.security.regenTitle')" size="sm" @close="closeRegen">
      <form class="space-y-4" @submit.prevent="canRegenerate && !busy && regenerate()">
        <p class="text-sm text-tp-muted">
          {{ t('settings.security.regenText') }}
        </p>
        <UiInput
          v-model="regenPassword"
          type="password"
          :label="t('auth.fields.password')"
          autocomplete="current-password"
          placeholder="••••••••"
        />
        <UiInput
          v-model="regenCode"
          :label="t('settings.security.authenticatorCode')"
          autocomplete="one-time-code"
          placeholder="123456"
        />
        <!-- Lets Enter submit; the visible button lives in the modal footer. -->
        <button type="submit" class="sr-only" tabindex="-1" aria-hidden="true" />
      </form>
      <template #footer>
        <UiButton variant="ghost" @click="closeRegen">{{ t('actions.cancel') }}</UiButton>
        <UiButton
          variant="primary"
          :loading="busy"
          :disabled="!canRegenerate"
          @click="regenerate"
        >
          <RefreshCw class="w-3.5 h-3.5" /> {{ t('settings.security.generate') }}
        </UiButton>
      </template>
    </UiModal>

    <!-- Disable confirmation -->
    <UiModal :open="disableOpen" :title="t('settings.twoFactor.disable')" size="sm" @close="closeDisable">
      <form class="space-y-4" @submit.prevent="disablePassword && !busy && disable()">
        <p class="text-sm text-tp-muted">{{ t('settings.security.disableText') }}</p>
        <UiInput
          v-model="disablePassword"
          type="password"
          :label="t('auth.fields.password')"
          autocomplete="current-password"
          placeholder="••••••••"
        />
      </form>
      <template #footer>
        <UiButton variant="ghost" @click="closeDisable">{{ t('actions.cancel') }}</UiButton>
        <UiButton variant="danger" :loading="busy" :disabled="!disablePassword" @click="disable">
          {{ t('settings.twoFactor.disable') }}
        </UiButton>
      </template>
    </UiModal>

    <!-- Single sign-on -->
    <section v-if="sso.enabled" class="bg-tp-surface rounded-xl overflow-hidden">
      <div class="px-5 py-4 border-b border-tp-border">
        <h2 class="text-tp-text font-display font-semibold text-sm">{{ t('settings.security.sso.title') }}</h2>
        <p class="text-tp-muted text-xs mt-0.5">
          {{ t('settings.security.sso.text', { name: sso.display_name ?? 'SSO' }) }}
        </p>
      </div>
      <div class="p-5 flex flex-wrap items-center justify-between gap-3">
        <p class="text-sm" :class="ssoLinked ? 'text-tp-success' : 'text-tp-muted'">
          {{ ssoLinked ? t('settings.security.sso.connected', { name: sso.display_name ?? 'SSO' }) : t('settings.security.sso.notConnected') }}
        </p>
        <UiButton variant="secondary" size="sm" :loading="busy" @click="connectSso">
          <Link2 class="w-3.5 h-3.5" />
          {{ ssoLinked ? t('settings.security.sso.reconnect') : t('settings.security.sso.connect', { name: sso.display_name ?? 'SSO' }) }}
        </UiButton>
      </div>
    </section>

    <!-- Discord bot: link for /server -->
    <SettingsDiscordLinkPanel @toast="(msg, kind) => flash(msg, kind)" />

    <!-- Active sessions -->
    <section class="bg-tp-surface rounded-xl overflow-hidden">
      <div class="px-5 py-4 border-b border-tp-border">
        <h2 class="text-tp-text font-display font-semibold text-sm">{{ t('settings.security.sessionsTitle') }}</h2>
        <p class="text-tp-muted text-xs mt-0.5">
          {{ t('settings.security.sessionsText') }}
        </p>
      </div>
      <div class="px-5 py-2">
        <ul v-if="sessions.length" class="divide-y divide-tp-border">
          <li v-for="s in sessions" :key="s.id" class="py-3 flex items-center justify-between gap-3">
            <div class="text-sm min-w-0">
              <div class="font-medium text-tp-text truncate">{{ s.user_agent || t('settings.security.unknownDevice') }}</div>
              <div class="text-tp-muted text-xs">{{ s.ip_address || '—' }} · {{ new Date(s.created_at).toLocaleString(locale) }}</div>
            </div>
            <UiButton variant="ghost" size="sm" @click="revokeSession(s.id)">
              {{ t('settings.security.revoke') }}
            </UiButton>
          </li>
        </ul>
        <p v-else class="text-sm text-tp-muted py-3">{{ t('settings.security.noSessions') }}</p>
      </div>
    </section>
  </div>
</template>
