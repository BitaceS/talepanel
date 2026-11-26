<script setup lang="ts">
import { ShieldCheck, RotateCw, RefreshCw, CheckCircle2, AlertTriangle, XCircle } from '@lucide/vue'
import { useApi } from '~/composables/useApi'
import { useAuthStore } from '~/stores/auth'
import type { MTLSStatus, MTLSNodeState } from '~/types'

// Node certificates (mTLS) and panel CA rotation. Everyone in the admin area
// sees the state; only the owner starts, finishes or cancels a rotation.

const emit = defineEmits<{ toast: [msg: string, type: 'success' | 'error'] }>()

const { t } = useI18n()
const api = useApi()
const authStore = useAuthStore()
const status = ref<MTLSStatus | null>(null)
const loading = ref(false)
const busy = ref<'' | 'start' | 'finish' | 'force' | 'cancel'>('')
const loadError = ref('')

function errMsg(err: unknown, fallback: string): string {
  const e = err as { data?: { error?: string } }
  return e.data?.error ?? fallback
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    status.value = await api.get<MTLSStatus>('/admin/mtls')
  } catch (err: unknown) {
    loadError.value = errMsg(err, t('admin.certificates.loadFailed'))
  } finally {
    loading.value = false
  }
}
onMounted(load)

// While a rotation runs, nodes renew on their next heartbeat (seconds); poll
// so the progress moves without a manual refresh.
let poll: ReturnType<typeof setInterval> | undefined
watch(() => status.value?.rotating, (rotating) => {
  clearInterval(poll)
  if (rotating) poll = setInterval(load, 10_000)
})
onBeforeUnmount(() => clearInterval(poll))

const mtlsNodes = computed(() => status.value?.nodes.filter(n => n.mtls_enabled) ?? [])
const renewedCount = computed(() => mtlsNodes.value.filter(isRenewed).length)

function isRenewed(n: MTLSNodeState): boolean {
  return n.on_active_ca && n.serving
}

function shortFP(fp?: string): string {
  if (!fp) return '—'
  return (fp.slice(0, 16).match(/.{2}/g) ?? []).join(':').toUpperCase()
}

function fmtDate(iso?: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() < 2000) return '—'
  return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}

function daysLeft(iso?: string | null): number | null {
  if (!iso) return null
  const ms = new Date(iso).getTime() - Date.now()
  return Number.isNaN(ms) ? null : Math.floor(ms / 86_400_000)
}

type NodeBadge = { label: string; cls: string; icon: typeof CheckCircle2 }
function nodeBadge(n: MTLSNodeState): NodeBadge {
  if (!n.mtls_enabled) return { label: t('admin.certificates.badges.noMtls'), cls: 'bg-tp-surface2 text-tp-muted', icon: XCircle }
  if (isRenewed(n)) {
    return status.value?.rotating
      ? { label: t('admin.certificates.badges.newCa'), cls: 'bg-tp-success/15 text-tp-success', icon: CheckCircle2 }
      : { label: t('admin.certificates.badges.ok'), cls: 'bg-tp-success/15 text-tp-success', icon: CheckCircle2 }
  }
  if (!n.renewal_supported) return { label: t('admin.certificates.badges.needsUpdate'), cls: 'bg-tp-error/15 text-tp-error', icon: AlertTriangle }
  return { label: t('admin.certificates.badges.renewing'), cls: 'bg-tp-warning/15 text-tp-warning', icon: RotateCw }
}

