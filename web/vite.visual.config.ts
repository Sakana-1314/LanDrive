// 仅用于本地可视化核对（已 gitignore，不提交）：把 /api 代理到 mock 后端。
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'
export default defineConfig({
  plugins: [vue()],
  define: { __API_HOST__: '""', __API_PROBE_TIMEOUT__: '6000' },
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: {
    host: '127.0.0.1',
    port: 4174,
    proxy: { '/api': { target: 'http://127.0.0.1:18081', changeOrigin: true } }
  }
})
