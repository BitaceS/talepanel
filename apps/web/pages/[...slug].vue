<script setup lang="ts">
definePageMeta({
  middleware: 'auth',
})

const route = useRoute()
const pageName = computed(() => {
  const slug = route.params.slug
  const segment = Array.isArray(slug) ? slug[0] : slug
  return segment ? segment.charAt(0).toUpperCase() + segment.slice(1) : 'This page'
})
</script>

<template>
  <div class="p-6 flex items-center justify-center min-h-96">
    <div class="bg-tp-surface rounded-2xl p-16 text-center max-w-sm w-full">
      <div class="w-12 h-12 bg-tp-surface2 rounded-xl flex items-center justify-center mx-auto mb-4">
        <span class="text-tp-muted text-xl font-bold">—</span>
      </div>
      <h4 class="text-tp-text font-display font-semibold text-lg mb-2">{{ pageName }}</h4>
      <p class="text-tp-muted text-sm">Page not found.</p>
      <NuxtLink to="/" class="inline-block mt-4 text-tp-accent text-sm hover:text-tp-primary transition-colors">Back to Dashboard</NuxtLink>
    </div>
  </div>
</template>
