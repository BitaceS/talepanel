<script setup lang="ts">
import type { Server } from '~/types'

interface Props {
  status: Server['status']
  size?: 'sm' | 'md'
}

withDefaults(defineProps<Props>(), {
  size: 'md',
})

const { t } = useI18n()

type BadgeVariant = 'success' | 'warning' | 'danger' | 'muted' | 'primary'

const statusMap: Record<Server['status'], { variant: BadgeVariant; label: string }> = {
  running:    { variant: 'success', label: 'status.online' },
  stopped:    { variant: 'muted',   label: 'status.offline' },
  crashed:    { variant: 'danger',  label: 'status.crashed' },
  starting:   { variant: 'warning', label: 'status.starting' },
  stopping:   { variant: 'warning', label: 'status.stopping' },
  installing: { variant: 'primary', label: 'status.installing' },
}
</script>

<template>
  <UiBadge :variant="statusMap[status].variant" :size="size">
    {{ t(statusMap[status].label) }}
  </UiBadge>
</template>
