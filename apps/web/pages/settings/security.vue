<script setup lang="ts">
import { useAuthStore } from '~/stores/auth'

definePageMeta({ title: 'Security', middleware: 'auth' })

const api = useApi()
const authStore = useAuthStore()

type User = { totp_enabled: boolean; email: string }

const me = ref<User | null>(null)
const busy = ref(false)
const toast = ref('')
const toastKind = ref<'ok' | 'error'>('ok')

function flash(msg: string, kind: 'ok' | 'error' = 'ok') {
  toast.value = msg
  toastKind.value = kind
  setTimeout(() => { toast.value = '' }, 3000)
}

onMounted(async () => {
  await refresh()
})

async function refresh() {
  const { user } = await api.get<{ user: User }>('/auth/me')
  me.value = user
}

// ── Setup flow ────────────────────────────────────────────────────────────
const setupState = ref<'idle' | 'pending' | 'confirmed'>('idle')
const setupData = ref<{ otpauth_uri: string; qr_code_base64: string; secret: string } | null>(null)
const setupCode = ref('')

async function startSetup() {
  busy.value = true
  try {
    setupData.value = await api.post<typeof setupData.value>('/auth/totp/setup')
    setupState.value = 'pending'
  } catch (err) {
    flash((err as Error).message || 'Could not start 2FA setup', 'error')
  } finally {
    busy.value = false
  }
}

async function confirmSetup() {
  busy.value = true
  try {
    await api.post('/auth/totp/confirm', { code: setupCode.value })
    setupState.value = 'confirmed'
    setupData.value = null
    setupCode.value = ''
    await refresh()
    flash('2FA enabled')
  } catch (err) {
    const e = err as { data?: { error?: string }; message?: string }
    flash(e.data?.error || e.message || 'Invalid code', 'error')
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

async function disable() {
  busy.value = true
  try {
    await api.post('/auth/totp/disable', { password: disablePassword.value })
    disableOpen.value = false
    disablePassword.value = ''
    await refresh()
    flash('2FA disabled')
  } catch (err) {
    const e = err as { data?: { error?: string } }
    flash(e.data?.error || 'Password incorrect', 'error')
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
  <div class="max-w-2xl mx-auto py-8 space-y-8">
    <header>
      <h1 class="text-2xl font-semibold">Security</h1>
      <p class="text-sm text-gray-500">Two-factor authentication and active sessions.</p>
    </header>

    <!-- Toast -->
    <div
      v-if="toast"
      :class="[
        'rounded-md px-4 py-2 text-sm',
        toastKind === 'ok' ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'
      ]"
    >
      {{ toast }}
    </div>

    <!-- TOTP card -->
    <section class="rounded-lg border border-gray-200 p-6 space-y-4">
      <h2 class="text-lg font-medium">Two-factor authentication</h2>

      <!-- Already enabled -->
      <div v-if="me?.totp_enabled && setupState !== 'pending'">
        <p class="text-sm text-green-700">
          2FA is <strong>active</strong> on your account.
        </p>
        <button
          class="mt-3 rounded-md bg-red-600 px-3 py-2 text-sm text-white hover:bg-red-700"
          :disabled="busy"
          @click="disableOpen = true"
        >
          Disable 2FA
        </button>
      </div>

      <!-- Not enabled, idle -->
      <div v-else-if="setupState === 'idle'">
        <p class="text-sm text-gray-600">
          Add an authenticator app (1Password, Authy, Aegis, Google Authenticator) for an extra login factor.
        </p>
        <button
          class="mt-3 rounded-md bg-blue-600 px-3 py-2 text-sm text-white hover:bg-blue-700"
          :disabled="busy"
          @click="startSetup"
        >
          {{ busy ? 'Working…' : 'Set up 2FA' }}
        </button>
      </div>

      <!-- Setup in progress -->
      <div v-else-if="setupState === 'pending' && setupData" class="space-y-4">
        <p class="text-sm text-gray-700">
          Scan this QR code with your authenticator app, then enter the 6-digit code to confirm.
        </p>
        <img
          :src="`data:image/png;base64,${setupData.qr_code_base64}`"
          alt="TOTP QR code"
          class="w-48 h-48 border border-gray-200 rounded"
        />
        <details class="text-sm text-gray-500">
          <summary class="cursor-pointer">Can't scan? Enter the secret manually</summary>
          <code class="block mt-2 p-2 bg-gray-100 rounded break-all">{{ setupData.secret }}</code>
        </details>

        <div class="flex items-end gap-3">
          <label class="flex-1">
            <span class="block text-sm font-medium text-gray-700 mb-1">Verification code</span>
            <input
              v-model="setupCode"
              type="text"
              inputmode="numeric"
              pattern="[0-9]{6}"
              maxlength="6"
              placeholder="123456"
              class="w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
            />
          </label>
          <button
            class="rounded-md bg-blue-600 px-3 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
            :disabled="busy || setupCode.length !== 6"
            @click="confirmSetup"
          >
            Confirm
          </button>
          <button
            class="rounded-md border border-gray-300 px-3 py-2 text-sm"
            :disabled="busy"
            @click="cancelSetup"
          >
            Cancel
          </button>
        </div>
      </div>
    </section>

    <!-- Disable confirmation modal -->
    <div
      v-if="disableOpen"
      class="fixed inset-0 bg-black/40 flex items-center justify-center z-50"
      @click.self="disableOpen = false"
    >
      <div class="bg-white rounded-lg p-6 w-96 space-y-4">
        <h3 class="font-medium">Disable 2FA</h3>
        <p class="text-sm text-gray-600">Enter your password to confirm.</p>
        <input
          v-model="disablePassword"
          type="password"
          placeholder="Password"
          class="w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
        />
        <div class="flex justify-end gap-2">
          <button
            class="rounded-md border border-gray-300 px-3 py-2 text-sm"
            @click="disableOpen = false"
          >
            Cancel
          </button>
          <button
            class="rounded-md bg-red-600 px-3 py-2 text-sm text-white hover:bg-red-700 disabled:opacity-50"
            :disabled="busy || !disablePassword"
            @click="disable"
          >
            Disable 2FA
          </button>
        </div>
      </div>
    </div>

    <!-- Active sessions -->
    <section class="rounded-lg border border-gray-200 p-6 space-y-4">
      <h2 class="text-lg font-medium">Active sessions</h2>
      <p class="text-sm text-gray-500">
        Each refresh token. Revoke anything you don't recognise.
      </p>
      <ul v-if="sessions.length" class="divide-y divide-gray-200">
        <li v-for="s in sessions" :key="s.id" class="py-3 flex items-center justify-between">
          <div class="text-sm">
            <div class="font-medium">{{ s.user_agent || 'unknown device' }}</div>
            <div class="text-gray-500">{{ s.ip_address || '—' }} · {{ new Date(s.created_at).toLocaleString() }}</div>
          </div>
          <button
            class="text-sm text-red-600 hover:underline"
            @click="revokeSession(s.id)"
          >
            Revoke
          </button>
        </li>
      </ul>
      <p v-else class="text-sm text-gray-500">No active sessions.</p>
    </section>
  </div>
</template>
