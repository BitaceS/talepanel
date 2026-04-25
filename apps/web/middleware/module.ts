import { useModulesStore } from '~/stores/modules'

export default defineNuxtRouteMiddleware((to) => {
  const moduleId = to.meta.moduleId as string | undefined
  if (!moduleId) return

  const modulesStore = useModulesStore()
  if (!modulesStore.isEnabled(moduleId)) {
    return navigateTo('/')
  }
})
