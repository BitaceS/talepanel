<script setup lang="ts">
interface Props {
  padding?: 'sm' | 'md' | 'lg'
  hoverable?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  padding: 'md',
  hoverable: false,
})

const paddingClasses: Record<NonNullable<Props['padding']>, string> = {
  sm: 'p-4',
  md: 'p-5',
  lg: 'p-6',
}

const slots = useSlots()
const hasHeader = computed(() => !!slots.header)
const hasFooter = computed(() => !!slots.footer)
</script>

<template>
  <div
    :class="[
      'bg-tp-surface2 rounded-xl',
      hoverable ? 'hover:bg-tp-surface3 transition-all duration-200 cursor-pointer' : '',
    ]"
  >
    <!-- Header slot -->
    <div
      v-if="hasHeader"
      class="px-5 py-3"
    >
      <slot name="header" />
    </div>

    <!-- Body -->
    <div :class="paddingClasses[padding]">
      <slot />
    </div>

    <!-- Footer slot -->
    <div
      v-if="hasFooter"
      class="px-5 py-3 bg-tp-surface3/50 rounded-b-xl"
    >
      <slot name="footer" />
    </div>
  </div>
</template>
