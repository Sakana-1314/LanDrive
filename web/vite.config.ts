import { fileURLToPath, URL } from 'node:url'
import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

// 部署形态：前端在**公网**，后端 API 在**内网**，两者不同源。
//
//   开发：vite dev server 把 /api 代理到本机后端（默认 127.0.0.1:8080），
//         因此开发态不需要 CORS，API 基址用相对路径 /api。
//   生产：通过 VITE_API_BASE_URL 指向内网 API 完整地址（构建期注入），
//         后端需把前端域名加入 LANDRIVE_CORS_ALLOW。
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const proxyTarget = env.VITE_DEV_API_TARGET || 'http://127.0.0.1:8080'

  return {
    plugins: [vue()],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url))
      }
    },
    server: {
      host: '0.0.0.0',
      port: 5173,
      proxy: {
        // 仅开发态生效；生产构建时前端直连 VITE_API_BASE_URL。
        '/api': { target: proxyTarget, changeOrigin: true }
      }
    },
    build: {
      outDir: 'dist',
      emptyOutDir: true,
      // 前端可能挂在反向代理的子路径下，用相对路径引用资源更稳妥。
      assetsDir: 'assets',
      chunkSizeWarningLimit: 3000,
      rollupOptions: {
        output: {
          manualChunks: {
            vendor: ['vue', 'vue-router', 'axios'],
            naive: ['naive-ui'],
            'preview-docx': ['docx-preview'],
            'preview-xlsx': ['exceljs'],
            'preview-pptx': ['pptx-preview']
          }
        }
      }
    }
  }
})
