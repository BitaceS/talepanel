<script setup lang="ts">
import { LogIn } from '@lucide/vue'
import { useAuthStore } from '~/stores/auth'

// Admin → Integrations: the OpenID Connect provider for "Sign in with …".
// Admins can read it, owners change it. The client secret is never sent
// back; leaving the field empty keeps the stored one.

interface SsoSettings {
  enabled: boolean
  display_name: string
  issuer_url: string
  client_id: string
  client_secret?: string
  panel_url: string
  allowed_domains: string[]
  auto_create: boolean
  default_role: 'user' | 'moderator'
  trust_mfa: boolean
  has_client_secret?: boolean
  redirect_url?: string
}

const { t } = useI18n()
const api = useApi()
const authStore = useAuthStore()
const emit = defineEmits<{ toast: [msg: string, type?: 'success' | 'error'] }>()

const loading = ref(true)
const saving = ref(false)
const error = ref('')
const hasSecret = ref(false)
const form = reactive({
  enabled: false,
  display_name: '',
  issuer_url: '',
  client_id: '',
  client_secret: '',
  panel_url: '',
  domains: '',
  auto_create: false,
  default_role: 'user' as 'user' | 'moderator',
  trust_mfa: false,
})

// The callback URL to register at the provider, from the panel URL field.
const redirectUrl = computed(() => {
  const base = (form.panel_url || window.location.origin).trim().replace(/\/+$/, '')
  return `${base}/api/v1/auth/oidc/callback`
})

function fill(s: SsoSettings) {
  form.enabled = s.enabled
  form.display_name = s.display_name === 'SSO' && !s.issuer_url ? '' : s.display_name
  form.issuer_url = s.issuer_url
  form.client_id = s.client_id
  form.client_secret = ''
  form.panel_url = s.panel_url || window.location.origin
  form.domains = (s.allowed_domains ?? []).join(', ')
  form.auto_create = s.auto_create
  form.default_role = s.default_role === 'moderator' ? 'moderator' : 'user'
  form.trust_mfa = s.trust_mfa
  hasSecret.value = !!s.has_client_secret
}

