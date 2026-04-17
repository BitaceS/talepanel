/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,ts}'],
  theme: {
    extend: {
      colors: {
        tp: {
          bg: '#0a0a0f',
          surface: '#12121a',
          surface2: '#1a1a25',
          border: '#2a2a3a',
          text: '#e4e4e7',
          muted: '#71717a',
          primary: '#6366f1',
          success: '#22c55e',
          warning: '#eab308',
          danger: '#ef4444',
        },
      },
    },
  },
  plugins: [],
}
