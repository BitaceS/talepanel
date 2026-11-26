<script setup lang="ts">
import { useAuthStore } from '~/stores/auth'

const authStore = useAuthStore()
const route = useRoute()
const { t, te, locale, locales, setLocale } = useI18n()
const router = useRouter()

// Pages set `title` to a message key (pages.*). Route names like
// "servers-id" read badly; fall back to the first path segment.
const pageTitle = computed(() => {
  if (route.meta.title) return t(route.meta.title as string)
  const first = route.path.split('/').filter(Boolean)[0]
  if (!first) return t('pages.dashboard')
  const key = `pages.${first}`
  return te(key) ? t(key) : first.replace(/-/g, ' ')
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
  if (!was) loadAlerts()
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

// ── Notifications: open alert events ─────────────────────────────────────────
interface AlertEvent {
  id: string
  severity: string
  title: string
  body?: string
  resolved: boolean
  created_at: string
}

const api = useApi()
const alertEvents = ref<AlertEvent[]>([])
const openAlerts = computed(() => alertEvents.value.filter(e => !e.resolved))
const unreadCount = computed(() => openAlerts.value.length)

async function loadAlerts() {
  try {
    const data = await api.get<{ events: AlertEvent[] }>('/alerts/events')
    alertEvents.value = data.events ?? []
  } catch {
    // Keep the last list; the bell just shows nothing new.
  }
}

let alertTimer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  loadAlerts()
  alertTimer = setInterval(loadAlerts, 60_000)
})
onUnmounted(() => clearInterval(alertTimer))

function timeAgo(iso: string): string {
  const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000))
  if (s < 60) return t('time.justNow')
  if (s < 3600) return t('time.minutesAgo', { n: Math.floor(s / 60) })
  if (s < 86400) return t('time.hoursAgo', { n: Math.floor(s / 3600) })
  return t('time.daysAgo', { n: Math.floor(s / 86400) })
}

function severityDot(sev: string): string {
  if (sev === 'critical' || sev === 'error') return 'bg-tp-danger'
  if (sev === 'warning') return 'bg-tp-warning'
  return 'bg-tp-primary'
}

// ── Help links ────────────────────────────────────────────────────────────────
// key → layout.help.<key>.label / .desc
const helpLinks = [
  { icon: 'menu_book', key: 'docs', href: 'https://talepanel.com/docs/' },
  { icon: 'school', key: 'start', href: 'https://talepanel.com/docs/start/install/' },
  { icon: 'forum', key: 'community', href: 'https://discord.gg/6N8WNrW5WJ' },
  { icon: 'bug_report', key: 'bug', href: 'https://github.com/BitaceS/talepanel/issues' },
]

const { info: versionInfo } = useUpdateCheck()

const { theme, setTheme } = useTheme()

const mobileNav = useMobileNav()
</script>

