// @ts-check
// Flat config generated from the Nuxt project (`@nuxt/eslint`, run
// `nuxt prepare` once so .nuxt/eslint.config.mjs exists).
import withNuxt from './.nuxt/eslint.config.mjs'

export default withNuxt({
  rules: {
    // The codebase predates linting; these stay warnings until cleaned up so
    // `npm run lint` can gate CI on real errors today.
    '@typescript-eslint/no-explicit-any': 'warn',
    '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
    'vue/no-v-html': 'warn',
    // Pure formatting rules: off until the templates are reformatted in one
    // go (doing it now would conflict with every open branch).
    'vue/first-attribute-linebreak': 'off',
    'vue/html-self-closing': 'off',
    'vue/attributes-order': 'off',
  },
})
