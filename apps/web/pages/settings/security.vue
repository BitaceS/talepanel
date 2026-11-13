<script setup lang="ts">
import { KeyRound, RefreshCw, Shield, ShieldCheck } from '@lucide/vue'

definePageMeta({ title: 'Security', middleware: 'auth' })

const api = useApi()
const route = useRoute()

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
  const { user } = await api.get<{ user: User }>('/auth/me')
  me.value = user
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
    flash('New recovery codes generated; the old ones no longer work')
  } catch (err) {
    flash(errorMessage(err, 'Could not generate recovery codes'), 'error')
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
    flash(errorMessage(err, 'Could not start 2FA setup'), 'error')
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
    flash('2FA enabled')
  } catch (err) {
    flash(errorMessage(err, 'Invalid code'), 'error')
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
    flash('2FA disabled')
  } catch (err) {
    flash(errorMessage(err, 'Password incorrect'), 'error')
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
  if (!confirm('Revoke this session?')) return
  await api.delete(`/auth/sessions/${id}`)
  await loadSessions()
  flash('Session revoked')
}

onMounted(() => loadSessions())
</script>

<template>
  <div class="max-w-2xl mx-auto py-8 space-y-6">
    <header>
      <h1 class="text-2xl font-display font-bold text-tp-text">Security</h1>
      <p class="text-sm text-tp-muted">Two-factor authentication and active sessions.</p>
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

    <!-- Signed in with a recovery code -->
    <div v-if="recoveryUsedNotice" class="flex items-start gap-3 bg-tp-warning/10 rounded-xl p-4">
      <KeyRound class="w-4 h-4 text-tp-warning shrink-0 mt-0.5" />
      <div class="text-sm flex-1">
        <p class="text-tp-text font-semibold">You signed in with a recovery code</p>
        <p class="text-tp-muted mt-0.5">
          That code is now used up<template v-if="recoveryStatus">; {{ recoveryStatus.remaining }} left</template>.
          If you lost your authenticator, disable 2FA below and set it up again on your new device;
          otherwise generate new codes once you're running low.
        </p>
      </div>
      <button class="text-tp-muted hover:text-tp-text text-xs" @click="recoveryUsedNotice = false">Dismiss</button>
    </div>

    <!-- TOTP card -->
    <section class="bg-tp-surface rounded-xl overflow-hidden">
      <div class="px-5 py-4 border-b border-tp-border">
        <h2 class="text-tp-text font-display font-semibold text-sm">Two-factor authentication</h2>
        <p class="text-tp-muted text-xs mt-0.5">
          An authenticator app adds a second login factor; recovery codes get you in if you lose it.
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
              2FA is <strong class="text-tp-success">active</strong> on your account.
            </p>
          </div>

          <div class="bg-tp-surface2 rounded-xl p-4 flex flex-wrap items-center justify-between gap-3">
            <div class="flex items-center gap-3">
              <KeyRound class="w-4 h-4 text-tp-muted shrink-0" />
              <div>
                <p class="text-tp-text text-sm font-medium">Recovery codes</p>
                <p
                  v-if="recoveryStatus"
                  :class="[
                    'text-xs',
                    recoveryStatus.remaining <= 3 ? 'text-tp-warning' : 'text-tp-muted',
                  ]"
                >
                  {{ recoveryStatus.remaining }} of {{ recoveryStatus.total }} unused
                  <template v-if="recoveryStatus.total === 0">: generate a set so you can still sign in without your authenticator</template>
                  <template v-else-if="recoveryStatus.remaining === 0">: generate new ones now</template>
                  <template v-else-if="recoveryStatus.remaining <= 3">: consider generating new ones</template>
                </p>
              </div>
            </div>
            <UiButton variant="secondary" size="sm" :disabled="busy" @click="regenOpen = true">
              <RefreshCw class="w-3.5 h-3.5" /> Generate new codes
            </UiButton>
          </div>

          <UiButton variant="danger" size="sm" :disabled="busy" @click="disableOpen = true">
            Disable 2FA
          </UiButton>
        </template>

        <!-- Not enabled, idle -->
        <template v-else-if="setupState === 'idle'">
          <p class="text-sm text-tp-muted">
            Add an authenticator app (1Password, Authy, Aegis, Google Authenticator) for an extra login factor.
          </p>
          <UiButton variant="primary" size="sm" :loading="busy" @click="startSetup">
            <Shield class="w-3.5 h-3.5" /> Set up 2FA
          </UiButton>
        </template>

        <!-- Setup in progress -->
        <div v-else-if="setupState === 'pending' && setupData" class="space-y-4">
          <p class="text-sm text-tp-text">
            Scan this QR code with your authenticator app, then enter the 6-digit code to confirm.
          </p>
          <img
            :src="`data:image/png;base64,${setupData.qr_code_base64}`"
            alt="TOTP QR code"
            class="w-48 h-48 rounded-xl bg-white p-2"
          />
          <details class="text-sm text-tp-muted">
            <summary class="cursor-pointer">Can't scan? Enter the secret manually</summary>
            <code class="block mt-2 p-2 bg-tp-surface2 text-tp-text rounded-lg break-all font-mono">{{ setupData.secret }}</code>
          </details>

          <div class="flex items-end gap-3">
            <label class="flex-1 flex flex-col gap-1.5">
              <span class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Verification code</span>
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
              Confirm
            </UiButton>
            <UiButton variant="ghost" :disabled="busy" @click="cancelSetup">
              Cancel
            </UiButton>
          </div>
        </div>
      </div>
    </section>

    <!-- Regenerate recovery codes -->
    <UiModal :open="regenOpen" title="Generate new recovery codes" size="sm" @close="closeRegen">
      <form class="space-y-4" @submit.prevent="canRegenerate && !busy && regenerate()">
        <p class="text-sm text-tp-muted">
          Your current recovery codes stop working immediately. Confirm with your password and a code from your authenticator app.
        </p>
        <UiInput
          v-model="regenPassword"
          type="password"
          label="Password"
          autocomplete="current-password"
          placeholder="••••••••"
        />
        <UiInput
          v-model="regenCode"
          label="Authenticator code"
          autocomplete="one-time-code"
          placeholder="123456"
        />
        <!-- Lets Enter submit; the visible button lives in the modal footer. -->
        <button type="submit" class="sr-only" tabindex="-1" aria-hidden="true" />
      </form>
      <template #footer>
        <UiButton variant="ghost" @click="closeRegen">Cancel</UiButton>
        <UiButton
          variant="primary"
          :loading="busy"
          :disabled="!canRegenerate"
          @click="regenerate"
        >
          <RefreshCw class="w-3.5 h-3.5" /> Generate
        </UiButton>
      </template>
    </UiModal>

    <!-- Disable confirmation -->
    <UiModal :open="disableOpen" title="Disable 2FA" size="sm" @close="closeDisable">
      <form class="space-y-4" @submit.prevent="disablePassword && !busy && disable()">
        <p class="text-sm text-tp-muted">Enter your password to confirm. Your recovery codes are deleted as well.</p>
        <UiInput
          v-model="disablePassword"
          type="password"
          label="Password"
          autocomplete="current-password"
          placeholder="••••••••"
        />
      </form>
      <template #footer>
        <UiButton variant="ghost" @click="closeDisable">Cancel</UiButton>
        <UiButton variant="danger" :loading="busy" :disabled="!disablePassword" @click="disable">
          Disable 2FA
        </UiButton>
      </template>
    </UiModal>

    <!-- Active sessions -->
    <section class="bg-tp-surface rounded-xl overflow-hidden">
      <div class="px-5 py-4 border-b border-tp-border">
        <h2 class="text-tp-text font-display font-semibold text-sm">Active sessions</h2>
        <p class="text-tp-muted text-xs mt-0.5">
          Each refresh token. Revoke anything you don't recognise.
        </p>
      </div>
      <div class="px-5 py-2">
        <ul v-if="sessions.length" class="divide-y divide-tp-border">
          <li v-for="s in sessions" :key="s.id" class="py-3 flex items-center justify-between gap-3">
            <div class="text-sm min-w-0">
              <div class="font-medium text-tp-text truncate">{{ s.user_agent || 'unknown device' }}</div>
              <div class="text-tp-muted text-xs">{{ s.ip_address || '—' }} · {{ new Date(s.created_at).toLocaleString() }}</div>
            </div>
            <UiButton variant="ghost" size="sm" @click="revokeSession(s.id)">
              Revoke
            </UiButton>
          </li>
        </ul>
        <p v-else class="text-sm text-tp-muted py-3">No active sessions.</p>
      </div>
    </section>
  </div>
</template>
