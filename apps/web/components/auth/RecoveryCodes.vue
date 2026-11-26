<script setup lang="ts">
import { Copy, Download, TriangleAlert, Check } from '@lucide/vue'

// Shows a freshly issued set of 2FA recovery codes. The API returns them
// exactly once (only hashes are stored), so this is the user's only chance
// to save them.
const props = defineProps<{
  codes: string[]
  email?: string
}>()

const emit = defineEmits<{
  done: []
}>()

const { t } = useI18n()

const copied = ref(false)
const saved = ref(false)

function asText(): string {
  return [
    t('auth.recoveryCodes.fileTitle'),
    ...(props.email ? [t('auth.recoveryCodes.fileAccount', { email: props.email })] : []),
    t('auth.recoveryCodes.fileGenerated', { date: new Date().toISOString() }),
    '',
    t('auth.recoveryCodes.fileUsage'),
    t('auth.recoveryCodes.fileInvalidate'),
    '',
    ...props.codes,
    '',
  ].join('\n')
}

async function copyCodes() {
  try {
    await navigator.clipboard.writeText(asText())
    copied.value = true
    saved.value = true
    setTimeout(() => { copied.value = false }, 2000)
  } catch {
    // Clipboard unavailable (insecure context, denied permission): the
    // download button still works.
  }
}

function downloadCodes() {
  const blob = new Blob([asText()], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'talepanel-recovery-codes.txt'
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
  saved.value = true
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-start gap-3 bg-tp-warning/10 rounded-xl p-3">
      <TriangleAlert class="w-4 h-4 text-tp-warning shrink-0 mt-0.5" />
      <div class="text-sm">
        <p class="text-tp-text font-semibold">{{ t('auth.recoveryCodes.warningTitle') }}</p>
        <p class="text-tp-muted mt-0.5">
          {{ t('auth.recoveryCodes.warningText') }}
        </p>
      </div>
    </div>

    <ol class="grid grid-cols-2 gap-2 bg-tp-surface2 rounded-xl p-4" :aria-label="t('auth.recoveryCodes.label')">
      <li
        v-for="code in codes"
        :key="code"
        class="font-mono text-tp-text text-sm tracking-wider text-center bg-tp-surface rounded-lg py-1.5 select-all"
      >
        {{ code }}
      </li>
    </ol>

    <div class="flex flex-wrap items-center justify-between gap-2">
      <div class="flex items-center gap-2">
        <UiButton variant="secondary" size="sm" @click="copyCodes">
          <Check v-if="copied" class="w-3.5 h-3.5" />
          <Copy v-else class="w-3.5 h-3.5" />
          {{ copied ? t('actions.copied') : t('actions.copy') }}
        </UiButton>
        <UiButton variant="secondary" size="sm" @click="downloadCodes">
          <Download class="w-3.5 h-3.5" /> {{ t('auth.recoveryCodes.downloadTxt') }}
        </UiButton>
      </div>
      <UiButton variant="primary" size="sm" @click="emit('done')">
        {{ saved ? t('auth.recoveryCodes.done') : t('auth.recoveryCodes.saved') }}
      </UiButton>
    </div>
  </div>
</template>
