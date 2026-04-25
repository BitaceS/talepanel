import { useModulesStore } from '~/stores/modules'

// Fetches public boot config (deployment profile) and seeds the modules
// store on first visit.  Existing user choices in localStorage are kept.
export default defineNuxtPlugin(async () => {
  const config = useRuntimeConfig()
  const apiBase = config.public.apiBase as string
  const modulesStore = useModulesStore()

  try {
    const res = await $fetch<{ deployment_profile?: string }>(
      `${apiBase}/health/config`,
      { credentials: 'include' },
    )
    const p = res.deployment_profile === 'hoster' ? 'hoster' : 'solo'
    modulesStore.seedFromBackend(p)
  } catch {
    // network failure / API down — keep whatever load() seeded.
  }
})
