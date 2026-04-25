export interface UpdateInfo {
  current_version: string
  latest_version: string
  has_update: boolean
  release_url: string
  published_at: string
}

interface PublicConfig {
  deployment_profile: string
  version?: string
  latest_version?: string
  has_update?: boolean
  release_url?: string
  published_at?: string
}

// Pull update info from the public /health/config endpoint so the banner
// works for non-admin users too.  The API caches the GitHub lookup for 24h
// in Redis, so this call is cheap.
export function useUpdateCheck() {
  const api = useApi()
  const info = ref<UpdateInfo | null>(null)
  const dismissed = ref(false)

  async function check() {
    try {
      const data = await api.get<PublicConfig>('/health/config')
      if (data.version && data.latest_version) {
        info.value = {
          current_version: data.version,
          latest_version: data.latest_version,
          has_update: !!data.has_update,
          release_url: data.release_url ?? '',
          published_at: data.published_at ?? '',
        }
      }
    } catch {
      // Network errors — silently ignore.
    }
  }

  onMounted(() => {
    check()
  })

  return { info, dismissed }
}