async function run(kind: 'start' | 'finish' | 'force' | 'cancel') {
  const prompts: Record<typeof kind, string> = {
    start: t('admin.certificates.confirm.start'),
    finish: t('admin.certificates.confirm.finish'),
    force: t('admin.certificates.confirm.force'),
    cancel: t('admin.certificates.confirm.cancel'),
  }
  if (!confirm(prompts[kind])) return
  busy.value = kind
  try {
    const path = kind === 'start' ? '/admin/mtls/rotation'
      : kind === 'cancel' ? '/admin/mtls/rotation/cancel'
        : '/admin/mtls/rotation/finish'
    status.value = await api.post<MTLSStatus>(path, kind === 'force' ? { force: true } : {})
    const done: Record<typeof kind, string> = {
      start: t('admin.certificates.done.start'),
      finish: t('admin.certificates.done.finish'),
      force: t('admin.certificates.done.finish'),
      cancel: t('admin.certificates.done.cancel'),
    }
    emit('toast', done[kind], 'success')
  } catch (err: unknown) {
    emit('toast', errMsg(err, t('admin.certificates.stepFailed')), 'error')
    await load()
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <div class="bg-tp-surface rounded-2xl p-6 space-y-5">
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div class="min-w-0">
        <h3 class="text-tp-text font-display font-semibold text-lg flex items-center gap-2">
          <ShieldCheck class="w-4 h-4 text-tp-primary" />
          {{ t('admin.certificates.title') }}
        </h3>
        <p class="text-tp-muted text-sm mt-1 max-w-2xl">
          {{ t('admin.certificates.desc') }}
        </p>
      </div>
      <UiButton variant="secondary" size="sm" :loading="loading" @click="load">
        <RefreshCw class="w-3.5 h-3.5" />
        {{ t('actions.refresh') }}
      </UiButton>
    </div>

    <div v-if="loadError" class="bg-tp-error/10 rounded-xl px-4 py-3 text-tp-error text-sm">{{ loadError }}</div>

    <template v-if="status">
      <dl class="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <div class="bg-tp-surface2 rounded-xl px-4 py-3 min-w-0">
          <dt class="text-xs text-tp-muted">{{ status.rotating ? t('admin.certificates.newCa') : t('admin.certificates.panelCa') }}</dt>
          <dd class="font-mono text-sm text-tp-text mt-0.5 truncate" :title="status.ca_fingerprint">{{ shortFP(status.ca_fingerprint) }}</dd>
        </div>
        <div class="bg-tp-surface2 rounded-xl px-4 py-3">
          <dt class="text-xs text-tp-muted">{{ t('admin.certificates.caValidUntil') }}</dt>
          <dd class="text-sm text-tp-text mt-0.5">{{ fmtDate(status.ca_not_after) }}</dd>
        </div>
        <div class="bg-tp-surface2 rounded-xl px-4 py-3">
          <dt class="text-xs text-tp-muted">{{ t('admin.certificates.panelCertUntil') }}</dt>
          <dd class="text-sm text-tp-text mt-0.5">{{ fmtDate(status.panel_cert_not_after) }}</dd>
        </div>
      </dl>

      <div v-if="status.rotating" class="bg-tp-warning/10 rounded-xl px-4 py-3 text-sm space-y-1">
        <p class="text-tp-text font-medium flex items-center gap-2">
          <RotateCw class="w-4 h-4 text-tp-warning" />
          {{ t('admin.certificates.rotationProgress', { done: renewedCount, total: mtlsNodes.length }) }}
        </p>
        <p class="text-tp-muted">
          <i18n-t keypath="admin.certificates.oldCaTrusted" tag="span" scope="global">
            <template #fp><span class="font-mono">{{ shortFP(status.previous_ca_fingerprint) }}</span></template>
          </i18n-t>
          {{ t('admin.certificates.renewOnHeartbeat') }}
        </p>
      </div>
      <div v-else-if="status.nodes_without_renewal > 0" class="bg-tp-error/10 rounded-xl px-4 py-3 text-sm text-tp-text flex gap-2">
        <AlertTriangle class="w-4 h-4 text-tp-error shrink-0 mt-0.5" />
        <span>
          {{ t('admin.certificates.oldDaemons', status.nodes_without_renewal) }}
        </span>
      </div>

      <ul v-if="status.nodes.length" class="divide-y divide-tp-outline/10">
        <li v-for="n in status.nodes" :key="n.id" class="flex flex-wrap items-center gap-x-4 gap-y-1 py-2.5">
          <span class="text-tp-text text-sm font-medium truncate min-w-0 flex-1 basis-40">{{ n.name }}</span>
          <span class="text-xs text-tp-muted whitespace-nowrap">
            <template v-if="n.mtls_enabled && n.cert_not_after">
              {{ t('admin.certificates.expires', { date: fmtDate(n.cert_not_after) }) }}
              <span v-if="(daysLeft(n.cert_not_after) ?? 999) < 30" class="text-tp-warning">{{ t('admin.certificates.daysLeft', { n: daysLeft(n.cert_not_after) }) }}</span>
            </template>
            <template v-else-if="!n.mtls_enabled">{{ t('admin.certificates.bearerOnly') }}</template>
          </span>
          <span class="text-xs px-2 py-0.5 rounded-full font-medium whitespace-nowrap inline-flex items-center gap-1" :class="nodeBadge(n).cls">
            <component :is="nodeBadge(n).icon" class="w-3 h-3" />
            {{ nodeBadge(n).label }}
          </span>
        </li>
      </ul>
      <p v-else class="text-tp-muted text-sm">{{ t('admin.certificates.noNodes') }}</p>

      <div v-if="authStore.isOwner" class="flex flex-wrap items-center gap-2">
        <UiButton v-if="!status.rotating" variant="danger" size="sm" :loading="busy === 'start'" :disabled="!!busy" @click="run('start')">
          <RotateCw class="w-3.5 h-3.5" /> {{ t('admin.certificates.rotateCa') }}
        </UiButton>
        <template v-else>
          <UiButton variant="primary" size="sm" :loading="busy === 'finish'" :disabled="!!busy || status.nodes_pending_renewal > 0" @click="run('finish')">
            <CheckCircle2 class="w-3.5 h-3.5" /> {{ t('admin.certificates.finishRotation') }}
          </UiButton>
          <UiButton v-if="status.nodes_pending_renewal > 0" variant="danger" size="sm" :loading="busy === 'force'" :disabled="!!busy" @click="run('force')">
            {{ t('admin.certificates.finishAnyway') }}
          </UiButton>
          <UiButton variant="secondary" size="sm" :loading="busy === 'cancel'" :disabled="!!busy" @click="run('cancel')">
            {{ t('admin.certificates.cancelRotation') }}
          </UiButton>
        </template>
      </div>
      <p v-else class="text-tp-outline text-xs">{{ t('admin.certificates.onlyOwner') }}</p>
    </template>
  </div>
</template>
