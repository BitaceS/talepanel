// https://nuxt.com/docs/api/configuration/nuxt-config
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
          "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
          "font-src 'self' data: https://fonts.gstatic.com",
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
