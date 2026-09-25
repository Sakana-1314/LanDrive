<script setup lang="ts">
// pptx 预览：用 pptx-preview 在浏览器中渲染幻灯片。
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NAlert, NSpin } from 'naive-ui'

const props = defineProps<{ blob: Blob }>()

const host = ref<HTMLElement | null>(null)
const loading = ref(true)
const error = ref('')

/** 幻灯片最大渲染宽度（再宽就只是把白纸拉大，没有意义）。 */
const MAX_WIDTH = 1280

async function render() {
  const el = host.value
  if (!el) return
  loading.value = true
  error.value = ''
  el.innerHTML = ''
  try {
    const mod = await import('pptx-preview')
    /*
     * 按容器实际宽度渲染，保持 16:9。
     *
     * 这里**不能**给宽度设下限：此前写的是 Math.max(640, …)，于是窄屏
     * （如 390px 手机）上幻灯片被硬渲染成 640px 宽，比容器宽 250px，
     * 右侧内容直接被裁掉。宽度以容器为准、只设上限即可 ——
     * 排版要跟着可用空间走，而不是反过来让内容决定布局。
     */
    const width = Math.max(1, Math.min(el.clientWidth || MAX_WIDTH, MAX_WIDTH))
    const previewer = mod.init(el, { width, height: Math.round((width * 9) / 16) })
    const buf = await props.blob.arrayBuffer()
    await previewer.preview(buf)
  } catch (e) {
    error.value = `幻灯片渲染失败：${(e as Error).message}`
  } finally {
    loading.value = false
  }
}

onMounted(render)
watch(
  () => props.blob,
  () => {
    void render()
  }
)
onBeforeUnmount(() => {
  if (host.value) host.value.innerHTML = ''
})
</script>

<template>
  <div class="pptx-wrap">
    <n-alert v-if="error" type="error" :title="error" />
    <n-spin v-if="loading" size="large" class="pptx-loading" />
    <div ref="host" class="pptx-host" />
  </div>
</template>

<style scoped>
/**
 * 内容面契约：与 xlsx / docx / text 一致，组件只负责内容本身，
 * 舞台底色由 PreviewView 的 .preview-stage 唯一提供。
 */
.pptx-wrap {
  min-height: 0;
  height: 100%;
  background: transparent;
}

.pptx-loading {
  display: block;
  margin: var(--space-lg) auto;
  text-align: center;
}

.pptx-host {
  margin: 0 auto;
}

/**
 * pptx-preview 会把 wrapper 硬编码成 `background: #000`（库源码里写死的），
 * 于是出现「双重舞台」：我们的舞台色外面套着库的黑底，浅色档下尤其割裂。
 * 这里把它盖成透明，让统一舞台透出来 —— 幻灯片白纸自己仍然是白的（内容）。
 */
.pptx-host :deep(.pptx-preview-wrapper) {
  background: transparent !important;
}
</style>
