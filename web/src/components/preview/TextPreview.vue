<script setup lang="ts">
// 文本预览：以纯文本展示，完全转义（用 textContent 赋值，天然免疫 XSS）。
import { computed, onMounted, ref, watch } from 'vue'
import { NAlert, NButton, NIcon, NSpin } from 'naive-ui'
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
    <n-spin v-else-if="loading" size="large" class="text-loading" />
    <template v-else>
      <!-- 工具条：与内容面同一套内边距，右侧给悬浮关闭键留出安全区 -->
      <div class="text-bar">
        <span class="text-meta">
          {{ lineCount }} 行 · {{ (blob.size / 1024).toFixed(1) }} KB
          <span v-if="truncated"> · 文件较大，仅显示前 4 MB</span>
        </span>
        <n-button size="tiny" quaternary @click="copyAll">
          <template #icon>
            <n-icon><copy-outline /></n-icon>
          </template>
          复制全部
        </n-button>
      </div>
      <pre ref="pre" class="text-content">{{ text }}</pre>
    </template>
  </div>
</template>

<style scoped>
/**
 * 内容面契约：只画内容面（底色 + 圆角 + 内边距），舞台由 PreviewView 提供。
 * 滚动归舞台，因此这里**不再写 calc(100dvh - 190px)** 这种魔数。
 */
.text-wrap {
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100%;
  padding: var(--preview-pad);
  border-radius: var(--radius-card);
  background: var(--color-surface);
}

.text-loading {
  margin: auto;
}

.text-bar {
  display: flex;
  flex: none;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-sm);
  margin-bottom: var(--space-xs);
  /* 悬浮关闭键压在预览面右上角：这里留出安全区，否则会盖住「复制全部」 */
  padding-right: 36px;
}

.text-meta {
  color: var(--color-text-muted);
  font-size: 12px;
}

/* 正文自己滚（拿剩余高度，不用魔数） */
.text-content {
  flex: 1;
  min-height: 0;
  margin: 0;
  overflow: auto;
  color: var(--color-text);
  background: var(--color-preview-code-bg);
  border: 1px solid var(--color-preview-code-border);
  border-radius: var(--radius-control);
  padding: var(--preview-pad);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
