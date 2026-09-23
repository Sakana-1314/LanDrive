<script setup lang="ts">
// docx 预览：用 docx-preview 把文档渲染成 HTML。
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NAlert, NSpin } from 'naive-ui'

const props = defineProps<{ blob: Blob }>()

const host = ref<HTMLElement | null>(null)
const error = ref('')
const loading = ref(true)

async function render() {
  const el = host.value
  if (!el) return
  loading.value = true
  error.value = ''
  el.innerHTML = ''
  try {
    // 动态导入，避免预览库进入主包。
    const docx = await import('docx-preview')
    await docx.renderAsync(props.blob, el, undefined, {
      className: 'docx-preview-root',
      inWrapper: true,
      ignoreWidth: false,
      ignoreHeight: true,
      breakPages: true,
      experimental: true,
      useBase64URL: true
    })
  } catch (e) {
    error.value = `文档渲染失败：${(e as Error).message}`
  } finally {
    loading.value = false
  }
}

onMounted(render)
watch(() => props.blob, render)
onBeforeUnmount(() => {
  if (host.value) host.value.innerHTML = ''
})
</script>

<template>
  <div class="docx-wrap">
    <n-alert v-if="error" type="error" :title="error" style="margin-bottom: 10px" />
    <n-spin v-if="loading" size="large" style="display: block; text-align: center; padding: 30px 0" />
    <div ref="host" class="docx-host" />
  </div>
</template>

<style scoped>
.docx-wrap {
  background: #fff;
  padding: 12px;
  border-radius: 8px;
  overflow: auto;
  max-height: calc(100vh - 130px);
}
.docx-host :deep(.docx-wrapper) {
  background: #f5f7fa;
  padding: 12px;
}
.docx-host :deep(.docx-wrapper > section.docx) {
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.12);
  margin-bottom: 16px;
}
</style>
