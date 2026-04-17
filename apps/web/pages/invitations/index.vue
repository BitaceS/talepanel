<script setup lang="ts">
import { useInvitationsStore } from '~/stores/invitations'

definePageMeta({ title: 'My Invitations', middleware: 'auth' })

const invStore = useInvitationsStore()
const loading = ref(false)

onMounted(async () => {
  loading.value = true
  await invStore.fetchMyInvitations()
  loading.value = false
})

async function accept(token: string) {
  await invStore.acceptInvitation(token)
}

async function decline(token: string) {
  await invStore.declineInvitation(token)
}
</script>

<template>
  <div class="p-6 max-w-3xl">
    <h2 class="text-tp-text font-display font-bold text-2xl mb-6">My Invitations</h2>

    <div class="bg-tp-surface rounded-xl overflow-hidden">
      <div v-if="loading" class="p-8 text-center text-tp-muted text-sm">Loading...</div>
      <div v-else-if="invStore.myInvitations.length === 0" class="p-8 text-center text-tp-muted text-sm">
        No pending invitations
      </div>
      <div v-else class="divide-y divide-tp-border">
        <div v-for="inv in invStore.myInvitations" :key="inv.id" class="px-5 py-4 flex items-center justify-between gap-4">
          <div class="flex-1 min-w-0">
            <p class="text-tp-text text-sm font-medium">Server invitation</p>
            <p class="text-tp-muted text-xs">Role: {{ inv.role }} &middot; Expires {{ new Date(inv.expires_at).toLocaleDateString() }}</p>
          </div>
          <div class="flex gap-2">
            <button
              @click="accept(inv.token!)"
              class="px-3 py-1.5 bg-tp-success/15 text-tp-success rounded-xl text-xs font-medium hover:bg-tp-success/25 transition-colors"
            >
              Accept
            </button>
            <button
              @click="decline(inv.token!)"
              class="px-3 py-1.5 bg-tp-error/10 text-tp-error rounded-xl text-xs font-medium hover:bg-tp-error/20 transition-colors"
            >
              Decline
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
