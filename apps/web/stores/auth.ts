import { defineStore } from 'pinia'
import type { User, LoginResponse } from '~/types'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: null as User | null,
    accessToken: null as string | null,
    loading: false,
    partialToken: null as string | null,
  }),

  getters: {
    isAuthenticated: (state): boolean => !!state.accessToken,
    isAdmin: (state): boolean =>
      state.user?.role === 'owner' || state.user?.role === 'admin',
    isOwner: (state): boolean => state.user?.role === 'owner',
  },

  actions: {
    // login returns true when fully authenticated, or false when the account
    // has 2FA enabled and a TOTP code is still required (partialToken is set).
    async login(email: string, password: string): Promise<boolean> {
      this.loading = true
      try {
        const config = useRuntimeConfig()
        const response = await $fetch<LoginResponse>(
          `${config.public.apiBase}/auth/login`,
          {
            method: 'POST',
            body: { email, password },
            credentials: 'include',
          }
        )

        if (response.requires_totp) {
          this.partialToken = response.partial_token ?? null
          return false
        }

        if (response.access_token) {
          this.accessToken = response.access_token
        }
        if (response.user) {
          this.user = response.user
        }

        this.partialToken = null
        await navigateTo('/')
        return true
      } finally {
        this.loading = false
      }
    },

    // completeSetup creates the first owner with the installer's one-time code
    // and keeps the session it returns, like login() does. No navigation: the
    // setup wizard continues with its next step.
    async completeSetup(payload: {
      setup_code: string
      email: string
      username: string
      password: string
    }): Promise<void> {
      this.loading = true
      try {
        const config = useRuntimeConfig()
        const response = await $fetch<LoginResponse>(
          `${config.public.apiBase}/setup/complete`,
          {
            method: 'POST',
            body: payload,
            credentials: 'include',
          }
        )
        this.accessToken = response.access_token ?? null
        this.user = response.user ?? null
        this.partialToken = null
      } finally {
        this.loading = false
      }
    },

    // verifyTotp completes a 2FA-gated login using the partial token from login().
    async verifyTotp(code: string): Promise<void> {
      if (!this.partialToken) {
        throw new Error('No pending 2FA challenge')
      }
      this.loading = true
      try {
        const config = useRuntimeConfig()
        const response = await $fetch<LoginResponse>(
          `${config.public.apiBase}/auth/totp/verify`,
          {
            method: 'POST',
            body: { partial_token: this.partialToken, code },
            credentials: 'include',
          }
        )

        if (response.access_token) {
          this.accessToken = response.access_token
        }
        if (response.user) {
          this.user = response.user
        }

        this.partialToken = null
        await navigateTo('/')
      } finally {
        this.loading = false
      }
    },

    // The access token lives in memory only; a reload re-derives it from the
    // httpOnly refresh cookie. Refresh tokens rotate on use, so concurrent
    // refreshes from several tabs are serialised with a Web Lock - otherwise
    // the loser would present an already-revoked cookie and be logged out.
    async refresh(): Promise<void> {
      const config = useRuntimeConfig()
      const doRefresh = () =>
        $fetch<LoginResponse>(`${config.public.apiBase}/auth/refresh`, {
          method: 'POST',
          credentials: 'include',
        })
      const response = import.meta.client && navigator.locks
        ? await navigator.locks.request('tp_auth_refresh', doRefresh)
        : await doRefresh()

      if (response.access_token) {
        this.accessToken = response.access_token
      }
    },

    async logout(): Promise<void> {
      try {
        const config = useRuntimeConfig()
        await $fetch(`${config.public.apiBase}/auth/logout`, {
          method: 'POST',
          headers: this.accessToken
            ? { Authorization: `Bearer ${this.accessToken}` }
            : {},
          credentials: 'include',
        })
      } catch {
        // Ignore errors on logout - clear state regardless
      } finally {
        this.accessToken = null
        this.user = null
        await navigateTo('/auth/login')
      }
    },

    async fetchMe(): Promise<void> {
      const config = useRuntimeConfig()
      const data = await $fetch<{ user: User }>(`${config.public.apiBase}/auth/me`, {
        headers: this.accessToken
          ? { Authorization: `Bearer ${this.accessToken}` }
          : {},
        credentials: 'include',
      })
      this.user = data.user
    },

    async initialize(): Promise<void> {
      // Older builds persisted the access token in localStorage, where any
      // XSS could read it. Drop it; the refresh cookie restores the session.
      if (import.meta.client) {
        localStorage.removeItem('tp_access_token')
      }

      try {
        await this.refresh()
        await this.fetchMe()
      } catch {
        // No valid session
        this.accessToken = null
        this.user = null
      }
    },
  },
})
