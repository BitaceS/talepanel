// vue-i18n options. Messages come from locales/ (see nuxt.config.ts).
export default defineI18nConfig(() => ({
  fallbackLocale: 'en',
  // Missing German keys fall back to English quietly.
  missingWarn: false,
  fallbackWarn: false,
}))
