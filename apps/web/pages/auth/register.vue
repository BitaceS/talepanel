<script setup lang="ts">
import { useApi } from '~/composables/useApi'

definePageMeta({ layout: 'auth' })

const api = useApi()

const form = reactive({
  username: '',
  email: '',
  password: '',
  confirm: '',
})

const errors = reactive({
  username: '',
  email: '',
  password: '',
  confirm: '',
})

const serverError = ref('')
const loading = ref(false)
const success = ref(false)
const showPassword = ref(false)

function validateEmail(email: string): boolean {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)
}

function validate(): boolean {
  errors.username = ''
  errors.email = ''
  errors.password = ''
  errors.confirm = ''

  if (!form.username) {
    errors.username = 'Username is required'
  } else if (form.username.length < 3) {
    errors.username = 'Username must be at least 3 characters'
  }

  if (!form.email) {
    errors.email = 'Email is required'
  } else if (!validateEmail(form.email)) {
    errors.email = 'Please enter a valid email address'
  }

  if (!form.password) {
    errors.password = 'Password is required'
  } else if (form.password.length < 8) {
    errors.password = 'Password must be at least 8 characters'
  }

  if (form.password !== form.confirm) {
    errors.confirm = 'Passwords do not match'
  }

  return !errors.username && !errors.email && !errors.password && !errors.confirm
}

