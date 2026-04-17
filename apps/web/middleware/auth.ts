import { useAuthStore } from '~/stores/auth'

export default defineNuxtRouteMiddleware((to) => {
  const authStore = useAuthStore()
  const publicRoutes = ['/auth/login', '/auth/register']

  if (!authStore.isAuthenticated && !publicRoutes.includes(to.path)) {
    return navigateTo('/auth/login')
  }

  if (authStore.isAuthenticated && publicRoutes.includes(to.path)) {
    return navigateTo('/')
  }
})
