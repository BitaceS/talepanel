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
      colors: {
        // Surface hierarchy (deep → shallow)
        'tp-bg':              '#0b1326',
        'tp-surface-lowest':  '#060e20',
        'tp-surface':         '#131b2e',
        'tp-surface2':        '#171f33',
        'tp-surface3':        '#222a3d',
        'tp-surface-highest': '#2d3449',

        // Borders & outlines
        'tp-border':    '#424754',
        'tp-outline':   '#8c909f',

        // Brand
        'tp-primary':   '#4d8eff',
        'tp-accent':    '#adc6ff',
        'tp-tertiary':  '#89ceff',

        // Semantic status
        'tp-success': '#22c55e',
        'tp-warning': '#f59e0b',
        'tp-danger':  '#ef4444',
        'tp-error':   '#ffb4ab',

        // Text
        'tp-text':  '#dae2fd',
        'tp-muted': '#c2c6d6',

        // On-primary (text on primary bg)
        'tp-on-primary': '#001a42',
      },
      fontFamily: {
        sans:    ['Inter', ...defaultTheme.fontFamily.sans],
        display: ['Manrope', ...defaultTheme.fontFamily.sans],
        mono:    ['JetBrains Mono', ...defaultTheme.fontFamily.mono],
      },
      borderRadius: {
        'xl':  '0.75rem',
        '2xl': '1rem',
        '3xl': '1.5rem',
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
        'glow':    '0 0 20px rgba(77, 142, 255, 0.15)',
        'glow-lg': '0 0 40px rgba(77, 142, 255, 0.2)',
        'ambient': '0 20px 40px rgba(0, 0, 0, 0.4)',
      },
    },
  },
  plugins: [],
} satisfies Config