async function onSubmit() {
  serverError.value = ''
  if (!validate()) return

  loading.value = true
  try {
    await api.post('/auth/register', {
      username: form.username,
      email: form.email,
      password: form.password,
    })
    success.value = true
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    serverError.value = e.data?.error ?? e.message ?? 'Registration failed.'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div>
    <div class="mb-6">
      <h2 class="text-tp-text font-display font-bold text-xl">Request Access</h2>
      <p class="text-tp-muted text-sm mt-1">Create your operator account</p>
    </div>

    <!-- Success -->
    <div v-if="success" class="text-center space-y-4">
      <div class="w-14 h-14 bg-tp-success/15 rounded-2xl flex items-center justify-center mx-auto">
        <span class="material-symbols-outlined text-tp-success text-3xl">check_circle</span>
      </div>
      <div>
        <p class="text-tp-text font-display font-semibold text-lg">Account created!</p>
        <p class="text-tp-muted text-sm mt-1">You can now sign in with your credentials.</p>
      </div>
      <NuxtLink to="/auth/login">
        <button class="w-full cta-gradient text-tp-on-primary font-bold text-sm uppercase tracking-wider py-3 rounded-xl flex items-center justify-center gap-2 hover:brightness-110 transition-all mt-2">
          <span>PROCEED TO LOGIN</span>
          <span class="material-symbols-outlined text-base">arrow_forward</span>
        </button>
      </NuxtLink>
    </div>

    <template v-else>
      <!-- Server error -->
      <div v-if="serverError" class="flex items-start gap-3 bg-tp-error/10 rounded-xl p-3 mb-5">
        <span class="material-symbols-outlined text-tp-error text-lg shrink-0 mt-0.5">error</span>
        <p class="text-tp-error text-sm">{{ serverError }}</p>
      </div>

      <form class="space-y-4" @submit.prevent="onSubmit">
        <!-- Username -->
        <div class="flex flex-col gap-1.5">
          <label class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Operator Name</label>
          <div class="relative">
            <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-tp-outline text-lg">person</span>
            <input
              v-model="form.username"
              type="text"
              placeholder="Choose a username"
              autocomplete="username"
              required
              :class="[
                'w-full bg-tp-surface text-tp-text rounded-xl pl-10 pr-4 py-2.5 text-sm',
                'placeholder:text-tp-outline',
                'focus:outline-none focus:bg-tp-surface-lowest focus:ring-2 focus:ring-tp-primary/50',
                'transition-all duration-150',
                errors.username ? 'ring-2 ring-tp-danger/50' : '',
              ]"
            />
          </div>
          <p v-if="errors.username" class="text-tp-danger text-xs">{{ errors.username }}</p>
        </div>

        <!-- Email -->
        <div class="flex flex-col gap-1.5">
          <label class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Email Address</label>
          <div class="relative">
            <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-tp-outline text-lg">alternate_email</span>
            <input
              v-model="form.email"
              type="email"
              placeholder="you@example.com"
              autocomplete="email"
              required
              :class="[
                'w-full bg-tp-surface text-tp-text rounded-xl pl-10 pr-4 py-2.5 text-sm',
                'placeholder:text-tp-outline',
                'focus:outline-none focus:bg-tp-surface-lowest focus:ring-2 focus:ring-tp-primary/50',
                'transition-all duration-150',
                errors.email ? 'ring-2 ring-tp-danger/50' : '',
              ]"
            />
          </div>
          <p v-if="errors.email" class="text-tp-danger text-xs">{{ errors.email }}</p>
        </div>

        <!-- Password -->
        <div class="flex flex-col gap-1.5">
          <label class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Access Key</label>
          <div class="relative">
            <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-tp-outline text-lg">lock</span>
            <input
              v-model="form.password"
              :type="showPassword ? 'text' : 'password'"
              placeholder="Min. 8 characters"
              autocomplete="new-password"
              required
              :class="[
                'w-full bg-tp-surface text-tp-text rounded-xl pl-10 pr-10 py-2.5 text-sm',
                'placeholder:text-tp-outline',
                'focus:outline-none focus:bg-tp-surface-lowest focus:ring-2 focus:ring-tp-primary/50',
                'transition-all duration-150',
                errors.password ? 'ring-2 ring-tp-danger/50' : '',
              ]"
            />
            <button
              type="button"
              class="absolute right-3 top-1/2 -translate-y-1/2 text-tp-outline hover:text-tp-muted transition-colors"
              @click="showPassword = !showPassword"
            >
              <span class="material-symbols-outlined text-lg">{{ showPassword ? 'visibility_off' : 'visibility' }}</span>
            </button>
          </div>
          <p v-if="errors.password" class="text-tp-danger text-xs">{{ errors.password }}</p>
        </div>

        <!-- Confirm Password -->
        <div class="flex flex-col gap-1.5">
          <label class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Confirm Access Key</label>
          <div class="relative">
            <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-tp-outline text-lg">lock</span>
            <input
              v-model="form.confirm"
              :type="showPassword ? 'text' : 'password'"
              placeholder="Repeat your access key"
              autocomplete="new-password"
              required
              :class="[
                'w-full bg-tp-surface text-tp-text rounded-xl pl-10 pr-4 py-2.5 text-sm',
                'placeholder:text-tp-outline',
                'focus:outline-none focus:bg-tp-surface-lowest focus:ring-2 focus:ring-tp-primary/50',
                'transition-all duration-150',
                errors.confirm ? 'ring-2 ring-tp-danger/50' : '',
              ]"
            />
          </div>
          <p v-if="errors.confirm" class="text-tp-danger text-xs">{{ errors.confirm }}</p>
        </div>

        <!-- Submit -->
        <button
          type="submit"
          :disabled="loading"
          class="w-full cta-gradient text-tp-on-primary font-bold text-sm uppercase tracking-wider py-3 rounded-xl flex items-center justify-center gap-2 hover:brightness-110 transition-all disabled:opacity-50 disabled:cursor-not-allowed mt-2"
        >
          <svg
            v-if="loading"
            class="w-4 h-4 animate-spin shrink-0"
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
          </svg>
          <span>CREATE ACCOUNT</span>
          <span class="material-symbols-outlined text-base">arrow_forward</span>
        </button>
      </form>

      <div class="mt-5 text-center">
        <p class="text-tp-muted text-sm">
          Already an operator?
          <NuxtLink to="/auth/login" class="text-tp-accent font-semibold hover:text-tp-primary transition-colors ml-1">
            Sign in
          </NuxtLink>
        </p>
      </div>

      <!-- Footer links -->
      <div class="flex items-center justify-center gap-4 mt-5 pt-4">
        <a href="#" class="flex items-center gap-1 text-tp-outline text-xs hover:text-tp-muted transition-colors">
          <span class="material-symbols-outlined text-sm">shield</span>
          Safety Protocols
        </a>
        <a href="#" class="flex items-center gap-1 text-tp-outline text-xs hover:text-tp-muted transition-colors">
          <span class="material-symbols-outlined text-sm">support</span>
          Support Hub
        </a>
      </div>
    </template>
  </div>
</template>
