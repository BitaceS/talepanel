import { useAuthStore } from '~/stores/auth'

export const useApi = () => {
  const config = useRuntimeConfig()
  const authStore = useAuthStore()

  const request = async <T>(
    path: string,
    options: Parameters<typeof $fetch>[1] = {}
  ): Promise<T> => {
    const headers: Record<string, string> = {}

    if (authStore.accessToken) {
      headers['Authorization'] = `Bearer ${authStore.accessToken}`
    }

    try {
      return await $fetch<T>(`${config.public.apiBase}${path}`, {
        ...options,
        headers: {
          ...headers,
          ...(options.headers as Record<string, string> | undefined),
        },
        credentials: 'include',
      })
    } catch (err: unknown) {
      const fetchErr = err as { status?: number }

      // Handle 401: try to refresh token
      if (fetchErr.status === 401 && authStore.accessToken) {
        try {
          await authStore.refresh()
          // Retry with new token
          headers['Authorization'] = `Bearer ${authStore.accessToken}`
          return await $fetch<T>(`${config.public.apiBase}${path}`, {
            ...options,
            headers: {
              ...headers,
              ...(options.headers as Record<string, string> | undefined),
            },
            credentials: 'include',
          })
        } catch {
          authStore.logout()
          await navigateTo('/auth/login')
          throw err
        }
      }
      throw err
    }
  }

  return {
    get: <T>(path: string, query?: Record<string, unknown>) =>
      request<T>(path, { method: 'GET', query }),
    post: <T>(path: string, body?: unknown) =>
      request<T>(path, { method: 'POST', body }),
    put: <T>(path: string, body?: unknown) =>
      request<T>(path, { method: 'PUT', body }),
    patch: <T>(path: string, body?: unknown) =>
      request<T>(path, { method: 'PATCH', body }),
    delete: <T>(path: string, query?: Record<string, unknown>) =>
      request<T>(path, { method: 'DELETE', query }),
  }
}
