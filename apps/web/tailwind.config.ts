import type { Config } from 'tailwindcss'
import defaultTheme from 'tailwindcss/defaultTheme'

export default {
  darkMode: 'class',
  content: [
    './components/**/*.{vue,ts}',
    './layouts/**/*.vue',
    './pages/**/*.vue',
    './plugins/**/*.{ts,js}',
    './composables/**/*.ts',
    './stores/**/*.ts',
    './app.vue',
    './error.vue',
  ],
  theme: {
    extend: {
      // "Tale" palette, shared with talepanel.com: night stone surfaces,
      // gold for the one primary action, ember as accent, cream text.
      colors: {
        // Surface hierarchy (deep → shallow)
        'tp-bg':              '#0e0c14',
        'tp-surface-lowest':  '#0a0810',
        'tp-surface':         '#15111c',
        'tp-surface2':        '#1f1925',
        'tp-surface3':        '#2a2230',
        'tp-surface-highest': '#3d3243',

        // Borders & outlines
        'tp-border':    '#3d3243',
        'tp-outline':   '#8f8274',

        // Brand
        'tp-primary':   '#f2c14e',
        'tp-accent':    '#eda05a',
        'tp-tertiary':  '#dccaa0',

        // Semantic status
        'tp-success': '#86c160',
        'tp-warning': '#e8863b',
        'tp-danger':  '#d0533f',
        'tp-error':   '#f0a090',

        // Text
        'tp-text':  '#efe3c8',
        'tp-muted': '#b6a998',

        // On-primary (text on primary bg)
        'tp-on-primary': '#2b1e12',
      },
      fontFamily: {
        sans:    ['Alegreya Sans', ...defaultTheme.fontFamily.sans],
        display: ['Alegreya SC', 'Georgia', ...defaultTheme.fontFamily.serif],
        mono:    ['JetBrains Mono', ...defaultTheme.fontFamily.mono],
      },
      // Squarer corners: carved blocks, not pills.
      borderRadius: {
        'md':  '0.1875rem',
        'lg':  '0.25rem',
        'xl':  '0.3125rem',
        '2xl': '0.375rem',
        '3xl': '0.5rem',
      },
      animation: {
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'fadeIn': 'fadeIn 0.2s ease-in-out',
        'spin-slow': 'spin 1.5s linear infinite',
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0', transform: 'translateY(-4px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
      },
      boxShadow: {
        'glow':    '0 0 0 1px rgba(242, 193, 78, 0.35), 0 0 18px rgba(242, 193, 78, 0.12)',
        'glow-lg': '0 0 0 1px rgba(242, 193, 78, 0.45), 0 0 32px rgba(242, 193, 78, 0.16)',
        'ambient': '0 20px 40px rgba(0, 0, 0, 0.4)',
      },
    },
  },
  plugins: [],
} satisfies Config
