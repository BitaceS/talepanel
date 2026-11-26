<script setup lang="ts">
import { Bot, Unlink } from '@lucide/vue'

// Settings → Security: link a Discord account for the bot's /server
// commands. The panel makes a one-time code, the user types it into /link in
// Discord. Shown only while the bot is enabled.

interface DiscordLink { discord_user_id: string, discord_username: string, linked_at: string }

const { t } = useI18n()
const api = useApi()
const emit = defineEmits<{ toast: [msg: string, kind?: 'ok' | 'error'] }>()

const enabled = ref(false)
const link = ref<DiscordLink | null>(null)
const code = ref('')
const busy = ref(false)

async function load() {
  try {
    const res = await api.get<{ enabled: boolean, link: DiscordLink | null }>('/auth/discord')
    enabled.value = res.enabled
    link.value = res.link
    if (res.link) code.value = ''
  } catch {
    enabled.value = false
  }
}

async function createCode() {
  busy.value = true
  try {
    code.value = (await api.post<{ code: string }>('/auth/discord/code')).code
  } catch {
    emit('toast', t('settings.security.discord.codeFailed'), 'error')
  } finally {
    busy.value = false
  }
}

async function unlink() {
  if (!confirm(t('settings.security.discord.unlinkConfirm'))) return
  busy.value = true
  try {
    await api.delete('/auth/discord')
    link.value = null
    emit('toast', t('settings.security.discord.unlinked'))
  } catch {
    emit('toast', t('settings.security.discord.unlinkFailed'), 'error')
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <section v-if="enabled" class="bg-tp-surface rounded-xl overflow-hidden">
    <div class="px-5 py-4 border-b border-tp-border">
      <h2 class="text-tp-text font-display font-semibold text-sm flex items-center gap-2">
        <Bot class="w-4 h-4 text-tp-primary" />
        {{ t('settings.security.discord.title') }}
      </h2>
      <p class="text-tp-muted text-xs mt-0.5">{{ t('settings.security.discord.text') }}</p>
    </div>
    <div class="p-5 space-y-3">
      <div v-if="link" class="flex flex-wrap items-center justify-between gap-3">
        <p class="text-sm text-tp-success">
          {{ t('settings.security.discord.linked', { name: link.discord_username || link.discord_user_id }) }}
        </p>
        <UiButton variant="secondary" size="sm" :loading="busy" @click="unlink">
          <Unlink class="w-3.5 h-3.5" />
          {{ t('settings.security.discord.unlink') }}
        </UiButton>
      </div>
      <template v-else>
        <div v-if="code" class="space-y-2">
          <p class="text-sm text-tp-text">{{ t('settings.security.discord.codeIntro') }}</p>
          <code class="block w-fit rounded-xl bg-tp-surface2 px-4 py-2 font-mono text-lg text-tp-text tracking-widest select-all">/link code:{{ code }}</code>
          <p class="text-tp-outline text-xs">{{ t('settings.security.discord.codeHint') }}</p>
          <UiButton variant="secondary" size="sm" @click="load">{{ t('settings.security.discord.done') }}</UiButton>
        </div>
        <div v-else class="flex flex-wrap items-center justify-between gap-3">
          <p class="text-sm text-tp-muted">{{ t('settings.security.discord.notLinked') }}</p>
          <UiButton variant="secondary" size="sm" :loading="busy" @click="createCode">
            {{ t('settings.security.discord.createCode') }}
          </UiButton>
        </div>
      </template>
    </div>
  </section>
</template>
