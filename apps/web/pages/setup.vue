<script setup lang="ts">
import { useAuthStore } from '~/stores/auth'
import { useServersStore } from '~/stores/servers'

definePageMeta({ layout: 'auth', title: 'First-time Setup' })

const api = useApi()
const authStore = useAuthStore()
const serversStore = useServersStore()

// 'loading' until /health/setup answers; 'manual' when the install has no
// setup code and the first account has to be created with tale-cli.
const mode = ref<'loading' | 'wizard' | 'manual'>('loading')
const step = ref(1)
const steps = ['Setup code', 'Your account', 'First server']

onMounted(async () => {
  try {
    const data = await api.get<{ needs_setup: boolean; setup_code_required?: boolean }>('/health/setup')
    if (!data.needs_setup) {
      await navigateTo(authStore.isAuthenticated ? '/' : '/auth/login')
      return
    }
    mode.value = data.setup_code_required ? 'wizard' : 'manual'
  } catch {
    await navigateTo('/auth/login')
  }
})

// ── Step 1: setup code ─────────────────────────────────────────────────────
// Codes look like ABCD-EFGH-JKLM and never contain 0, O, 1 or I.
const code = ref('')
const codeError = ref('')
const CODE_PATTERN = /^[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$/

function formatCode(raw: string): string {
  const chars = raw.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 12)
  return chars.match(/.{1,4}/g)?.join('-') ?? ''
}

function onCodeInput(event: Event) {
  const input = event.target as HTMLInputElement
  code.value = formatCode(input.value)
  input.value = code.value
  codeError.value = ''
}

function submitCode() {
  if (CODE_PATTERN.test(code.value)) {
    codeError.value = ''
    step.value = 2
    return
  }
  if (/[01OI]/.test(code.value)) {
    codeError.value = 'Setup codes never contain 0, O, 1 or I. Please check the code in your terminal.'
  } else {
    codeError.value = 'The code has 12 letters and numbers, like ABCD-EFGH-JKLM.'
  }
}

// ── Step 2: owner account ──────────────────────────────────────────────────
const account = reactive({ email: '', username: '', password: '', confirm: '' })
const accountErrors = reactive({ email: '', username: '', confirm: '' })
const accountError = ref('')
const setupDone = ref(false)
const showPassword = ref(false)

// Mirrors the API's owner password policy.
const passwordChecks = computed(() => [
  { label: 'At least 12 characters', ok: [...account.password].length >= 12 },
  { label: 'At least one number', ok: /[0-9]/.test(account.password) },
  { label: 'At least one symbol, like ! ? # or -', ok: /[^A-Za-z0-9]/.test(account.password) },
])
const passwordOk = computed(() => passwordChecks.value.every(c => c.ok))

function validateAccount(): boolean {
  accountErrors.email = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(account.email.trim())
    ? '' : 'Please enter a valid email address'
  accountErrors.username = /^[A-Za-z0-9_]{3,20}$/.test(account.username.trim())
    ? '' : '3 to 20 letters, numbers or underscores'
  accountErrors.confirm = account.password === account.confirm ? '' : 'The passwords do not match'
  return !accountErrors.email && !accountErrors.username && !accountErrors.confirm && passwordOk.value
}

async function submitAccount() {
  accountError.value = ''
  if (!validateAccount()) return

  try {
    await authStore.completeSetup({
      setup_code: code.value,
      email: account.email.trim(),
      username: account.username.trim(),
      password: account.password,
    })
    account.password = ''
    account.confirm = ''
    step.value = 3
    loadPatchlines()
  } catch (err: unknown) {
    const e = err as { status?: number; data?: { error?: string } }
    if (e.status === 403) {
      codeError.value = 'That setup code is not correct. Please check it and try again.'
      step.value = 1
    } else if (e.status === 409) {
      setupDone.value = true
    } else if (e.status === 429) {
      accountError.value = 'Too many attempts. Please wait a minute and try again.'
    } else {
      accountError.value = e.data?.error ?? 'Something went wrong. Please try again.'
    }
  }
}

// ── Step 3: first server ───────────────────────────────────────────────────
interface HytalePatchline { patchline: string; latest: string }
const patchlines = ref<HytalePatchline[]>([])

async function loadPatchlines() {
  try {
    const res = await api.get<{ patchlines: HytalePatchline[] }>('/hytale/versions')
    patchlines.value = res.patchlines
  } catch {
    patchlines.value = []
  }
}

function patchlineLatest(name: string) {
  return patchlines.value.find(p => p.patchline === name)?.latest
}

const ramOptions = [
  { mb: 2048, label: '2 GB', hint: 'A few friends' },
  { mb: 4096, label: '4 GB', hint: 'Recommended' },
  { mb: 8192, label: '8 GB', hint: 'Bigger groups' },
]

