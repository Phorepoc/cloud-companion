// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { quasar } from '@quasar/vite-plugin'
import { brand, iconSvg, manifest } from './src/brand.js'

// The brand token (src/brand.js) feeds the page title, the manifest and the icon, so renaming the product is one edit.
function brandPlugin() {
  return {
    name: 'brand-token',
    transformIndexHtml: (html) => html.replaceAll('%BRAND_NAME%', brand.name).replaceAll('%THEME_COLOR%', brand.colors.light),
    generateBundle() {
      this.emitFile({ type: 'asset', fileName: 'manifest.webmanifest', source: manifest() })
      this.emitFile({ type: 'asset', fileName: 'icon.svg', source: iconSvg() })
    },
  }
}

export default defineConfig({
  base: './',
  plugins: [vue(), quasar(), brandPlugin()],
  build: { outDir: 'dist', emptyOutDir: true, chunkSizeWarningLimit: 900 },
})
