<script setup lang="ts">
// pptx 预览：用 pptx-preview 在浏览器中渲染幻灯片。
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NAlert, NSpin } from 'naive-ui'

const props = defineProps<{ blob: Blob }>()

const host = ref<HTMLElement | null>(null)
const loading = ref(true)
const error = ref('')

async function render() {
  const el = host.value
  if (!el) return
  loading.value = true
  error.value = ''
  el.innerHTML = ''
  try {
    const mod = await import('pptx-preview')
    // 按容器实际宽度渲染，保持 16:9。
    const width = Math.max(640, Math.min(el.clientWidth || 960, 1280))
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
    <n-spin v-if="loading" size="large" style="display: block; text-align: center; padding: 40px 0" />
    <div ref="host" class="pptx-host" />
  </div>
</template>

<style scoped>
.pptx-wrap {
  height: 100%;
  padding: 12px;
  overflow: auto;
  border-radius: var(--radius-card);
  /* 幻灯片外的舞台：深色档下跟着变深，避免深色界面里一块浅灰底。 */
  background: var(--color-preview-slide-backdrop);
}
.pptx-host {
  margin: 0 auto;
}
</style>
