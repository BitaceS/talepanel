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
      'flex flex-col bg-tp-surface glass-panel border-r transition-all duration-300 shrink-0 h-screen sticky top-0',
      collapsed ? 'w-16' : 'w-64',
    ]"
  >
    <!-- Logo section -->
    <div class="flex items-center h-14 px-4 shrink-0">
      <NuxtLink to="/" class="flex items-center gap-3 min-w-0">
        <img src="/logo.svg" alt="" class="w-8 h-8 shrink-0" />
        <Transition name="fade-width">
          <div v-if="!collapsed" class="flex flex-col min-w-0">
            <span class="text-tp-text font-display font-bold text-lg leading-none">TalePanel</span>
            <span class="text-tp-outline font-mono text-[10px] leading-none mt-1 uppercase tracking-wider">Hytale servers</span>
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
          <span class="text-tp-outline font-mono text-[10px] font-medium uppercase tracking-[0.18em]">
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
            'nav-item relative flex items-center gap-3 rounded-lg px-3 py-2.5 text-[15px] font-medium transition-all duration-150',
            isActive(item.to)
              ? 'is-active bg-tp-surface2 text-tp-text'
              : 'text-tp-muted hover:text-tp-text hover:bg-tp-surface2/60',
          ]"
          :title="collapsed ? item.label : undefined"
        >
          <span :class="['material-symbols-outlined text-xl shrink-0', isActive(item.to) ? 'text-tp-primary' : '']">{{ item.icon }}</span>
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
          'flex items-center gap-2.5 rounded-xl p-2.5 bg-tp-surface2 border border-tp-border/60',
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
/* Active item: gold edge on the left plus the diamond from talepanel.com's menu */
.nav-item.is-active::before {
  content: '';
  position: absolute;
  left: -0.5rem;
  top: 0.5rem;
  bottom: 0.5rem;
  width: 2px;
  background: #f2c14e;
}
.nav-item.is-active::after {
  content: '';
  position: absolute;
  right: 0.75rem;
  top: 50%;
  width: 6px;
  height: 6px;
  margin-top: -3px;
  background: #f2c14e;
  transform: rotate(45deg);
}

.fade-width-enter-active,
.fade-width-leave-active {
  transition: opacity 0.2s ease;
}
.fade-width-enter-from,
.fade-width-leave-to {
  opacity: 0;
}
</style>