<template>
  <div class="relative flex h-screen bg-tp-bg overflow-hidden">
    <GamePixelScene v-if="theme === 'game'" />

    <!-- Sidebar -->
    <LayoutSidebar />

    <!-- Main content area -->
    <div class="relative z-10 flex-1 flex flex-col min-w-0 overflow-hidden">
      <!-- Top bar (glass) -->
      <header class="sticky top-0 z-30 h-14 glass-panel border-b flex items-center justify-between gap-2 px-3 md:px-6 shrink-0">
        <div class="flex items-center gap-1 min-w-0">
          <button
            type="button"
            class="md:hidden w-10 h-10 -ml-1 flex items-center justify-center rounded-lg text-tp-muted hover:text-tp-text hover:bg-tp-surface2"
            :aria-label="t('nav.openMenu')"
            aria-controls="app-sidebar"
            :aria-expanded="mobileNav.open.value"
            @click.stop="mobileNav.toggle()"
          >
            <span class="material-symbols-outlined text-2xl">menu</span>
          </button>
          <h1 class="text-tp-text font-display font-bold text-lg md:text-xl capitalize truncate">
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
              :placeholder="t('layout.search')"
              class="bg-transparent text-tp-text text-sm placeholder:text-tp-outline focus:outline-none w-44"
            />
          </form>

          <!-- Notifications -->
          <div class="relative" data-dropdown>
            <button
              class="relative w-8 h-8 flex items-center justify-center rounded-lg text-tp-muted hover:text-tp-text hover:bg-tp-surface2 transition-colors"
              :title="t('layout.notifications')"
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
                  <h3 class="text-tp-text font-display font-semibold text-sm">{{ t('layout.openAlerts') }}</h3>
                  <span class="text-tp-outline text-xs">{{ unreadCount }}</span>
                </div>

                <div class="max-h-72 overflow-y-auto scrollbar-thin">
                  <p v-if="openAlerts.length === 0" class="px-4 py-6 text-center text-tp-muted text-sm">
                    {{ t('layout.noOpenAlerts') }}
                  </p>
                  <NuxtLink
                    v-for="ev in openAlerts.slice(0, 8)"
                    :key="ev.id"
                    to="/alerts"
                    class="flex items-start gap-3 px-4 py-3 hover:bg-tp-surface3/50 transition-colors"
                    @click="closeAll"
                  >
                    <div :class="['w-2 h-2 rounded-full shrink-0 mt-1.5', severityDot(ev.severity)]" />
                    <div class="flex-1 min-w-0">
                      <p class="text-tp-text text-sm font-medium truncate">{{ ev.title }}</p>
                      <p v-if="ev.body" class="text-tp-muted text-xs truncate">{{ ev.body }}</p>
                    </div>
                    <span class="text-tp-outline text-[11px] shrink-0 mt-0.5">{{ timeAgo(ev.created_at) }}</span>
                  </NuxtLink>
                </div>

                <div class="px-4 py-3 border-t border-tp-border/20">
                  <NuxtLink
                    to="/alerts"
                    class="text-tp-accent text-xs font-medium hover:text-tp-primary transition-colors flex items-center justify-center gap-1"
                    @click="closeAll"
                  >
                    {{ t('layout.viewAllAlerts') }}
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
              :title="t('layout.help.title')"
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
                  <h3 class="text-tp-text font-display font-semibold text-sm">{{ t('layout.help.heading') }}</h3>
                </div>

                <div>
                  <a
                    v-for="link in helpLinks"
                    :key="link.key"
                    :href="link.href"
                    target="_blank"
                    rel="noopener"
                    class="flex items-center gap-3 px-4 py-2.5 hover:bg-tp-surface3/50 transition-colors"
                  >
                    <div class="w-8 h-8 bg-tp-surface3 rounded-lg flex items-center justify-center shrink-0">
                      <span class="material-symbols-outlined text-tp-accent text-base">{{ link.icon }}</span>
                    </div>
                    <div class="min-w-0">
                      <p class="text-tp-text text-sm font-medium">{{ t(`layout.help.${link.key}.label`) }}</p>
                      <p class="text-tp-outline text-[10px]">{{ t(`layout.help.${link.key}.desc`) }}</p>
                    </div>
                  </a>
                </div>

                <div class="px-4 py-3 border-t border-tp-border/20">
                  <div class="flex items-center gap-1.5 text-tp-outline text-[10px]">
                    <span class="material-symbols-outlined text-xs">info</span>
                    TalePanel {{ versionInfo?.current_version ?? '' }}
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
                  {{ authStore.user?.username ?? t('layout.user') }}
                </p>
                <p class="text-tp-muted text-[10px] capitalize mt-0.5">
                  {{ t(`roles.${authStore.user?.role ?? 'user'}`) }}
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
                    <p class="text-tp-text text-sm font-semibold truncate">{{ authStore.user?.username ?? t('layout.user') }}</p>
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
                    {{ t('layout.profile') }}
                  </NuxtLink>
                  <NuxtLink
                    to="/settings"
                    class="flex items-center gap-3 px-4 py-2.5 text-tp-muted hover:text-tp-text hover:bg-tp-surface3/50 transition-colors text-sm"
                    @click="closeAll"
                  >
                    <span class="material-symbols-outlined text-lg">settings</span>
                    {{ t('nav.settings') }}
                  </NuxtLink>
                  <NuxtLink
                    v-if="authStore.user?.role === 'admin' || authStore.user?.role === 'owner'"
                    to="/admin"
                    class="flex items-center gap-3 px-4 py-2.5 text-tp-muted hover:text-tp-text hover:bg-tp-surface3/50 transition-colors text-sm"
                    @click="closeAll"
                  >
                    <span class="material-symbols-outlined text-lg">admin_panel_settings</span>
                    {{ t('nav.admin') }}
                  </NuxtLink>
                </div>

                <div class="border-t border-tp-border/20 px-4 py-3">
                  <p class="text-tp-outline text-[11px] font-mono uppercase tracking-[0.18em] mb-2">{{ t('layout.theme') }}</p>
                  <div class="grid grid-cols-2 gap-1.5" role="radiogroup" :aria-label="t('layout.theme')">
                    <button
                      v-for="pt in PANEL_THEMES"
                      :key="pt.id"
                      role="radio"
                      :aria-checked="theme === pt.id"
                      :class="[
                        'rounded-lg px-2 py-1.5 text-xs font-semibold transition-colors',
                        theme === pt.id ? 'bg-tp-primary text-tp-on-primary' : 'bg-tp-surface3 text-tp-muted hover:text-tp-text',
                      ]"
                      @click="setTheme(pt.id)"
                    >{{ t(`layout.themes.${pt.id}`) }}</button>
                  </div>
                </div>

                <div class="border-t border-tp-border/20 px-4 py-3">
                  <p class="text-tp-outline text-[11px] font-mono uppercase tracking-[0.18em] mb-2">{{ t('layout.language') }}</p>
                  <div class="grid grid-cols-2 gap-1.5" role="radiogroup" :aria-label="t('layout.language')">
                    <button
                      v-for="l in locales"
                      :key="l.code"
                      role="radio"
                      :aria-checked="locale === l.code"
                      :lang="l.code"
                      :class="[
                        'rounded-lg px-2 py-1.5 text-xs font-semibold transition-colors',
                        locale === l.code ? 'bg-tp-primary text-tp-on-primary' : 'bg-tp-surface3 text-tp-muted hover:text-tp-text',
                      ]"
                      @click="setLocale(l.code)"
                    >{{ l.name }}</button>
                  </div>
                </div>

                <div class="border-t border-tp-border/20">
                  <button
                    class="flex items-center gap-3 px-4 py-2.5 text-tp-muted hover:text-tp-error hover:bg-tp-error/10 transition-colors text-sm w-full"
                    @click="authStore.logout(); closeAll()"
                  >
                    <span class="material-symbols-outlined text-lg">logout</span>
                    {{ t('layout.logOut') }}
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
