<script setup lang="ts">
import type { Server } from '~/types'

interface Props {
  status: Server['status']
  size?: 'sm' | 'md'
}

withDefaults(defineProps<Props>(), {
  size: 'md',
})

type BadgeVariant = 'success' | 'warning' | 'danger' | 'muted' | 'primary'

const statusMap: Record<Server['status'], { variant: BadgeVariant; label: string }> = {
  running:    { variant: 'success', label: 'Online' },
  stopped:    { variant: 'muted',   label: 'Offline' },
  crashed:    { variant: 'danger',  label: 'Crashed' },
  starting:   { variant: 'warning', label: 'Starting' },
  stopping:   { variant: 'warning', label: 'Stopping' },
  installing: { variant: 'primary', label: 'Installing' },
}
</script>

<template>
  <UiBadge :variant="statusMap[status].variant" :size="size">
    {{ statusMap[status].label }}
  </UiBadge>
</template>
