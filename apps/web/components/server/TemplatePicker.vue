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
  <div role="radiogroup" aria-label="Server template" class="grid grid-cols-1 sm:grid-cols-2 gap-2 max-h-96 overflow-y-auto pr-1">
    <button
      type="button"
      role="radio"
      :aria-checked="modelValue === null"
      :class="cardClass(modelValue === null)"
      @click="emit('update:modelValue', null)"
    >
      <span class="material-symbols-outlined text-tp-muted text-xl shrink-0">draft</span>
      <span class="min-w-0">
        <span class="block text-tp-text text-sm font-medium">Blank</span>
        <span class="block text-tp-muted text-xs mt-0.5">Start from the panel defaults and set everything yourself.</span>
      </span>
    </button>

    <button
      v-for="t in templates"
      :key="t.id"
      type="button"
      role="radio"
      :aria-checked="modelValue === t.id"
      :class="cardClass(modelValue === t.id)"
      @click="emit('update:modelValue', t.id)"
    >
      <span class="material-symbols-outlined text-tp-primary text-xl shrink-0">{{ t.icon || 'dns' }}</span>
      <span class="min-w-0">
        <span class="block text-tp-text text-sm font-medium">
          {{ t.name }}
          <span v-if="!t.is_builtin" class="ml-1 text-[10px] uppercase tracking-wider text-tp-outline">custom</span>
        </span>
        <span v-if="t.description" class="block text-tp-muted text-xs mt-0.5">{{ t.description }}</span>
        <span class="flex flex-wrap gap-1 mt-1.5">
          <span
            v-for="chip in templateSummary(t.settings)"
            :key="chip"
            class="text-[10px] px-1.5 py-0.5 rounded bg-tp-surface3 text-tp-muted font-mono"
          >{{ chip }}</span>
        </span>
      </span>
    </button>
  </div>
</template>
