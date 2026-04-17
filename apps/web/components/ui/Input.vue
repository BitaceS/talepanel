<script setup lang="ts">
interface Props {
  modelValue?: string | number
  type?: string
  placeholder?: string
  label?: string
  error?: string
  required?: boolean
  disabled?: boolean
  autocomplete?: string
}

const props = withDefaults(defineProps<Props>(), {
  type: 'text',
  required: false,
  disabled: false,
})

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const id = `input-${Math.random().toString(36).slice(2, 8)}`

function onInput(event: Event) {
  emit('update:modelValue', (event.target as HTMLInputElement).value)
}
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <label
      v-if="label"
      :for="id"
      class="text-xs font-semibold text-tp-muted uppercase tracking-wider"
    >
      {{ label }}
      <span v-if="required" class="text-tp-danger ml-0.5">*</span>
    </label>

    <input
      :id="id"
      :type="type"
      :value="modelValue"
      :placeholder="placeholder"
      :required="required"
      :disabled="disabled"
      :autocomplete="autocomplete"
      :class="[
        'w-full bg-tp-surface text-tp-text rounded-xl px-4 py-2.5 text-sm',
        'placeholder:text-tp-outline',
        'focus:outline-none focus:bg-tp-surface-lowest focus:ring-2 focus:ring-tp-primary/50',
        'transition-all duration-150',
        error ? 'ring-2 ring-tp-danger/50' : '',
        disabled ? 'opacity-50 cursor-not-allowed' : '',
      ]"
      @input="onInput"
    />

    <p v-if="error" class="text-tp-danger text-xs">
      {{ error }}
    </p>
  </div>
</template>
