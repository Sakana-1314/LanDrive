<script setup lang="ts">
// docx 预览：用 docx-preview 把文档渲染成 HTML。
//
// 纸张仍是白的 —— 那是文档自身的内容样式（Word 页面本就是白底黑字），
// 不随界面外观变；但纸张**外面**的舞台底色与滚动区必须走令牌，
// 否则深色界面里会插着一块刺眼的浅灰底。
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
  height: 100%;
  padding: 12px;
  overflow: auto;
  border-radius: var(--radius-card);
  /* 纸张外的舞台：深色档下不能用浅色（白纸是内容，舞台是 UI）。 */
  background: var(--color-preview-stage);
}

/* docx-preview 的类名由 renderAsync 的 className 派生：
   容器是 `<className>-wrapper`，每张纸是 `section.<className>`。
   此前这里写成 `.docx-wrapper`（默认 className 才是 docx），与实际的
   `docx-preview-root` 对不上，于是这段覆盖**从未生效**过。 */
.docx-host :deep(.docx-preview-root-wrapper) {
  padding: 12px;
  /* 库内置的是 background: gray，深色档下必须用令牌盖掉。 */
  background: transparent;
}

/* 纸张本身保持白色：这是文档内容，不跟随界面外观。 */
.docx-host :deep(.docx-preview-root-wrapper > section.docx-preview-root) {
  background: var(--color-preview-paper);
  box-shadow: 0 2px 12px rgb(0 0 0 / 18%);
  margin-bottom: 16px;
}
</style>
