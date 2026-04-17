<script setup lang="ts">
interface Props {
  variant?: 'success' | 'warning' | 'danger' | 'muted' | 'primary' | 'tertiary'
  size?: 'sm' | 'md'
}

const props = withDefaults(defineProps<Props>(), {
  variant: 'muted',
  size: 'md',
})

const variantConfig: Record<
  NonNullable<Props['variant']>,
  { dot: string; badge: string }
> = {
  success: {
    dot: 'bg-tp-success',
    badge: 'bg-tp-success/15 text-tp-success',
  },
  warning: {
    dot: 'bg-tp-warning',
    badge: 'bg-tp-warning/15 text-tp-warning',
  },
  danger: {
    dot: 'bg-tp-danger',
    badge: 'bg-tp-danger/15 text-tp-danger',
  },
  muted: {
    dot: 'bg-tp-muted',
    badge: 'bg-tp-surface3 text-tp-muted',
  },
  primary: {
    dot: 'bg-tp-primary',
    badge: 'bg-tp-primary/15 text-tp-accent',
  },
  tertiary: {
    dot: 'bg-tp-tertiary',
    badge: 'bg-tp-tertiary/15 text-tp-tertiary',
  },
}

const sizeClasses: Record<NonNullable<Props['size']>, string> = {
  sm: 'px-2 py-0.5 text-[10px] gap-1',
  md: 'px-2.5 py-1 text-xs gap-1.5',
}

const dotSizeClasses: Record<NonNullable<Props['size']>, string> = {
  sm: 'w-1.5 h-1.5',
  md: 'w-2 h-2',
}
</script>

<template>
  <span
    :class="[
      'inline-flex items-center rounded-full font-medium',
      variantConfig[variant].badge,
      sizeClasses[size],
    ]"
  >
    <span
      :class="[
        'rounded-full shrink-0',
        variantConfig[variant].dot,
        dotSizeClasses[size],
      ]"
    />
    <slot />
  </span>
</template>
