export interface UpdateInfo {
  current_version: string
  latest_version: string
  has_update: boolean
  release_url: string
  published_at: string
}

export function useUpdateCheck() {
  const api = useApi()
  const info = ref<UpdateInfo | null>(null)
  const dismissed = ref(false)

  async function check() {
    try {
      const data = await api.get<UpdateInfo>('/admin/update/check')
      info.value = data
    } catch {
      // non-admin users or network errors — silently ignore
    }
  }

  onMounted(() => {
    check()
  })

  return { info, dismissed }
}
