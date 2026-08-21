import { fileURLToPath, URL } from 'node:url'
import { resolve, dirname } from 'node:path'
import { existsSync, readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueJsx from '@vitejs/plugin-vue-jsx'

const __dirname = dirname(fileURLToPath(import.meta.url))
const require = createRequire(import.meta.url)

function resolveVueOfficePptxEntry(): string {
  try {
    const pkgDir = dirname(require.resolve('@vue-office/pptx/package.json'))
    const candidates = [
      resolve(pkgDir, 'lib/v3/index.js'),
      resolve(pkgDir, 'lib/index.js'),
      resolve(pkgDir, 'lib/v3/vue-office-pptx.mjs'),
    ]
    const matched = candidates.find((candidate) => existsSync(candidate))
    return matched ?? '@vue-office/pptx'
  } catch {
    return '@vue-office/pptx'
  }
}

function serveWidgetBundleInDev(): Plugin {
  return {
    name: 'serve-widget-bundle-in-dev',
    configureServer(server) {
      server.middlewares.use('/widget/weknora-widget.iife.js', (_req, res, next) => {
        const bundlePath = resolve(__dirname, 'dist-widget/weknora-widget.iife.js')
        if (!existsSync(bundlePath)) {
          next()
          return
        }
        res.setHeader('Content-Type', 'application/javascript')
        res.end(readFileSync(bundlePath))
      })
    },
  }
}

export default defineConfig({
  base: process.env.VITE_APP_BASE_PATH || '/',
  plugins: [
    vue(),
    vueJsx(),
    serveWidgetBundleInDev(),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      '@vue-office/pptx': resolveVueOfficePptxEntry(),
    },
  },
  server: {
    port: 5173,
    host: true,
    watch: process.env.VITE_WATCH_USE_POLLING === 'true'
      ? { usePolling: true, interval: 1000 }
      : undefined,
    // 代理配置，用于开发环境
    proxy: {
      '/api': {
        target: process.env.VITE_DEV_API_TARGET || 'http://localhost:8080',
        changeOrigin: true,
        secure: false,
      },
      '/files': {
        target: process.env.VITE_DEV_API_TARGET || 'http://localhost:8080',
        changeOrigin: true,
        secure: false,
      },
      '/swagger': {
        target: process.env.VITE_DEV_API_TARGET || 'http://localhost:8080',
        changeOrigin: true,
        secure: false,
      }
    }
  }
})
