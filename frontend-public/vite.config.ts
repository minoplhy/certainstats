import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import viteCompression from 'vite-plugin-compression'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    viteCompression({ algorithm: 'gzip', ext: '.gz' }),
    viteCompression({ algorithm: 'brotliCompress', ext: '.br' }),
  ],
  base: './', // CRITICAL: Makes the build path-independent (relative assets)
  build: {
    outDir: 'out', // Match Go's expected directory
    emptyOutDir: true,
  }
})

