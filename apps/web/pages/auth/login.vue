<script setup lang="ts">
import { useAuthStore } from '~/stores/auth'

definePageMeta({
  layout: 'auth',
})

const authStore = useAuthStore()

const form = reactive({
  email: '',
  password: '',
  rememberMe: false,
})

const errors = reactive({
  email: '',
  password: '',
})

const serverError = ref('')
const showPassword = ref(false)

function validateEmail(email: string): boolean {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)
}

function validate(): boolean {
  errors.email = ''
  errors.password = ''

  if (!form.email) {
    errors.email = 'Email is required'
  } else if (!validateEmail(form.email)) {
    errors.email = 'Please enter a valid email address'
  }

  if (!form.password) {
    errors.password = 'Password is required'
  }

  return !errors.email && !errors.password
}

async function onSubmit() {
  serverError.value = ''

  if (!validate()) return

  try {
    await authStore.login(form.email, form.password)
  } catch (err: unknown) {
    const e = err as { data?: { error?: string }; message?: string }
    serverError.value = e.data?.error ?? e.message ?? 'Login failed. Please check your credentials.'
  }
}
</script>

<template>
  <div>
    <!-- Title -->
    <div class="mb-6">
      <h2 class="text-tp-text font-display font-bold text-xl">Welcome back</h2>
      <p class="text-tp-muted text-sm mt-1">Sign in to your command center</p>
    </div>

    <!-- Server error -->
    <div
      v-if="serverError"
      class="flex items-start gap-3 bg-tp-error/10 rounded-xl p-3 mb-5"
    >
      <span class="material-symbols-outlined text-tp-error text-lg shrink-0 mt-0.5">error</span>
      <p class="text-tp-error text-sm">{{ serverError }}</p>
    </div>

    <!-- Form -->
    <form class="space-y-5" @submit.prevent="onSubmit">
      <!-- Email -->
      <div class="flex flex-col gap-1.5">
        <label class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Email Address</label>
        <div class="relative">
          <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-tp-outline text-lg">alternate_email</span>
          <input
            v-model="form.email"
            type="email"
            placeholder="commander@hytale.net"
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

      <!-- Password / Access Key -->
      <div class="flex flex-col gap-1.5">
        <div class="flex items-center justify-between">
          <label class="text-xs font-semibold text-tp-muted uppercase tracking-wider">Access Key</label>
          <a href="#" class="text-xs text-tp-accent hover:text-tp-primary transition-colors">Lost credentials?</a>
        </div>
        <div class="relative">
          <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-tp-outline text-lg">lock</span>
          <input
            v-model="form.password"
            :type="showPassword ? 'text' : 'password'"
            placeholder="Enter your access key"
            autocomplete="current-password"
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

      <!-- Keep session active -->
      <div class="flex items-center gap-2.5">
        <input
          id="remember-me"
          v-model="form.rememberMe"
          type="checkbox"
          class="w-4 h-4 rounded bg-tp-surface text-tp-primary focus:ring-tp-primary/50 focus:ring-2 focus:outline-none cursor-pointer accent-tp-primary"
        />
        <label for="remember-me" class="text-sm text-tp-muted cursor-pointer select-none">
          Keep session active
        </label>
      </div>

      <!-- Submit -->
      <button
        type="submit"
        :disabled="authStore.loading"
        class="w-full cta-gradient text-tp-on-primary font-bold text-sm uppercase tracking-wider py-3 rounded-xl flex items-center justify-center gap-2 hover:brightness-110 transition-all disabled:opacity-50 disabled:cursor-not-allowed"
      >
        <svg
          v-if="authStore.loading"
          class="w-4 h-4 animate-spin shrink-0"
          fill="none"
          viewBox="0 0 24 24"
        >
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
        </svg>
        <span>INITIATE LINK</span>
        <span class="material-symbols-outlined text-base">arrow_forward</span>
      </button>
    </form>

    <!-- Register link -->
    <div class="mt-5 text-center">
      <p class="text-tp-muted text-sm">
        New Operator?
        <NuxtLink to="/auth/register" class="text-tp-accent font-semibold hover:text-tp-primary transition-colors ml-1">
          Request Access
        </NuxtLink>
      </p>
    </div>

    <!-- Footer links -->
    <div class="flex items-center justify-center gap-4 mt-6 pt-5">
      <a href="#" class="flex items-center gap-1 text-tp-outline text-xs hover:text-tp-muted transition-colors">
        <span class="material-symbols-outlined text-sm">shield</span>
        Safety Protocols
      </a>
      <a href="#" class="flex items-center gap-1 text-tp-outline text-xs hover:text-tp-muted transition-colors">
        <span class="material-symbols-outlined text-sm">description</span>
        System Logs
      </a>
      <a href="#" class="flex items-center gap-1 text-tp-outline text-xs hover:text-tp-muted transition-colors">
        <span class="material-symbols-outlined text-sm">support</span>
        Support Hub
      </a>
    </div>
  </div>
</template>
