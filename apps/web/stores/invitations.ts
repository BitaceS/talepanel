import { defineStore } from 'pinia'
import type { ServerInvitation } from '~/types'

export const useInvitationsStore = defineStore('invitations', {
  state: () => ({
    serverInvitations: [] as ServerInvitation[],
    myInvitations: [] as ServerInvitation[],
    loading: false,
  }),

  actions: {
    async fetchServerInvitations(serverId: string) {
      const api = useApi()
      this.loading = true
      try {
        const data = await api.get<{ invitations: ServerInvitation[] }>(
          `/servers/${serverId}/invitations`
        )
        this.serverInvitations = data.invitations
      } finally {
        this.loading = false
      }
    },

    async createInvitation(
      serverId: string,
      inviteeEmail: string,
      role: string = 'viewer'
    ) {
      const api = useApi()
      const data = await api.post<{ invitation: ServerInvitation }>(
        `/servers/${serverId}/invitations`,
        { invitee_email: inviteeEmail, role }
      )
      this.serverInvitations.unshift(data.invitation)
      return data.invitation
    },

    async revokeInvitation(serverId: string, invitationId: string) {
      const api = useApi()
      await api.delete(`/servers/${serverId}/invitations/${invitationId}`)
      this.serverInvitations = this.serverInvitations.filter(
        (i) => i.id !== invitationId
      )
    },

    async fetchMyInvitations() {
      const api = useApi()
      this.loading = true
      try {
        const data = await api.get<{ invitations: ServerInvitation[] }>(
          '/invitations/mine'
        )
        this.myInvitations = data.invitations
      } finally {
        this.loading = false
      }
    },

    async acceptInvitation(token: string) {
      const api = useApi()
      await api.post(`/invitations/${token}/accept`)
      this.myInvitations = this.myInvitations.filter((i) => i.token !== token)
    },

    async declineInvitation(token: string) {
      const api = useApi()
      await api.post(`/invitations/${token}/decline`)
      this.myInvitations = this.myInvitations.filter((i) => i.token !== token)
    },
  },
})
