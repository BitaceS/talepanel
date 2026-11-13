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

const copied = ref(false)
const saved = ref(false)

function asText(): string {
  return [
    'TalePanel 2FA recovery codes',
    ...(props.email ? [`Account: ${props.email}`] : []),
    `Generated: ${new Date().toISOString()}`,
    '',
    'Each code works once, in place of the code from your authenticator app.',
    'Generating new codes invalidates all of these.',
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
        <p class="text-tp-text font-semibold">Store these recovery codes now. They won't be shown again.</p>
        <p class="text-tp-muted mt-0.5">
          If you lose your authenticator, each code signs you in once instead of the 6-digit code.
          Keep them somewhere safe, such as a password manager.
        </p>
      </div>
    </div>

    <ol class="grid grid-cols-2 gap-2 bg-tp-surface2 rounded-xl p-4" aria-label="Recovery codes">
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
          {{ copied ? 'Copied' : 'Copy' }}
        </UiButton>
        <UiButton variant="secondary" size="sm" @click="downloadCodes">
          <Download class="w-3.5 h-3.5" /> Download .txt
        </UiButton>
      </div>
      <UiButton variant="primary" size="sm" @click="emit('done')">
        {{ saved ? 'Done' : "I've saved them" }}
      </UiButton>
    </div>
  </div>
</template>
