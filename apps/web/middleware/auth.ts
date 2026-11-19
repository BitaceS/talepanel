import { useAuthStore } from '~/stores/auth'

export default defineNuxtRouteMiddleware((to) => {
  const authStore = useAuthStore()
  const publicRoutes = ['/auth/login', '/auth/register', '/setup']

  if (!authStore.isAuthenticated && !publicRoutes.includes(to.path)) {
    return navigateTo('/auth/login')
  }

  if (authStore.isAuthenticated && publicRoutes.includes(to.path)) {
    return navigateTo('/')
  }

  // Staff 2FA policy: nothing but the security page until 2FA is set up.
  if (authStore.isAuthenticated && authStore.twoFactorSetupRequired && to.path !== '/settings/security') {
    return navigateTo('/settings/security')
  }
})
