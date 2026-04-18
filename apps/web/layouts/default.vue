<script setup lang="ts">
import { useAuthStore } from '~/stores/auth'

const authStore = useAuthStore()
const route = useRoute()
const router = useRouter()

const pageTitle = computed(() => {
  return (route.meta.title as string | undefined) ?? route.name?.toString() ?? 'TalePanel'
})

// ── Dropdowns ─────────────────────────────────────────────────────────────────
const showNotifications = ref(false)
const showHelp = ref(false)
const showUserMenu = ref(false)

function closeAll() {
  showNotifications.value = false
  showHelp.value = false
  showUserMenu.value = false
}

function toggleNotifications() {
  const was = showNotifications.value
  closeAll()
  showNotifications.value = !was
}

function toggleHelp() {
  const was = showHelp.value
  closeAll()
  showHelp.value = !was
}

function toggleUserMenu() {
  const was = showUserMenu.value
  closeAll()
  showUserMenu.value = !was
}

// Close dropdowns on click outside
function onClickOutside(e: MouseEvent) {
  const target = e.target as HTMLElement
  if (!target.closest('[data-dropdown]')) {
    closeAll()
  }
}

onMounted(() => {
  document.addEventListener('click', onClickOutside)
})

onUnmounted(() => {
  document.removeEventListener('click', onClickOutside)
})

// Close on route change
watch(() => route.path, () => closeAll())

// ── Search ────────────────────────────────────────────────────────────────────
const searchQuery = ref('')
function onSearch() {
  const q = searchQuery.value.trim()
  if (!q) return
  router.push(`/servers?q=${encodeURIComponent(q)}`)
  searchQuery.value = ''
}

// ── Mock notifications ────────────────────────────────────────────────────────
const notifications = ref([
  { id: '1', title: 'Server started', desc: 'Hytale-Prod-01 is now running', time: '2m ago', read: false },
  { id: '2', title: 'Node heartbeat', desc: 'dev-node reconnected', time: '5m ago', read: false },
  { id: '3', title: 'Backup completed', desc: 'world_alpha backup finished', time: '12m ago', read: true },
  { id: '4', title: 'Player joined', desc: 'ShadowMiner_42 connected', time: '18m ago', read: true },
])

const unreadCount = computed(() => notifications.value.filter(n => !n.read).length)

function markAllRead() {
  notifications.value.forEach(n => n.read = true)
}

// ── Help links ────────────────────────────────────────────────────────────────
const helpLinks = [
  { icon: 'menu_book', label: 'Documentation', desc: 'Guides and API reference', href: '#' },
  { icon: 'school', label: 'Getting Started', desc: 'Quick setup tutorial', href: '#' },
  { icon: 'forum', label: 'Community', desc: 'Discord and forums', href: '#' },
  { icon: 'bug_report', label: 'Report a Bug', desc: 'Submit an issue report', href: '#' },
  { icon: 'support_agent', label: 'Contact Support', desc: 'Get help from our team', href: '#' },
]
</script>

