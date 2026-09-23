import { fileURLToPath, URL } from 'node:url'
import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

// 部署形态：前端在**公网**，后端 API 在**内网**，两者不同源。
//
// 后端域名由**构建期**环境变量 `HOST` 注入（与同组织其它前端项目保持一致），
// 取值优先级：进程环境变量 HOST > .env(.production/.development) 中的 HOST。
//   例：HOST=https://api.example.com npm run build
//       docker build --build-arg HOST=https://xxx -f web/Dockerfile web
// 未设置时 `__API_HOST__` 为空串，前端走同源 `/api`（适用于反向代理部署）。
//
// 开发态：vite dev server 把 /api 代理到本机后端，因此无需设置 HOST。
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')

  // HOST 是后端域名（仅 origin，不含 /api）；结尾斜杠统一去掉。
  const host = ((process.env.HOST ?? '').trim() || (env.HOST ?? '').trim()).replace(/\/+$/, '')

  // 接口连通性探测超时（毫秒）。超时即判定当前网络访问不到内网接口。
  const probeTimeout = Number(
    (process.env.VITE_API_PROBE_TIMEOUT ?? '').trim() || (env.VITE_API_PROBE_TIMEOUT ?? '').trim() || 6000
  )

  // 开发态 vite 代理目标（仅本地开发用）。
  const devTarget = (process.env.VITE_DEV_API_TARGET ?? '').trim() || env.VITE_DEV_API_TARGET || 'http://127.0.0.1:8080'

  return {
    plugins: [vue()],
    define: {
      // 后端域名（构建期注入），前端据此拼出 `${HOST}/api`。
      __API_HOST__: JSON.stringify(host),
      __API_PROBE_TIMEOUT__: JSON.stringify(Number.isFinite(probeTimeout) ? probeTimeout : 6000)
    },
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url))
      }
    },
    server: {
      host: '0.0.0.0',
      port: 5173,
      proxy: {
        // 仅开发态生效；生产构建时前端直连 HOST 指向的内网 API。
        '/api': { target: devTarget, changeOrigin: true }
      }
    },
    build: {
      outDir: 'dist',
      emptyOutDir: true,
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
