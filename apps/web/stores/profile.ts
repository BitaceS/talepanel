import { defineStore } from 'pinia'
import type { User, NotificationPref } from '~/types'

export const useProfileStore = defineStore('profile', {
  state: () => ({
    profile: null as User | null,
    notificationPrefs: [] as NotificationPref[],
    loading: false,
  }),

  actions: {
    async fetchProfile() {
      const api = useApi()
      this.loading = true
      try {
        const data = await api.get<{ user: User }>('/auth/profile')
        this.profile = data.user
      } finally {
        this.loading = false
      }
    },

    async updateProfile(updates: {
      display_name?: string
      avatar_url?: string
      language?: string
      timezone?: string
    }) {
      const api = useApi()
      const data = await api.patch<{ user: User }>('/auth/profile', updates)
      this.profile = data.user
      return data.user
    },

    async fetchNotificationPrefs() {
      const api = useApi()
      const data = await api.get<{ preferences: NotificationPref[] }>(
        '/auth/profile/notifications'
      )
      this.notificationPrefs = data.preferences
    },

    async setNotificationPref(pref: {
      alert_type: string
      email: boolean
      discord: boolean
      telegram: boolean
    }) {
      const api = useApi()
      await api.put('/auth/profile/notifications', pref)
      await this.fetchNotificationPrefs()
    },
  },
})