const server = reactive({ name: 'My Hytale Server', hytale_version: 'latest', ram_limit_mb: 4096 })
const serverLoading = ref(false)
const serverError = ref('')

async function createServer() {
  if (!server.name.trim()) {
    serverError.value = 'Please give your server a name'
    return
  }
  serverLoading.value = true
  serverError.value = ''
  try {
    const created = await serversStore.createServer({
      name: server.name.trim(),
      hytale_version: server.hytale_version,
      ram_limit_mb: server.ram_limit_mb,
    })
    await navigateTo(`/servers/${created.id}`)
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    const msg = e.data?.error ?? e.message ?? ''
    serverError.value = /no available node/i.test(msg)
      ? 'The game server machine is not connected to the panel yet. This usually takes a minute after installing; try again shortly or skip for now.'
      : msg || 'The server could not be created. You can try again from the Servers page.'
  } finally {
    serverLoading.value = false
  }
}

async function skipServer() {
  await navigateTo('/')
}

const inputClass = [
  'w-full bg-tp-surface text-tp-text rounded-xl pl-10 pr-4 py-2.5 text-sm',
  'placeholder:text-tp-outline',
  'focus:outline-none focus:bg-tp-surface-lowest focus:ring-2 focus:ring-tp-primary/50',
  'transition-all duration-150',
]
</script>

