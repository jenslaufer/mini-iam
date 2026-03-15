import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  test: {
    environment: 'happy-dom',
    globals: true,
    include: ['src/**/*.test.js'],
    exclude: ['e2e/**'],
  },
  server: {
    port: 3000,
    proxy: {
      '/auth': {
        target: 'http://localhost:8080',
        rewrite: (path) => path.replace(/^\/auth/, ''),
      },
    },
  },
})
