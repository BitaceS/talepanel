<script setup lang="ts">
import { Bot } from '@lucide/vue'
import { useAuthStore } from '~/stores/auth'

// Admin → Integrations: the Discord bot. Slash commands reach the panel at
// its interactions endpoint; the bot token is only used to register them and
// to keep the status message up to date. Admins can read the settings,
// owners change them. The token is never sent back.

interface DiscordSettings {
  enabled: boolean
  application_id: string
  public_key: string
  guild_id: string
  status_channel_id: string
  status_server_ids: string[] | null
  show_player_names: boolean
  language: 'en' | 'de'
  has_bot_token?: boolean
}

interface ServerOption { id: string, name: string }

const { t } = useI18n()
const api = useApi()
const authStore = useAuthStore()
const emit = defineEmits<{ toast: [msg: string, type?: 'success' | 'error'] }>()

const loading = ref(true)
const saving = ref(false)
const error = ref('')
const hasToken = ref(false)
const servers = ref<ServerOption[]>([])
const form = reactive({
  enabled: false,
  application_id: '',
  public_key: '',
  bot_token: '',
  guild_id: '',
  status_channel_id: '',
  status_server_ids: [] as string[],
  show_player_names: false,
  language: 'en' as 'en' | 'de',
})

const endpointUrl = computed(() => `${window.location.origin}/api/v1/discord/interactions`)
// View Channel + Send Messages + Embed Links.
const inviteUrl = computed(() => form.application_id
  ? `https://discord.com/oauth2/authorize?client_id=${encodeURIComponent(form.application_id)}&scope=bot+applications.commands&permissions=19456`
  : '')

function fill(s: DiscordSettings) {
  form.enabled = s.enabled
  form.application_id = s.application_id
  form.public_key = s.public_key
  form.bot_token = ''
  form.guild_id = s.guild_id
  form.status_channel_id = s.status_channel_id
  form.status_server_ids = [...(s.status_server_ids ?? [])]
  form.show_player_names = s.show_player_names
  form.language = s.language === 'de' ? 'de' : 'en'
  hasToken.value = !!s.has_bot_token
}

