/// <reference types="vite/client" />

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