async function load() {
  loading.value = true
  try {
    const res = await api.get<{ settings: SsoSettings }>('/admin/sso')
    fill(res.settings)
  } catch {
    // Older API without SSO: the card shows the empty form.
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  error.value = ''
  try {
    const res = await api.put<{ settings: SsoSettings }>('/admin/sso', {
      enabled: form.enabled,
      display_name: form.display_name,
      issuer_url: form.issuer_url,
      client_id: form.client_id,
      client_secret: form.client_secret,
      panel_url: form.panel_url,
      allowed_domains: form.domains.split(/[\s,]+/).filter(Boolean),
      auto_create: form.auto_create,
      default_role: form.default_role,
      trust_mfa: form.trust_mfa,
    })
    fill(res.settings)
    emit('toast', t('admin.sso.saved'))
  } catch (err: unknown) {
    const e = err as { data?: { error?: string } }
    error.value = e.data?.error ?? t('admin.sso.saveFailed')
  } finally {
    saving.value = false
  }
}

async function copyRedirect() {
  try {
    await navigator.clipboard.writeText(redirectUrl.value)
    emit('toast', t('actions.copied'))
  } catch {
    // Clipboard blocked: the URL is visible and selectable.
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
        <LogIn class="w-4 h-4 text-tp-primary" />
        {{ t('admin.sso.title') }}
      </h3>
      <p class="text-tp-muted text-sm mt-1">{{ t('admin.sso.desc') }}</p>
    </div>

    <p v-if="loading" class="text-tp-muted text-sm">{{ t('common.loading') }}</p>
    <form v-else class="space-y-4" @submit.prevent="save">
      <label class="flex items-center gap-2 text-sm text-tp-text">
        <input v-model="form.enabled" type="checkbox" class="accent-tp-primary" :disabled="readOnly" />
        {{ t('admin.sso.enabled') }}
      </label>

      <div class="grid gap-4 md:grid-cols-2">
        <div>
          <label for="sso-name" :class="labelClass">{{ t('admin.sso.displayName') }}</label>
          <input id="sso-name" v-model="form.display_name" :class="inputClass" :disabled="readOnly" maxlength="40" placeholder="Keycloak" />
        </div>
        <div>
          <label for="sso-issuer" :class="labelClass">{{ t('admin.sso.issuerUrl') }}</label>
          <input id="sso-issuer" v-model="form.issuer_url" :class="inputClass" :disabled="readOnly" type="url" placeholder="https://id.example.com/realms/main" />
        </div>
        <div>
          <label for="sso-client" :class="labelClass">{{ t('admin.sso.clientId') }}</label>
          <input id="sso-client" v-model="form.client_id" :class="inputClass" :disabled="readOnly" autocomplete="off" />
        </div>
        <div>
          <label for="sso-secret" :class="labelClass">{{ t('admin.sso.clientSecret') }}</label>
          <input
            id="sso-secret" v-model="form.client_secret" :class="inputClass" :disabled="readOnly" type="password" autocomplete="new-password"
            :placeholder="hasSecret ? t('admin.sso.secretKept') : ''"
          />
        </div>
        <div>
          <label for="sso-panel" :class="labelClass">{{ t('admin.sso.panelUrl') }}</label>
          <input id="sso-panel" v-model="form.panel_url" :class="inputClass" :disabled="readOnly" type="url" />
        </div>
        <div>
          <label for="sso-domains" :class="labelClass">{{ t('admin.sso.domains') }}</label>
          <input id="sso-domains" v-model="form.domains" :class="inputClass" :disabled="readOnly" placeholder="example.com" />
          <p class="text-tp-outline text-xs mt-1">{{ t('admin.sso.domainsHint') }}</p>
        </div>
      </div>

      <div class="rounded-xl bg-tp-surface2 px-4 py-3 text-xs space-y-1">
        <p class="text-tp-muted">{{ t('admin.sso.redirectHint') }}</p>
        <div class="flex items-center gap-2">
          <code class="font-mono text-tp-text break-all">{{ redirectUrl }}</code>
          <button type="button" class="text-tp-accent hover:text-tp-primary shrink-0" @click="copyRedirect">{{ t('actions.copy') }}</button>
        </div>
      </div>

      <div class="space-y-3">
        <label class="flex items-start gap-2 text-sm text-tp-text">
          <input v-model="form.auto_create" type="checkbox" class="accent-tp-primary mt-0.5" :disabled="readOnly" />
          <span>
            {{ t('admin.sso.autoCreate') }}
            <span class="block text-tp-outline text-xs">{{ t('admin.sso.autoCreateHint') }}</span>
          </span>
        </label>
        <div v-if="form.auto_create" class="pl-6">
          <label for="sso-role" :class="labelClass">{{ t('admin.sso.defaultRole') }}</label>
          <select id="sso-role" v-model="form.default_role" :class="[inputClass, 'max-w-xs']" :disabled="readOnly">
            <option value="user">{{ t('roles.user') }}</option>
            <option value="moderator">{{ t('roles.moderator') }}</option>
          </select>
        </div>
        <label class="flex items-start gap-2 text-sm text-tp-text">
          <input v-model="form.trust_mfa" type="checkbox" class="accent-tp-primary mt-0.5" :disabled="readOnly" />
          <span>
            {{ t('admin.sso.trustMfa') }}
            <span class="block text-tp-outline text-xs">{{ t('admin.sso.trustMfaHint') }}</span>
          </span>
        </label>
      </div>

      <p class="text-tp-muted text-xs">{{ t('admin.sso.linkHint') }}</p>
      <p v-if="error" class="text-tp-danger text-sm" role="alert">{{ error }}</p>

      <div class="flex items-center gap-3">
        <UiButton type="submit" variant="primary" size="sm" :loading="saving" :disabled="readOnly">
          {{ t('actions.save') }}
        </UiButton>
        <span v-if="readOnly" class="text-tp-outline text-xs">{{ t('admin.sso.onlyOwner') }}</span>
      </div>
    </form>
  </div>
</template>