<template>
  <div class="flex h-screen bg-tp-bg overflow-hidden">
    <!-- Sidebar -->
    <LayoutSidebar />

    <!-- Main content area -->
    <div class="flex-1 flex flex-col min-w-0 overflow-hidden">
      <!-- Top bar (glass) -->
      <header class="sticky top-0 z-30 h-14 glass-panel flex items-center justify-between px-6 shrink-0">
        <div>
          <h1 class="text-tp-text font-display font-bold text-lg capitalize">
            {{ pageTitle }}
          </h1>
        </div>

        <div class="flex items-center gap-3">
          <!-- Search -->
          <form class="hidden md:flex items-center gap-2 bg-tp-surface2 rounded-xl px-3 py-1.5" @submit.prevent="onSearch">
            <span class="material-symbols-outlined text-tp-outline text-lg">search</span>
            <input
              v-model="searchQuery"
              type="text"
              placeholder="Search infrastructure..."
              class="bg-transparent text-tp-text text-sm placeholder:text-tp-outline focus:outline-none w-44"
            />
          </form>

          <!-- Notifications -->
          <div class="relative" data-dropdown>
            <button
              class="relative w-8 h-8 flex items-center justify-center rounded-lg text-tp-muted hover:text-tp-text hover:bg-tp-surface2 transition-colors"
              title="Notifications"
              @click.stop="toggleNotifications"
            >
              <span class="material-symbols-outlined text-xl">notifications</span>
              <span
                v-if="unreadCount > 0"
                class="absolute top-0.5 right-0.5 w-4 h-4 bg-tp-primary rounded-full text-tp-on-primary text-[9px] font-bold flex items-center justify-center"
              >{{ unreadCount }}</span>
            </button>

            <!-- Notification panel -->
            <Transition name="dropdown">
              <div
                v-if="showNotifications"
                class="absolute right-0 top-full mt-2 w-80 bg-tp-surface2 rounded-xl shadow-ambient overflow-hidden z-50"
              >
                <div class="flex items-center justify-between px-4 py-3">
                  <h3 class="text-tp-text font-display font-semibold text-sm">Notifications</h3>
                  <button
                    v-if="unreadCount > 0"
                    class="text-tp-accent text-xs hover:text-tp-primary transition-colors"
                    @click="markAllRead"
                  >Mark all read</button>
                </div>

                <div class="max-h-72 overflow-y-auto scrollbar-thin">
                  <div
                    v-for="notif in notifications"
                    :key="notif.id"
                    :class="[
                      'flex items-start gap-3 px-4 py-3 hover:bg-tp-surface3/50 transition-colors cursor-pointer',
                      !notif.read ? 'bg-tp-primary/5' : '',
                    ]"
                  >
                    <div :class="['w-2 h-2 rounded-full shrink-0 mt-1.5', notif.read ? 'bg-tp-surface-highest' : 'bg-tp-primary']" />
                    <div class="flex-1 min-w-0">
                      <p class="text-tp-text text-sm font-medium truncate">{{ notif.title }}</p>
                      <p class="text-tp-muted text-xs truncate">{{ notif.desc }}</p>
                    </div>
                    <span class="text-tp-outline text-[10px] shrink-0 mt-0.5">{{ notif.time }}</span>
                  </div>
                </div>

                <div class="px-4 py-3 border-t border-tp-border/20">
                  <NuxtLink
                    to="/alerts"
                    class="text-tp-accent text-xs font-medium hover:text-tp-primary transition-colors flex items-center justify-center gap-1"
                    @click="closeAll"
                  >
                    View all notifications
                    <span class="material-symbols-outlined text-sm">arrow_forward</span>
                  </NuxtLink>
                </div>
              </div>
            </Transition>
          </div>

          <!-- Help -->
          <div class="relative" data-dropdown>
            <button
              class="w-8 h-8 flex items-center justify-center rounded-lg text-tp-muted hover:text-tp-text hover:bg-tp-surface2 transition-colors"
              title="Help"
              @click.stop="toggleHelp"
            >
              <span class="material-symbols-outlined text-xl">help_outline</span>
            </button>

            <!-- Help panel -->
            <Transition name="dropdown">
              <div
                v-if="showHelp"
                class="absolute right-0 top-full mt-2 w-72 bg-tp-surface2 rounded-xl shadow-ambient overflow-hidden z-50"
              >
                <div class="px-4 py-3">
                  <h3 class="text-tp-text font-display font-semibold text-sm">Help & Resources</h3>
                </div>

                <div>
                  <a
                    v-for="link in helpLinks"
                    :key="link.label"
                    :href="link.href"
                    class="flex items-center gap-3 px-4 py-2.5 hover:bg-tp-surface3/50 transition-colors"
                  >
                    <div class="w-8 h-8 bg-tp-surface3 rounded-lg flex items-center justify-center shrink-0">
                      <span class="material-symbols-outlined text-tp-accent text-base">{{ link.icon }}</span>
                    </div>
                    <div class="min-w-0">
                      <p class="text-tp-text text-sm font-medium">{{ link.label }}</p>
                      <p class="text-tp-outline text-[10px]">{{ link.desc }}</p>
                    </div>
                  </a>
                </div>

                <div class="px-4 py-3 border-t border-tp-border/20">
                  <div class="flex items-center gap-1.5 text-tp-outline text-[10px]">
                    <span class="material-symbols-outlined text-xs">info</span>
                    TalePanel v1.0.0
                  </div>
                </div>
              </div>
            </Transition>
          </div>

          <!-- User menu -->
          <div class="relative" data-dropdown>
            <button
              class="flex items-center gap-2.5 bg-tp-surface2 rounded-xl px-3 py-1.5 hover:bg-tp-surface3 transition-colors"
              @click.stop="toggleUserMenu"
            >
              <div class="w-7 h-7 rounded-full bg-tp-surface-highest flex items-center justify-center text-tp-accent text-xs font-semibold uppercase">
                {{ authStore.user?.username?.charAt(0) ?? 'U' }}
              </div>
              <div class="hidden sm:block text-left">
                <p class="text-tp-text text-xs font-semibold leading-none">
                  {{ authStore.user?.username ?? 'User' }}
                </p>
                <p class="text-tp-muted text-[10px] capitalize mt-0.5">
                  {{ authStore.user?.role ?? 'user' }}
                </p>
              </div>
              <span class="material-symbols-outlined text-tp-outline text-base hidden sm:block">expand_more</span>
            </button>

            <!-- User dropdown -->
            <Transition name="dropdown">
              <div
                v-if="showUserMenu"
                class="absolute right-0 top-full mt-2 w-56 bg-tp-surface2 rounded-xl shadow-ambient overflow-hidden z-50"
              >
                <!-- User info -->
                <div class="px-4 py-3 flex items-center gap-3">
                  <div class="w-9 h-9 rounded-full bg-tp-surface-highest flex items-center justify-center text-tp-accent text-sm font-semibold uppercase">
                    {{ authStore.user?.username?.charAt(0) ?? 'U' }}
                  </div>
                  <div class="min-w-0">
                    <p class="text-tp-text text-sm font-semibold truncate">{{ authStore.user?.username ?? 'User' }}</p>
                    <p class="text-tp-outline text-[10px] truncate">{{ authStore.user?.email ?? '' }}</p>
                  </div>
                </div>

                <div class="border-t border-tp-border/20">
                  <NuxtLink
                    to="/settings"
                    class="flex items-center gap-3 px-4 py-2.5 text-tp-muted hover:text-tp-text hover:bg-tp-surface3/50 transition-colors text-sm"
                    @click="closeAll"
                  >
                    <span class="material-symbols-outlined text-lg">person</span>
                    Profile
                  </NuxtLink>
                  <NuxtLink
                    to="/settings"
                    class="flex items-center gap-3 px-4 py-2.5 text-tp-muted hover:text-tp-text hover:bg-tp-surface3/50 transition-colors text-sm"
                    @click="closeAll"
                  >
                    <span class="material-symbols-outlined text-lg">settings</span>
                    Settings
                  </NuxtLink>
                  <NuxtLink
                    v-if="authStore.user?.role === 'admin' || authStore.user?.role === 'owner'"
                    to="/admin"
                    class="flex items-center gap-3 px-4 py-2.5 text-tp-muted hover:text-tp-text hover:bg-tp-surface3/50 transition-colors text-sm"
                    @click="closeAll"
                  >
                    <span class="material-symbols-outlined text-lg">admin_panel_settings</span>
                    Admin Panel
                  </NuxtLink>
                </div>

                <div class="border-t border-tp-border/20">
                  <button
                    class="flex items-center gap-3 px-4 py-2.5 text-tp-muted hover:text-tp-error hover:bg-tp-error/10 transition-colors text-sm w-full"
                    @click="authStore.logout(); closeAll()"
                  >
                    <span class="material-symbols-outlined text-lg">logout</span>
                    Log out
                  </button>
                </div>
              </div>
            </Transition>
          </div>
        </div>
      </header>

      <!-- Page content -->
      <main class="flex-1 overflow-y-auto scrollbar-thin" @click="closeAll">
        <UpdateBanner />
        <NuxtPage />
      </main>
    </div>
  </div>
</template>

<style scoped>
.dropdown-enter-active,
.dropdown-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}
.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
