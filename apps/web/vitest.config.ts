import { defineConfig } from 'vitest/config'

// Unit tests for plain TypeScript helpers (utils/). They do not need Nuxt.
export default defineConfig({
  test: {
    include: ['tests/unit/**/*.test.ts'],
    environment: 'node',
  },
})