<template>
  <div>
    <!-- Loading -->
    <div v-if="mode === 'loading'" class="flex justify-center py-10">
      <span class="material-symbols-outlined text-tp-outline text-3xl animate-spin">progress_activity</span>
    </div>

    <!-- No setup code configured: create the owner with tale-cli -->
    <template v-else-if="mode === 'manual'">
      <div class="mb-6">
        <h2 class="text-tp-text font-display font-bold text-xl">Welcome to TalePanel</h2>
        <p class="text-tp-muted text-sm mt-1">No admin account exists yet. Create one to get started.</p>
      </div>

      <div class="bg-tp-surface rounded-xl p-5 space-y-4 mb-5">
        <div class="flex items-center gap-2">
          <span class="material-symbols-outlined text-tp-accent text-lg">terminal</span>
          <p class="text-tp-text font-medium text-sm">Run this command on your panel server:</p>
        </div>
        <pre class="bg-tp-bg rounded-lg p-4 text-sm font-mono text-tp-accent overflow-x-auto">docker compose run --rm api tale-cli admin create</pre>
        <p class="text-sm text-tp-muted">
          This creates the first owner account. Once done,
          <NuxtLink to="/auth/login" class="text-tp-accent font-semibold hover:text-tp-primary transition-colors">
            sign in here
          </NuxtLink>.
        </p>
      </div>

      <p class="text-center text-xs text-tp-muted">
        Running TalePanel outside Docker? Use
        <code class="font-mono text-tp-accent">./tale-cli admin create</code> directly.
      </p>
    </template>

    <!-- Setup wizard -->
    <template v-else>
      <!-- Progress -->
      <ol class="flex items-center mb-7" aria-label="Setup progress">
        <li
          v-for="(label, i) in steps"
          :key="label"
          :class="['flex items-center', i < steps.length - 1 ? 'flex-1' : '']"
          :aria-current="step === i + 1 ? 'step' : undefined"
        >
          <div class="flex flex-col items-center gap-1.5">
            <span
              :class="[
                'w-8 h-8 rounded-full flex items-center justify-center text-xs font-bold transition-colors',
                step > i + 1 ? 'bg-tp-success/20 text-tp-success'
                : step === i + 1 ? 'bg-tp-primary text-tp-on-primary'
                : 'bg-tp-surface3 text-tp-outline',
              ]"
            >
              <span v-if="step > i + 1" class="material-symbols-outlined text-base">check</span>
              <template v-else>{{ i + 1 }}</template>
            </span>
            <span :class="['text-[11px] whitespace-nowrap', step === i + 1 ? 'text-tp-text font-semibold' : 'text-tp-outline']">
              {{ label }}
            </span>
          </div>
          <div
            v-if="i < steps.length - 1"
            :class="['flex-1 h-0.5 mx-2 mb-5 rounded-full', step > i + 1 ? 'bg-tp-success/40' : 'bg-tp-surface3']"
          />
        </li>
      </ol>

      <!-- Already completed (409) -->
      <div v-if="setupDone" class="space-y-5">
        <div class="flex items-start gap-3 bg-tp-surface rounded-xl p-4">
          <span class="material-symbols-outlined text-tp-accent text-lg shrink-0 mt-0.5">info</span>
          <p class="text-tp-text text-sm">This panel has already been set up. Please sign in with your account.</p>
        </div>
        <NuxtLink
          to="/auth/login"
          class="w-full cta-gradient text-tp-on-primary font-bold text-sm uppercase tracking-wider py-3 rounded-xl flex items-center justify-center gap-2 hover:brightness-110 transition-all"
        >
          Go to sign in
          <span class="material-symbols-outlined text-base">arrow_forward</span>
        </NuxtLink>
      </div>

      <!-- Step 1: setup code -->
      <form v-else-if="step === 1" class="space-y-5" @submit.prevent="submitCode">
        <div>
          <h2 class="text-tp-text font-display font-bold text-xl">Welcome to TalePanel</h2>
          <p class="text-tp-muted text-sm mt-1">
            Enter the setup code that was printed at the end of the installer in your terminal.
            It proves that you are the person who installed this panel.
          </p>
        </div>

        <div class="flex flex-col gap-1.5">
          <label for="setup-code" class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Setup code</label>
          <div class="relative">
            <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-tp-outline text-lg">key</span>
            <input
              id="setup-code"
              :value="code"
              type="text"
              inputmode="text"
              autocomplete="off"
              autocapitalize="characters"
              spellcheck="false"
              placeholder="ABCD-EFGH-JKLM"
              maxlength="14"
              autofocus
              :class="[...inputClass, 'font-mono tracking-[0.2em] uppercase', codeError ? 'ring-2 ring-tp-danger/50' : '']"
              @input="onCodeInput"
            />
          </div>
          <p v-if="codeError" class="text-tp-danger text-xs">{{ codeError }}</p>
          <p v-else class="text-tp-outline text-xs">Lost the code? Run the installer again to get a new one.</p>
        </div>

        <button
          type="submit"
          class="w-full cta-gradient text-tp-on-primary font-bold text-sm uppercase tracking-wider py-3 rounded-xl flex items-center justify-center gap-2 hover:brightness-110 transition-all"
        >
          Continue
          <span class="material-symbols-outlined text-base">arrow_forward</span>
        </button>
      </form>

      <!-- Step 2: owner account -->
      <form v-else-if="step === 2" class="space-y-4" @submit.prevent="submitAccount">
        <div>
          <h2 class="text-tp-text font-display font-bold text-xl">Your admin account</h2>
          <p class="text-tp-muted text-sm mt-1">This account owns the panel. You use it to sign in from now on.</p>
        </div>

        <div v-if="accountError" class="flex items-start gap-3 bg-tp-error/10 rounded-xl p-3">
          <span class="material-symbols-outlined text-tp-error text-lg shrink-0 mt-0.5">error</span>
          <p class="text-tp-error text-sm">{{ accountError }}</p>
        </div>

        <div class="flex flex-col gap-1.5">
          <label for="setup-email" class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Email address</label>
          <div class="relative">
            <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-tp-outline text-lg">alternate_email</span>
            <input
              id="setup-email"
              v-model="account.email"
              type="email"
              autocomplete="email"
              placeholder="you@example.com"
              required
              :class="[...inputClass, accountErrors.email ? 'ring-2 ring-tp-danger/50' : '']"
            />
          </div>
          <p v-if="accountErrors.email" class="text-tp-danger text-xs">{{ accountErrors.email }}</p>
        </div>

        <div class="flex flex-col gap-1.5">
          <label for="setup-username" class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Username</label>
          <div class="relative">
            <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-tp-outline text-lg">person</span>
            <input
              id="setup-username"
              v-model="account.username"
              type="text"
              autocomplete="username"
              placeholder="admin"
              required
              :class="[...inputClass, accountErrors.username ? 'ring-2 ring-tp-danger/50' : '']"
            />
          </div>
          <p v-if="accountErrors.username" class="text-tp-danger text-xs">{{ accountErrors.username }}</p>
        </div>

        <div class="flex flex-col gap-1.5">
          <label for="setup-password" class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Password</label>
          <div class="relative">
            <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-tp-outline text-lg">lock</span>
            <input
              id="setup-password"
              v-model="account.password"
              :type="showPassword ? 'text' : 'password'"
              autocomplete="new-password"
              required
              :class="[...inputClass, 'pr-10']"
            />
            <button
              type="button"
              class="absolute right-3 top-1/2 -translate-y-1/2 text-tp-outline hover:text-tp-muted transition-colors"
              :aria-label="showPassword ? 'Hide password' : 'Show password'"
              @click="showPassword = !showPassword"
            >
              <span class="material-symbols-outlined text-lg">{{ showPassword ? 'visibility_off' : 'visibility' }}</span>
            </button>
          </div>
          <ul class="space-y-1 mt-1">
            <li
              v-for="check in passwordChecks"
              :key="check.label"
              :class="['flex items-center gap-1.5 text-xs', check.ok ? 'text-tp-success' : 'text-tp-outline']"
            >
              <span class="material-symbols-outlined text-sm">{{ check.ok ? 'check_circle' : 'radio_button_unchecked' }}</span>
              {{ check.label }}
            </li>
          </ul>
        </div>

        <div class="flex flex-col gap-1.5">
          <label for="setup-confirm" class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Repeat password</label>
          <div class="relative">
            <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-tp-outline text-lg">lock</span>
            <input
              id="setup-confirm"
              v-model="account.confirm"
              :type="showPassword ? 'text' : 'password'"
              autocomplete="new-password"
              required
              :class="[...inputClass, accountErrors.confirm ? 'ring-2 ring-tp-danger/50' : '']"
            />
          </div>
          <p v-if="accountErrors.confirm" class="text-tp-danger text-xs">{{ accountErrors.confirm }}</p>
        </div>

        <div class="flex items-center gap-3 pt-1">
          <UiButton variant="ghost" size="md" @click="step = 1">Back</UiButton>
          <button
            type="submit"
            :disabled="authStore.loading || !passwordOk"
            class="flex-1 cta-gradient text-tp-on-primary font-bold text-sm uppercase tracking-wider py-3 rounded-xl flex items-center justify-center gap-2 hover:brightness-110 transition-all disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <span v-if="authStore.loading" class="material-symbols-outlined text-base animate-spin">progress_activity</span>
            Create account
            <span class="material-symbols-outlined text-base">arrow_forward</span>
          </button>
        </div>
      </form>

      <!-- Step 3: first server -->
      <form v-else class="space-y-4" @submit.prevent="createServer">
        <div>
          <h2 class="text-tp-text font-display font-bold text-xl">Create your first Hytale server</h2>
          <p class="text-tp-muted text-sm mt-1">
            Your account is ready. Pick a name and how much memory the server may use. You can change this later.
          </p>
        </div>

        <div v-if="serverError" class="flex items-start gap-3 bg-tp-error/10 rounded-xl p-3">
          <span class="material-symbols-outlined text-tp-error text-lg shrink-0 mt-0.5">error</span>
          <p class="text-tp-error text-sm">{{ serverError }}</p>
        </div>

        <UiInput v-model="server.name" label="Server name" placeholder="My Hytale Server" :required="true" />

        <div class="flex flex-col gap-1.5">
          <label for="setup-hytale-version" class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Hytale version</label>
          <select
            id="setup-hytale-version"
            v-model="server.hytale_version"
            class="w-full bg-tp-surface text-tp-text rounded-xl px-3 py-2.5 text-sm focus:outline-none focus:ring-2 focus:ring-tp-primary/50"
          >
            <option value="latest">Release{{ patchlineLatest('release') ? ` (${patchlineLatest('release')})` : '' }}</option>
            <option value="pre-release">Pre-release{{ patchlineLatest('pre-release') ? ` (${patchlineLatest('pre-release')})` : '' }}</option>
          </select>
          <p v-if="server.hytale_version === 'pre-release'" class="text-tp-warning text-xs">
            Pre-release worlds cannot be opened by release servers.
          </p>
        </div>

        <fieldset class="flex flex-col gap-1.5">
          <legend class="text-xs font-semibold text-tp-muted uppercase tracking-wider mb-1.5">Memory</legend>
          <div class="grid grid-cols-3 gap-2">
            <button
              v-for="opt in ramOptions"
              :key="opt.mb"
              type="button"
              :aria-pressed="server.ram_limit_mb === opt.mb"
              :class="[
                'rounded-xl px-3 py-2.5 text-left transition-all',
                server.ram_limit_mb === opt.mb
                  ? 'bg-tp-primary/15 ring-2 ring-tp-primary/60'
                  : 'bg-tp-surface hover:bg-tp-surface2',
              ]"
              @click="server.ram_limit_mb = opt.mb"
            >
              <span class="block text-tp-text text-sm font-semibold">{{ opt.label }}</span>
              <span class="block text-tp-outline text-[11px]">{{ opt.hint }}</span>
            </button>
          </div>
        </fieldset>

        <div class="flex items-center gap-3 pt-1">
          <UiButton variant="ghost" size="md" :disabled="serverLoading" @click="skipServer">Skip for now</UiButton>
          <button
            type="submit"
            :disabled="serverLoading"
            class="flex-1 cta-gradient text-tp-on-primary font-bold text-sm uppercase tracking-wider py-3 rounded-xl flex items-center justify-center gap-2 hover:brightness-110 transition-all disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <span v-if="serverLoading" class="material-symbols-outlined text-base animate-spin">progress_activity</span>
            Create server
            <span class="material-symbols-outlined text-base">arrow_forward</span>
          </button>
        </div>
      </form>
    </template>
  </div>
</template>
