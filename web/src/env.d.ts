/// <reference types="vite/client" />

/**
 * 后端域名，由 vite.config.ts 从构建期环境变量 HOST 注入（如 https://api.example.com）。
 * 仅 origin，不含 /api；为空串表示走同源 /api。
 */
declare const __API_HOST__: string

/** 接口连通性探测超时（毫秒），由构建期环境变量 VITE_API_PROBE_TIMEOUT 注入。 */
declare const __API_PROBE_TIMEOUT__: number

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<{}, {}, any>
  export default component
}

// pptx-preview 未提供类型声明，这里给出本项目实际用到的形状。
declare module 'pptx-preview' {
  export interface PptxPreviewer {
    preview: (data: ArrayBuffer | Blob) => Promise<void> | void
    destroy?: () => void
  }
  export interface PptxPreviewOptions {
    width?: number
    height?: number
  }
  export function init(element: HTMLElement, options?: PptxPreviewOptions): PptxPreviewer
}
