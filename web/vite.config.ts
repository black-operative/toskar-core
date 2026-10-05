/// <reference types="vitest/config" />

import react from '@vitejs/plugin-react'
import path from 'node:path'
import { defineConfig, searchForWorkspaceRoot, type ProxyOptions } from 'vite'

// The daemon refuses browser requests from origins other than its own
// (websites must not use the local API). This dev server is a trusted hop,
// so it drops the page's Origin, like toskarctl sending none.
const dropOrigin: ProxyOptions['configure'] = (proxy) => {
  proxy.on('proxyReq', (req) => req.removeHeader('origin'))
}

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: 'dist',
  },
  server: {
    fs: {
      // The translation catalog is shared with the iPhone app, outside web/.
      allow: [searchForWorkspaceRoot(process.cwd()), path.resolve(__dirname, '../i18n')],
    },
    proxy: {
      '/api': {
        target: 'http://localhost:7331',
        changeOrigin: true,
        configure: dropOrigin,
      },
      '/v1': {
        target: 'http://localhost:7331',
        changeOrigin: true,
        configure: dropOrigin,
      },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    globals: true,
  },
})
