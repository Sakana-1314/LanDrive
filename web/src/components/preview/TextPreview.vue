<script setup lang="ts">
// 文本预览：以纯文本展示，完全转义（用 textContent 赋值，天然免疫 XSS）。
import { computed, onMounted, ref, watch } from 'vue'
import { NAlert, NButton, NIcon, NSpin, NSpace } from 'naive-ui'
import { CopyOutline } from '@vicons/ionicons5'

const props = defineProps<{ blob: Blob; name?: string }>()

/** 超过该大小只显示前若干字节，避免浏览器卡死。 */
const MAX_BYTES = 4 * 1024 * 1024

const text = ref('')
const loading = ref(true)
const error = ref('')
const truncated = ref(false)
const pre = ref<HTMLElement | null>(null)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const slice = props.blob.size > MAX_BYTES ? props.blob.slice(0, MAX_BYTES) : props.blob
    truncated.value = props.blob.size > MAX_BYTES
    const buf = await slice.arrayBuffer()
    // 先按 UTF-8 解码；若出现替换字符则回退 GBK（常见的 Windows 中文文本文件）。
    let s = new TextDecoder('utf-8', { fatal: false }).decode(buf)
    if (s.includes('\uFFFD')) {
      try {
        s = new TextDecoder('gbk').decode(buf)
      } catch {
        /* 浏览器不支持 gbk 时保持 utf-8 结果 */
      }
    }
    text.value = s
  } catch (e) {
    error.value = `读取文本失败：${(e as Error).message}`
  } finally {
    loading.value = false
  }
}

const lineCount = computed(() => (text.value ? text.value.split('\n').length : 0))

async function copyAll() {
  try {
    await navigator.clipboard.writeText(text.value)
  } catch {
    /* 非 HTTPS 环境下剪贴板 API 不可用 */
  }
}

onMounted(load)
watch(
  () => props.blob,
  () => {
    void load()
  }
)
</script>

<template>
  <div class="text-wrap">
    <n-alert v-if="error" type="error" :title="error" />
    <n-spin v-else-if="loading" size="large" style="display: block; text-align: center; padding: 40px 0" />
    <template v-else>
      <n-space justify="space-between" align="center" style="margin-bottom: 6px">
        <span style="font-size: 12px; opacity: 0.65">
          {{ lineCount }} 行 · {{ (blob.size / 1024).toFixed(1) }} KB
          <span v-if="truncated"> · 文件较大，仅显示前 4 MB</span>
        </span>
        <n-button size="tiny" quaternary @click="copyAll">
          <template #icon>
            <n-icon><copy-outline /></n-icon>
          </template>
          复制全部
        </n-button>
      </n-space>
      <pre ref="pre" class="text-content">{{ text }}</pre>
    </template>
  </div>
</template>

<style scoped>
.text-wrap {
  height: 100%;
  padding: 12px;
  border-radius: var(--radius-card);
  background: var(--color-surface);
}
.text-content {
  margin: 0;
  max-height: calc(100dvh - 190px);
  overflow: auto;
  color: var(--color-text);
  background: var(--color-preview-code-bg);
  border: 1px solid var(--color-preview-code-border);
  border-radius: var(--radius-control);
  padding: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
