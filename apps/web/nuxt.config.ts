// https://nuxt.com/docs/api/configuration/nuxt-config

// One message file per area and language: locales/<code>/<area>.json.
const LOCALE_AREAS = ['common', 'auth', 'dashboard', 'servers', 'server', 'nodes', 'admin', 'settings', 'backups', 'mods', 'monitoring', 'players', 'alerts', 'misc']

export default defineNuxtConfig({
  ssr: false,

  // Nuxt 4 defaults srcDir to `app/`. Keep the existing flat layout
  // (pages/, components/, stores/, ... at the project root) so `~/` imports,
  // the tailwind content globs and the docker-compose bind-mount stay valid.
  srcDir: '.',

  modules: [
    '@pinia/nuxt',
    '@nuxtjs/tailwindcss',
    '@vueuse/nuxt',
    '@nuxt/eslint',
    '@nuxtjs/i18n',
  ],

  // UI languages. Messages live in locales/<code>/<area>.json and are loaded
  // when a language is picked. No URL prefix; the choice is kept in the
  // tp_lang cookie (first visit: the browser's language, else English).
  i18n: {
    restructureDir: '.',
    langDir: 'locales',
    strategy: 'no_prefix',
    defaultLocale: 'en',
    locales: [
      { code: 'en', language: 'en', name: 'English', files: LOCALE_AREAS.map(a => `en/${a}.json`) },
      { code: 'de', language: 'de', name: 'Deutsch', files: LOCALE_AREAS.map(a => `de/${a}.json`) },
    ],
    detectBrowserLanguage: {
      useCookie: true,
      cookieKey: 'tp_lang',
      redirectOn: 'root',
      alwaysRedirect: false,
      fallbackLocale: 'en',
    },
    vueI18n: './i18n.config.ts',
  },

  runtimeConfig: {
    public: {
      apiBase: process.env.NUXT_PUBLIC_API_BASE ?? 'http://localhost:8080/api/v1',
    },
  },

  app: {
    head: {
      title: 'TalePanel',
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
        { name: 'description', content: 'TalePanel - Hytale Server Management' },
      ],
      link: [
        { rel: 'icon', type: 'image/svg+xml', href: '/logo.svg' },
      ],
      htmlAttrs: {
        class: 'dark',
        lang: 'en',
      },
    },
  },

  css: ['~/assets/css/main.css'],

  router: {
    options: {
      // Global middleware applied to all routes
    },
  },

  // Security headers on every page, whatever proxy sits in front (Caddy,
  // Coolify, ...). frame-ancestors stops clickjacking of destructive buttons.
  // script-src needs 'unsafe-inline': Nuxt injects the runtime config (whose
  // apiBase differs per deployment) and an importmap as inline scripts, so
  // they cannot be hashed at build time. The API may live on another origin,
  // hence https:/wss: in connect-src; dev adds plain http/ws for localhost.
  routeRules: {
    '/**': {
      headers: {
        'Content-Security-Policy': [
          "default-src 'self'",
          "script-src 'self' 'unsafe-inline'",
          "style-src 'self' 'unsafe-inline'",
          "font-src 'self' data:",
          "img-src 'self' data: blob: https:",
          `connect-src 'self' https: wss:${process.env.NODE_ENV === 'production' ? '' : ' http: ws:'}`,
          "frame-ancestors 'none'",
          "base-uri 'self'",
          "form-action 'self'",
          "object-src 'none'",
        ].join('; '),
        'X-Frame-Options': 'DENY',
        'X-Content-Type-Options': 'nosniff',
        'Referrer-Policy': 'strict-origin-when-cross-origin',
        'Permissions-Policy': 'camera=(), microphone=(), geolocation=()',
      },
    },
  },

  typescript: {
    strict: true,
  },

  // Enable polling so Vite detects file changes inside Docker on Windows
  // (inotify events don't propagate from Windows host to Linux container).
  vite: {
    server: {
      watch: {
        usePolling: true,
        interval: 300,
      },
    },
  },

  compatibilityDate: '2024-11-01',
})
