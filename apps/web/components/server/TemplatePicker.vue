<script setup lang="ts">
import type { ServerTemplate } from '~/types'

// Card grid for choosing a server template in the create-server form.
// v-model is the chosen template's id, or null for "Blank".

interface Props {
  modelValue: string | null
  templates: ServerTemplate[]
}

defineProps<Props>()
const emit = defineEmits<{ 'update:modelValue': [value: string | null] }>()
const { t } = useI18n()

function cardClass(selected: boolean) {
  return [
    'w-full text-left rounded-xl border p-3 transition-colors flex gap-3 items-start',
    selected
      ? 'border-tp-primary bg-tp-primary/10'
      : 'border-tp-border bg-tp-surface hover:bg-tp-surface2',
  ]
}
</script>

<template>
  <div role="radiogroup" :aria-label="t('servers.templatePicker.aria')" class="grid grid-cols-1 sm:grid-cols-2 gap-2 max-h-96 overflow-y-auto pr-1">
    <button
      type="button"
      role="radio"
      :aria-checked="modelValue === null"
      :class="cardClass(modelValue === null)"
      @click="emit('update:modelValue', null)"
    >
      <span class="material-symbols-outlined text-tp-muted text-xl shrink-0">draft</span>
      <span class="min-w-0">
        <span class="block text-tp-text text-sm font-medium">{{ t('servers.templatePicker.blank') }}</span>
        <span class="block text-tp-muted text-xs mt-0.5">{{ t('servers.templatePicker.blankHint') }}</span>
      </span>
    </button>

    <button
      v-for="tpl in templates"
      :key="tpl.id"
      type="button"
      role="radio"
      :aria-checked="modelValue === tpl.id"
      :class="cardClass(modelValue === tpl.id)"
      @click="emit('update:modelValue', tpl.id)"
    >
      <span class="material-symbols-outlined text-tp-primary text-xl shrink-0">{{ tpl.icon || 'dns' }}</span>
      <span class="min-w-0">
        <span class="block text-tp-text text-sm font-medium">
          {{ tpl.name }}
          <span v-if="!tpl.is_builtin" class="ml-1 text-[10px] uppercase tracking-wider text-tp-outline">{{ t('servers.templatePicker.custom') }}</span>
        </span>
        <span v-if="tpl.description" class="block text-tp-muted text-xs mt-0.5">{{ tpl.description }}</span>
        <span class="flex flex-wrap gap-1 mt-1.5">
          <span
            v-for="chip in templateSummary(tpl.settings, (k, n) => t(k, n))"
            :key="chip"
            class="text-[10px] px-1.5 py-0.5 rounded bg-tp-surface3 text-tp-muted font-mono"
          >{{ chip }}</span>
        </span>
      </span>
    </button>
  </div>
</template>
