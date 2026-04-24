<script setup lang="ts">
definePageMeta({ layout: 'auth', title: 'First-time Setup' })

const api = useApi()

onMounted(async () => {
  try {
    const data = await api.get<{ needs_setup: boolean }>('/health/setup')
    if (!data.needs_setup) {
      await navigateTo('/auth/login')
    }
  } catch {
    await navigateTo('/auth/login')
  }
})
</script>

<template>
  <div>
    <!-- Title -->
    <div class="mb-6">
      <h2 class="text-tp-text font-display font-bold text-xl">Welcome to TalePanel</h2>
      <p class="text-tp-muted text-sm mt-1">No admin account exists yet. Create one to get started.</p>
    </div>

    <!-- Instructions card -->
    <div class="bg-tp-surface rounded-xl p-5 space-y-4 mb-5">
      <div class="flex items-center gap-2">
        <span class="material-symbols-outlined text-tp-accent text-lg">terminal</span>
        <p class="text-tp-text font-medium text-sm">Run this command on your panel server:</p>
      </div>
      <pre class="bg-tp-bg rounded-lg p-4 text-sm font-mono text-tp-accent overflow-x-auto">docker compose run --rm api tale-cli admin create</pre>
      <p class="text-sm text-tp-muted">
        This creates the first owner account. Once done,
        <NuxtLink to="/auth/login" class="text-tp-accent font-semibold hover:text-tp-primary transition-colors">
          sign in here
        </NuxtLink>.
      </p>
    </div>

    <!-- Outside Docker note -->
    <p class="text-center text-xs text-tp-muted">
      Running TalePanel outside Docker? Use
      <code class="font-mono text-tp-accent">./tale-cli admin create</code> directly.
    </p>
  </div>
</template>
