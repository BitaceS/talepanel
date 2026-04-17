import { defineStore } from 'pinia'
import type { User, LoginResponse } from '~/types'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: null as User | null,
    accessToken: null as string | null,
    loading: false,
  }),

  getters: {
    isAuthenticated: (state): boolean => !!state.accessToken,
    isAdmin: (state): boolean =>
      state.user?.role === 'owner' || state.user?.role === 'admin',
    isOwner: (state): boolean => state.user?.role === 'owner',
  },

  actions: {
    _loadTokenFromStorage() {
      if (import.meta.client) {
        const stored = localStorage.getItem('tp_access_token')
        if (stored) {
          this.accessToken = stored
        }
      }
    },

    _saveTokenToStorage(token: string | null) {
      if (import.meta.client) {
        if (token) {
          localStorage.setItem('tp_access_token', token)
        } else {
          localStorage.removeItem('tp_access_token')
        }
      }
    },

    async login(email: string, password: string): Promise<void> {
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

        if (response.access_token) {
          this.accessToken = response.access_token
          this._saveTokenToStorage(response.access_token)
        }
        if (response.user) {
          this.user = response.user
        }

        await navigateTo('/')
      } finally {
        this.loading = false
      }
    },

    async refresh(): Promise<void> {
      const config = useRuntimeConfig()
      const response = await $fetch<LoginResponse>(
        `${config.public.apiBase}/auth/refresh`,
        {
          method: 'POST',
          credentials: 'include',
        }
      )

      if (response.access_token) {
        this.accessToken = response.access_token
        this._saveTokenToStorage(response.access_token)
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
        this._saveTokenToStorage(null)
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
      this._loadTokenFromStorage()

      if (this.accessToken) {
        try {
          await this.fetchMe()
        } catch (err: unknown) {
          const fetchErr = err as { status?: number }
          // Token expired - try refresh
          if (fetchErr.status === 401) {
            try {
              await this.refresh()
              await this.fetchMe()
            } catch {
              // Refresh failed - clear state
              this.accessToken = null
              this.user = null
              this._saveTokenToStorage(null)
            }
          } else {
            this.accessToken = null
            this.user = null
            this._saveTokenToStorage(null)
          }
        }
      } else {
        // No stored token - try refresh cookie
        try {
          await this.refresh()
          await this.fetchMe()
        } catch {
          // No valid session
        }
      }
    },
  },
})
