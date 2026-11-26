<script setup lang="ts">
import { useInvitationsStore } from '~/stores/invitations'

definePageMeta({ title: 'pages.invitations', middleware: 'auth' })

const invStore = useInvitationsStore()
const { t, locale } = useI18n()
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
    <h2 class="text-tp-text font-display font-bold text-2xl mb-6">{{ t('pages.invitations') }}</h2>

    <div class="bg-tp-surface rounded-xl overflow-hidden">
      <div v-if="loading" class="p-8 text-center text-tp-muted text-sm">{{ t('common.loading') }}</div>
      <div v-else-if="invStore.myInvitations.length === 0" class="p-8 text-center text-tp-muted text-sm">
        {{ t('settings.invitations.empty') }}
      </div>
      <div v-else class="divide-y divide-tp-border">
        <div v-for="inv in invStore.myInvitations" :key="inv.id" class="px-5 py-4 flex items-center justify-between gap-4">
          <div class="flex-1 min-w-0">
            <p class="text-tp-text text-sm font-medium">{{ t('settings.invitations.title') }}</p>
            <p class="text-tp-muted text-xs">{{ t('settings.invitations.role', { role: inv.role }) }} &middot; {{ t('settings.invitations.expires', { date: new Date(inv.expires_at).toLocaleDateString(locale) }) }}</p>
          </div>
          <div class="flex gap-2">
            <button
              @click="accept(inv.token!)"
              class="px-3 py-1.5 bg-tp-success/15 text-tp-success rounded-xl text-xs font-medium hover:bg-tp-success/25 transition-colors"
            >
              {{ t('settings.invitations.accept') }}
            </button>
            <button
              @click="decline(inv.token!)"
              class="px-3 py-1.5 bg-tp-error/10 text-tp-error rounded-xl text-xs font-medium hover:bg-tp-error/20 transition-colors"
            >
              {{ t('settings.invitations.decline') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
