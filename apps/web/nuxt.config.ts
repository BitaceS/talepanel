// https://nuxt.com/docs/api/configuration/nuxt-config
export default defineNuxtConfig({
  ssr: false,

  modules: [
    '@pinia/nuxt',
    '@nuxtjs/tailwindcss',
    '@vueuse/nuxt',
  ],

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
        { rel: 'preconnect', href: 'https://fonts.googleapis.com' },
        { rel: 'preconnect', href: 'https://fonts.gstatic.com', crossorigin: '' },
        {
          rel: 'stylesheet',
          href: 'https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&family=Manrope:wght@400;500;600;700;800&family=JetBrains+Mono:wght@400;500;600&display=swap',
        },
        {
          rel: 'stylesheet',
          href: 'https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@24,400,0,0',
        },
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

  // Apply auth middleware globally
  routeRules: {
    '/auth/**': {},   // public - auth middleware handles redirect if already logged in
    '/**': {},        // auth middleware guards these
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
