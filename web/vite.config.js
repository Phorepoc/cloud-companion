// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { quasar } from '@quasar/vite-plugin'

export default defineConfig({
  base: './',
  plugins: [vue(), quasar()],
  build: { outDir: 'dist', emptyOutDir: true, chunkSizeWarningLimit: 900 },
})
