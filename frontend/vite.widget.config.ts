import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'

export default defineConfig({
  publicDir: false,
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  build: {
    outDir: 'dist-widget',
    emptyOutDir: true,
    lib: {
      entry: fileURLToPath(new URL('./src/widget/index.ts', import.meta.url)),
      name: 'WeKnoraWidget',
      formats: ['es', 'iife'],
      fileName: (format) => `weknora-widget.${format === 'es' ? 'js' : 'iife.js'}`,
    },
  },
})