async function load() {
  loading.value = true
  try {
    const [res, list] = await Promise.all([
      api.get<{ settings: DiscordSettings }>('/admin/discord'),
      api.get<{ servers: ServerOption[] }>('/servers').catch(() => ({ servers: [] as ServerOption[] })),
    ])
    fill(res.settings)
    servers.value = [...(list.servers ?? [])].sort((a, b) => a.name.localeCompare(b.name))
  } catch {
    // Older API without the bot: the card shows the empty form.
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  error.value = ''
  try {
    const res = await api.put<{ settings: DiscordSettings }>('/admin/discord', { ...form })
    fill(res.settings)
    emit('toast', t('admin.discord.saved'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    error.value = e.data?.error ?? t('admin.discord.saveFailed')
  } finally {
    saving.value = false
  }
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    emit('toast', t('actions.copied'))
  } catch {
    // Clipboard blocked: the text is visible and selectable.
  }
}

onMounted(load)

const inputClass = 'w-full bg-tp-surface2 text-tp-text rounded-xl px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-tp-primary/50 disabled:opacity-60'
const labelClass = 'block text-xs font-semibold text-tp-muted mb-1'
const readOnly = computed(() => !authStore.isOwner)
</script>

<template>
  <div class="bg-tp-surface rounded-2xl p-6 space-y-4">
    <div>
      <h3 class="text-tp-text font-display font-semibold text-lg flex items-center gap-2">
        <Bot class="w-4 h-4 text-tp-primary" />
        {{ t('admin.discord.title') }}
      </h3>
      <p class="text-tp-muted text-sm mt-1">{{ t('admin.discord.desc') }}</p>
    </div>

    <p v-if="loading" class="text-tp-muted text-sm">{{ t('common.loading') }}</p>
    <form v-else class="space-y-4" @submit.prevent="save">
      <ol class="rounded-xl bg-tp-surface2 px-4 py-3 text-xs text-tp-muted space-y-1.5 list-decimal list-inside">
        <li>{{ t('admin.discord.steps.create') }}</li>
        <li>{{ t('admin.discord.steps.save') }}</li>
        <li>
          {{ t('admin.discord.steps.endpoint') }}
          <span class="flex items-center gap-2 mt-1">
            <code class="font-mono text-tp-text break-all">{{ endpointUrl }}</code>
            <button type="button" class="text-tp-accent hover:text-tp-primary shrink-0" @click="copy(endpointUrl)">{{ t('actions.copy') }}</button>
          </span>
        </li>
        <li>
          {{ t('admin.discord.steps.invite') }}
          <a v-if="inviteUrl" :href="inviteUrl" target="_blank" rel="noopener noreferrer" class="text-tp-accent hover:text-tp-primary underline">{{ t('admin.discord.inviteLink') }}</a>
          <span v-else class="text-tp-outline">{{ t('admin.discord.inviteNeedsApp') }}</span>
        </li>
        <li>{{ t('admin.discord.steps.enable') }}</li>
      </ol>

      <label class="flex items-center gap-2 text-sm text-tp-text">
        <input v-model="form.enabled" type="checkbox" class="accent-tp-primary" :disabled="readOnly" />
        {{ t('admin.discord.enabled') }}
      </label>

      <div class="grid gap-4 md:grid-cols-2">
        <div>
          <label for="dc-app" :class="labelClass">{{ t('admin.discord.applicationId') }}</label>
          <input id="dc-app" v-model="form.application_id" :class="inputClass" :disabled="readOnly" inputmode="numeric" autocomplete="off" />
        </div>
        <div>
          <label for="dc-key" :class="labelClass">{{ t('admin.discord.publicKey') }}</label>
          <input id="dc-key" v-model="form.public_key" :class="[inputClass, 'font-mono']" :disabled="readOnly" autocomplete="off" />
        </div>
        <div>
          <label for="dc-token" :class="labelClass">{{ t('admin.discord.botToken') }}</label>
          <input
            id="dc-token" v-model="form.bot_token" :class="inputClass" :disabled="readOnly" type="password" autocomplete="new-password"
            :placeholder="hasToken ? t('admin.discord.tokenKept') : ''"
          />
        </div>
        <div>
          <label for="dc-guild" :class="labelClass">{{ t('admin.discord.guildId') }}</label>
          <input id="dc-guild" v-model="form.guild_id" :class="inputClass" :disabled="readOnly" inputmode="numeric" autocomplete="off" />
          <p class="text-tp-outline text-xs mt-1">{{ t('admin.discord.idHint') }}</p>
        </div>
      </div>

      <div class="border-t border-tp-border pt-4 space-y-3">
        <div>
          <h4 class="text-tp-text text-sm font-semibold">{{ t('admin.discord.statusTitle') }}</h4>
          <p class="text-tp-muted text-xs mt-0.5">{{ t('admin.discord.statusDesc') }}</p>
        </div>
        <div class="grid gap-4 md:grid-cols-2">
          <div>
            <label for="dc-channel" :class="labelClass">{{ t('admin.discord.channelId') }}</label>
            <input id="dc-channel" v-model="form.status_channel_id" :class="inputClass" :disabled="readOnly" inputmode="numeric" autocomplete="off" />
            <p class="text-tp-outline text-xs mt-1">{{ t('admin.discord.channelHint') }}</p>
          </div>
          <div>
            <label for="dc-lang" :class="labelClass">{{ t('admin.discord.language') }}</label>
            <select id="dc-lang" v-model="form.language" :class="[inputClass, 'max-w-xs']" :disabled="readOnly">
              <option value="en">English</option>
              <option value="de">Deutsch</option>
            </select>
          </div>
        </div>
        <fieldset>
          <legend :class="labelClass">{{ t('admin.discord.servers') }}</legend>
          <p v-if="!servers.length" class="text-tp-outline text-xs">{{ t('admin.discord.noServers') }}</p>
          <div v-else class="grid gap-1.5 sm:grid-cols-2">
            <label v-for="s in servers" :key="s.id" class="flex items-center gap-2 text-sm text-tp-text">
              <input v-model="form.status_server_ids" type="checkbox" :value="s.id" class="accent-tp-primary" :disabled="readOnly" />
              <span class="truncate">{{ s.name }}</span>
            </label>
          </div>
          <p class="text-tp-outline text-xs mt-1">{{ t('admin.discord.serversHint') }}</p>
        </fieldset>
        <label class="flex items-start gap-2 text-sm text-tp-text">
          <input v-model="form.show_player_names" type="checkbox" class="accent-tp-primary mt-0.5" :disabled="readOnly" />
          <span>
            {{ t('admin.discord.playerNames') }}
            <span class="block text-tp-outline text-xs">{{ t('admin.discord.playerNamesHint') }}</span>
          </span>
        </label>
      </div>

      <p class="text-tp-muted text-xs">{{ t('admin.discord.commandsHint') }}</p>
      <p v-if="error" class="text-tp-danger text-sm" role="alert">{{ error }}</p>

      <div class="flex items-center gap-3">
        <UiButton type="submit" variant="primary" size="sm" :loading="saving" :disabled="readOnly">
          {{ t('actions.save') }}
        </UiButton>
        <span v-if="readOnly" class="text-tp-outline text-xs">{{ t('admin.discord.onlyOwner') }}</span>
      </div>
    </form>
  </div>
</template>
