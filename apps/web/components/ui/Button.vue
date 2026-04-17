<script setup lang="ts">
interface Props {
  variant?: 'primary' | 'secondary' | 'danger' | 'ghost'
  size?: 'sm' | 'md' | 'lg'
  loading?: boolean
  disabled?: boolean
  type?: 'button' | 'submit'
}

const props = withDefaults(defineProps<Props>(), {
  variant: 'primary',
  size: 'md',
  loading: false,
  disabled: false,
  type: 'button',
})

const emit = defineEmits<{
  click: [event: MouseEvent]
}>()

const variantClasses: Record<NonNullable<Props['variant']>, string> = {
  primary: 'bg-tp-primary text-tp-on-primary hover:brightness-110 focus:ring-tp-primary/50',
  secondary: 'bg-tp-surface3 text-tp-text hover:bg-tp-surface-highest focus:ring-tp-surface3/50',
  danger: 'bg-tp-danger text-white hover:bg-tp-danger/90 focus:ring-tp-danger/50',
  ghost: 'text-tp-muted hover:text-tp-text hover:bg-tp-surface2 focus:ring-tp-surface2/50',
}

const sizeClasses: Record<NonNullable<Props['size']>, string> = {
  sm: 'px-3 py-1.5 text-xs rounded-lg',
  md: 'px-4 py-2.5 text-sm rounded-xl',
  lg: 'px-5 py-3 text-base rounded-xl',
}

const classes = computed(() => [
  'inline-flex items-center justify-center gap-2 font-semibold transition-all duration-150 focus:outline-none focus:ring-2',
  variantClasses[props.variant],
  sizeClasses[props.size],
  (props.disabled || props.loading) ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer',
])

function handleClick(event: MouseEvent) {
  if (!props.disabled && !props.loading) {
    emit('click', event)
  }
}
</script>

<template>
  <button
    :type="type"
    :class="classes"
    :disabled="disabled || loading"
    @click="handleClick"
  >
    <!-- Spinner -->
    <svg
      v-if="loading"
      class="w-4 h-4 animate-spin shrink-0"
      xmlns="http://www.w3.org/2000/svg"
      fill="none"
      viewBox="0 0 24 24"
    >
      <circle
        class="opacity-25"
        cx="12"
        cy="12"
        r="10"
        stroke="currentColor"
        stroke-width="4"
      />
      <path
        class="opacity-75"
        fill="currentColor"
        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
      />
    </svg>

    <slot />
  </button>
</template>
