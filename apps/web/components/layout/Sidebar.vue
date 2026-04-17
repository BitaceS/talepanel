<script setup lang="ts">
import { useAuthStore } from '~/stores/auth'
import { useModulesStore } from '~/stores/modules'

const authStore = useAuthStore()
const modulesStore = useModulesStore()
const route = useRoute()

// Collapse state persisted to localStorage
const collapsed = ref(false)

onMounted(() => {
  const stored = localStorage.getItem('tp_sidebar_collapsed')
  if (stored !== null) {
    collapsed.value = stored === 'true'
  }
})

function toggleCollapse() {
  collapsed.value = !collapsed.value
  localStorage.setItem('tp_sidebar_collapsed', String(collapsed.value))
}

interface NavItem {
  label: string
  icon: string
  to: string
  moduleId?: string
}

interface NavSection {
  title: string
  items: NavItem[]
}

const navSections: NavSection[] = [
  {
    title: 'Overview',
    items: [
      { label: 'Dashboard', icon: 'dashboard', to: '/' },
    ],
  },
  {
    title: 'Infrastructure',
    items: [
      { label: 'Servers', icon: 'dns', to: '/servers' },
      { label: 'Nodes', icon: 'lan', to: '/nodes', moduleId: 'nodes' },
    ],
  },
  {
    title: 'Operations',
    items: [
      { label: 'Backups', icon: 'backup', to: '/backups', moduleId: 'backups' },
      { label: 'Alerts', icon: 'notifications_active', to: '/alerts', moduleId: 'alerts' },
      { label: 'Monitoring', icon: 'monitoring', to: '/monitoring', moduleId: 'monitoring' },
    ],
  },
  {
    title: 'System',
    items: [
      { label: 'Settings', icon: 'settings', to: '/settings' },
    ],
  },
]

const isAdmin = computed(() =>
  authStore.user?.role === 'admin' || authStore.user?.role === 'owner'
)

const adminSection: NavSection = {
  title: 'Administration',
  items: [
    { label: 'Admin Panel', icon: 'admin_panel_settings', to: '/admin' },
  ],
}

const allSections = computed(() => {
  const base = isAdmin.value ? [...navSections, adminSection] : navSections
  return base.map(section => ({
    ...section,
    items: section.items.filter(item =>
      !item.moduleId || modulesStore.isEnabled(item.moduleId)
    ),
  })).filter(section => section.items.length > 0)
})

function isActive(to: string): boolean {
  if (to === '/') return route.path === '/'
  return route.path.startsWith(to)
}
</script>

<template>
  <aside
    :class="[
      'flex flex-col bg-tp-surface glass-panel transition-all duration-300 shrink-0 h-screen sticky top-0',
      collapsed ? 'w-16' : 'w-64',
    ]"
  >
    <!-- Logo section -->
    <div class="flex items-center h-14 px-4 shrink-0">
      <NuxtLink to="/" class="flex items-center gap-3 min-w-0">
        <div class="w-8 h-8 bg-tp-primary rounded-lg flex items-center justify-center shrink-0">
          <span class="material-symbols-outlined text-tp-on-primary text-lg">cloud_queue</span>
        </div>
        <Transition name="fade-width">
          <div v-if="!collapsed" class="flex flex-col min-w-0">
            <span class="text-tp-text font-display font-bold text-sm leading-none">TalePanel</span>
            <span class="text-tp-outline text-[10px] leading-none mt-1 uppercase tracking-wider">Ethereal Command</span>
          </div>
        </Transition>
      </NuxtLink>
    </div>

    <!-- Navigation -->
    <nav class="flex-1 overflow-y-auto scrollbar-thin py-4 px-2 space-y-1">
      <template v-for="section in allSections" :key="section.title">
        <!-- Section label -->
        <div
          v-if="!collapsed"
          class="px-3 pt-5 pb-2 first:pt-0"
        >
          <span class="text-tp-outline text-[10px] font-semibold uppercase tracking-widest">
            {{ section.title }}
          </span>
        </div>
        <div v-else class="py-2">
          <div class="w-6 h-px bg-tp-border/30 mx-auto" />
        </div>

        <!-- Nav items -->
        <NuxtLink
          v-for="item in section.items"
          :key="item.to"
          :to="item.to"
          :class="[
            'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-all duration-150',
            isActive(item.to)
              ? 'bg-tp-primary text-tp-on-primary'
              : 'text-tp-muted hover:text-tp-text hover:bg-tp-surface2',
          ]"
          :title="collapsed ? item.label : undefined"
        >
          <span class="material-symbols-outlined text-xl shrink-0">{{ item.icon }}</span>
          <span v-if="!collapsed" class="truncate">{{ item.label }}</span>
        </NuxtLink>
      </template>
    </nav>

    <!-- Bottom section -->
    <div class="p-3 shrink-0">
      <!-- Collapse toggle -->
      <button
        class="w-full flex items-center justify-center mb-3 rounded-lg h-8 text-tp-muted hover:text-tp-text hover:bg-tp-surface2 transition-colors"
        @click="toggleCollapse"
        :title="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
      >
        <span
          :class="['material-symbols-outlined text-lg transition-transform duration-300', collapsed ? 'rotate-180' : '']"
        >chevron_left</span>
      </button>

      <!-- User info -->
      <div
        :class="[
          'flex items-center gap-2.5 rounded-xl p-2.5 bg-tp-surface2',
          collapsed ? 'justify-center' : '',
        ]"
      >
        <div class="w-8 h-8 rounded-full bg-tp-surface-highest flex items-center justify-center text-tp-accent text-sm font-semibold uppercase shrink-0">
          {{ authStore.user?.username?.charAt(0) ?? 'U' }}
        </div>
        <div v-if="!collapsed" class="flex-1 min-w-0">
          <p class="text-tp-text text-sm font-medium truncate">
            {{ authStore.user?.username ?? 'User' }}
          </p>
          <p class="text-tp-outline text-[10px] capitalize truncate">
            {{ authStore.user?.role ?? 'user' }}
          </p>
        </div>
      </div>

      <!-- Logout -->
      <button
        :class="[
          'w-full flex items-center gap-2 mt-2 rounded-lg px-3 py-2 text-sm text-tp-muted hover:text-tp-error hover:bg-tp-error/10 transition-colors',
          collapsed ? 'justify-center' : '',
        ]"
        title="Logout"
        @click="authStore.logout()"
      >
        <span class="material-symbols-outlined text-lg shrink-0">logout</span>
        <span v-if="!collapsed">Logout</span>
      </button>
    </div>
  </aside>
</template>

<style scoped>
.fade-width-enter-active,
.fade-width-leave-active {
  transition: opacity 0.2s ease;
}
.fade-width-enter-from,
.fade-width-leave-to {
  opacity: 0;
}
</style>
