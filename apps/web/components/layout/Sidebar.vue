<script setup lang="ts">
import { useAuthStore } from '~/stores/auth'
import { useModulesStore } from '~/stores/modules'
import { useMediaQuery } from '@vueuse/core'

const authStore = useAuthStore()
const { t } = useI18n()
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

// Below md the sidebar is an off-canvas drawer: always full width when open,
// closed after navigating, on Escape and on a tap outside.
const isDesktop = useMediaQuery('(min-width: 768px)')
const mini = computed(() => isDesktop.value && collapsed.value)
const { open: drawerOpen, close: closeDrawer } = useMobileNav()
watch(() => route.fullPath, closeDrawer)
watch(isDesktop, (d) => { if (d) closeDrawer() })

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && drawerOpen.value) closeDrawer()
}
onMounted(() => document.addEventListener('keydown', onKey))
onUnmounted(() => document.removeEventListener('keydown', onKey))

// Move focus into the drawer when it opens so keyboard users land in it.
const asideEl = ref<HTMLElement | null>(null)
watch(drawerOpen, async (o) => {
  if (!o) return
  await nextTick()
  asideEl.value?.querySelector<HTMLElement>('nav a')?.focus()
})

// label and title are message keys (locales/*/common.json).
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
    title: 'nav.sectionOverview',
    items: [
      { label: 'nav.dashboard', icon: 'dashboard', to: '/' },
    ],
  },
  {
    title: 'nav.sectionInfrastructure',
    items: [
      { label: 'nav.servers', icon: 'dns', to: '/servers' },
      { label: 'nav.nodes', icon: 'lan', to: '/nodes', moduleId: 'nodes' },
    ],
  },
  {
    title: 'nav.sectionOperations',
    items: [
      { label: 'nav.backups', icon: 'backup', to: '/backups', moduleId: 'backups' },
      { label: 'nav.alerts', icon: 'notifications_active', to: '/alerts', moduleId: 'alerts' },
      { label: 'nav.monitoring', icon: 'monitoring', to: '/monitoring', moduleId: 'monitoring' },
    ],
  },
  {
    title: 'nav.sectionSystem',
    items: [
      { label: 'nav.settings', icon: 'settings', to: '/settings' },
    ],
  },
]

const isAdmin = computed(() =>
  authStore.user?.role === 'admin' || authStore.user?.role === 'owner'
)

const adminSection: NavSection = {
  title: 'nav.sectionAdministration',
  items: [
    { label: 'nav.admin', icon: 'admin_panel_settings', to: '/admin' },
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
  <Transition name="backdrop">
    <div
      v-if="drawerOpen && !isDesktop"
      class="fixed inset-0 z-40 bg-black/60"
      aria-hidden="true"
      @click="closeDrawer"
    />
  </Transition>
  <aside
    id="app-sidebar"
    ref="asideEl"
    :class="[
      'flex flex-col bg-tp-surface glass-panel border-r transition-all duration-300 shrink-0 h-screen',
      'fixed inset-y-0 left-0 z-50 w-64 md:sticky md:top-0 md:z-auto md:translate-x-0',
      drawerOpen ? 'translate-x-0 shadow-2xl' : '-translate-x-full',
      mini ? 'md:w-16' : 'md:w-64',
    ]"
    :aria-label="t('nav.mainNavigation')"
    :inert="!isDesktop && !drawerOpen ? true : undefined"
  >
    <!-- Logo section -->
    <div class="flex items-center justify-between h-14 px-4 shrink-0">
      <NuxtLink to="/" class="flex items-center gap-3 min-w-0">
        <img src="/logo.svg" alt="" class="w-8 h-8 shrink-0" />
        <Transition name="fade-width">
          <div v-if="!mini" class="flex flex-col min-w-0">
            <span class="text-tp-text font-display font-bold text-lg leading-none">TalePanel</span>
            <span class="text-tp-outline font-mono text-[10px] leading-none mt-1 uppercase tracking-wider">{{ t('nav.tagline') }}</span>
          </div>
        </Transition>
      </NuxtLink>
      <button
        v-if="!isDesktop"
        type="button"
        class="w-9 h-9 -mr-2 flex items-center justify-center rounded-lg text-tp-muted hover:text-tp-text hover:bg-tp-surface2"
        :aria-label="t('nav.closeMenu')"
        @click="closeDrawer"
      >
        <span class="material-symbols-outlined text-xl">close</span>
      </button>
    </div>

    <!-- Navigation -->
    <nav class="flex-1 overflow-y-auto scrollbar-thin py-4 px-2 space-y-1">
      <template v-for="section in allSections" :key="section.title">
        <!-- Section label -->
        <div
          v-if="!mini"
          class="px-3 pt-5 pb-2 first:pt-0"
        >
          <span class="text-tp-outline font-mono text-[10px] font-medium uppercase tracking-[0.18em]">
            {{ t(section.title) }}
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
          :title="mini ? t(item.label) : undefined"
        >
          <span :class="['material-symbols-outlined text-xl shrink-0', isActive(item.to) ? 'text-tp-primary' : '']">{{ item.icon }}</span>
          <span v-if="!mini" class="truncate">{{ t(item.label) }}</span>
        </NuxtLink>
      </template>
    </nav>

    <!-- Bottom section -->
    <div class="p-3 shrink-0">
      <!-- Collapse toggle -->
      <button
        class="hidden md:flex w-full items-center justify-center mb-3 rounded-lg h-8 text-tp-muted hover:text-tp-text hover:bg-tp-surface2 transition-colors"
        @click="toggleCollapse"
        :title="mini ? t('nav.expandSidebar') : t('nav.collapseSidebar')"
      >
        <span
          :class="['material-symbols-outlined text-lg transition-transform duration-300', mini ? 'rotate-180' : '']"
        >chevron_left</span>
      </button>

      <!-- User info -->
      <div
        :class="[
          'flex items-center gap-2.5 rounded-xl p-2.5 bg-tp-surface2 border border-tp-border/60',
          mini ? 'justify-center' : '',
        ]"
      >
        <div class="w-8 h-8 rounded-full bg-tp-surface-highest flex items-center justify-center text-tp-accent text-sm font-semibold uppercase shrink-0">
          {{ authStore.user?.username?.charAt(0) ?? 'U' }}
        </div>
        <div v-if="!mini" class="flex-1 min-w-0">
          <p class="text-tp-text text-sm font-medium truncate">
            {{ authStore.user?.username ?? 'User' }}
          </p>
          <p class="text-tp-outline text-[10px] capitalize truncate">
            {{ t(`roles.${authStore.user?.role ?? 'user'}`) }}
          </p>
        </div>
      </div>

      <!-- Logout -->
      <button
        :class="[
          'w-full flex items-center gap-2 mt-2 rounded-lg px-3 py-2 text-sm text-tp-muted hover:text-tp-error hover:bg-tp-error/10 transition-colors',
          mini ? 'justify-center' : '',
        ]"
        :title="t('nav.logout')"
        @click="authStore.logout()"
      >
        <span class="material-symbols-outlined text-lg shrink-0">logout</span>
        <span v-if="!mini">{{ t('nav.logout') }}</span>
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

.backdrop-enter-active,
.backdrop-leave-active {
  transition: opacity 0.2s ease;
}
.backdrop-enter-from,
.backdrop-leave-to {
  opacity: 0;
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
