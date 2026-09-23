<script setup lang="ts">
// 预览页：按后端 /preview 给出的 kind 选择渲染方式。
//
// 图片 / PDF / 音视频走浏览器原生能力（blob URL），
// docx / xlsx / pptx 用动态导入的纯前端库，旧版 Office 格式提示下载。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NButton,
  NCard,
  NEmpty,
  NIcon,
  NResult,
  NSpace,
  NSpin,
  NTag,
  NText,
  useMessage
} from 'naive-ui'
import { ArrowBackOutline, DownloadOutline } from '@vicons/ionicons5'
import { downloadFile, errMsg, fetchFileBlob, getPreviewInfo } from '@/api'
import type { PreviewInfo } from '@/api/types'
import { extLabel, formatBytes } from '@/utils/format'
import { extTagType } from '@/utils/theme'

const route = useRoute()
const router = useRouter()
const message = useMessage()

const info = ref<PreviewInfo | null>(null)
const blob = ref<Blob | null>(null)
const objectUrl = ref('')
const loading = ref(true)
const loadError = ref('')
/** 长时间无进展时展示提示，避免用户以为卡死。 */
const slowHint = ref(false)

const id = computed(() => Number(route.params.id))

async function load() {
  loading.value = true
  loadError.value = ''
  blob.value = null
  revoke()
  try {
    const meta = await getPreviewInfo(id.value)
    info.value = meta
    // 不支持的类型不必下载内容，直接提示。
    if (meta.kind === 'unsupported' || meta.kind === 'legacy-office') {
      loading.value = false
      return
    }
    const b = await fetchFileBlob(id.value)
    blob.value = b
    if (meta.kind === 'pdf' || meta.kind === 'video' || meta.kind === 'audio' || meta.kind === 'image') {
      objectUrl.value = URL.createObjectURL(b)
    }
  } catch (e) {
    loadError.value = errMsg(e)
  } finally {
    loading.value = false
  }
}

function revoke() {
  if (objectUrl.value) {
    URL.revokeObjectURL(objectUrl.value)
    objectUrl.value = ''
  }
}

async function onDownload() {
  if (!info.value) return
  try {
    await downloadFile(info.value.id, info.value.name)
  } catch (e) {
    message.error(errMsg(e))
  }
}

function goBack() {
  if (window.history.length > 1) router.back()
  else router.push('/files')
}

let slowTimer: number | undefined

onMounted(() => {
  slowTimer = window.setTimeout(() => (slowHint.value = true), 8000)
  void load().finally(() => {
    if (slowTimer) window.clearTimeout(slowTimer)
    slowHint.value = false
  })
})

onBeforeUnmount(() => {
  if (slowTimer) window.clearTimeout(slowTimer)
  revoke()
})

// 动态导入预览组件：主包不包含任何预览库。
import { defineAsyncComponent } from 'vue'
const DocxPreview = defineAsyncComponent(() => import('@/components/preview/DocxPreview.vue'))
const XlsxPreview = defineAsyncComponent(() => import('@/components/preview/XlsxPreview.vue'))
const PptxPreview = defineAsyncComponent(() => import('@/components/preview/PptxPreview.vue'))
const TextPreview = defineAsyncComponent(() => import('@/components/preview/TextPreview.vue'))
</script>

<template>
  <div class="preview-page">
    <n-card class="card-surface preview-bar" :bordered="false">
      <div class="bar-left">
        <n-button quaternary circle aria-label="返回" @click="goBack">
          <template #icon>
            <n-icon><arrow-back-outline /></n-icon>
          </template>
        </n-button>
        <span class="bar-name" :title="info?.name">{{ info?.name || '文件预览' }}</span>
        <n-tag v-if="info" size="small" :type="extTagType(info.ext)" :bordered="false">
          {{ extLabel(info.ext) }}
        </n-tag>
        <n-tag v-if="info" size="small" :bordered="false">{{ formatBytes(info.size_bytes) }}</n-tag>
      </div>
      <n-button size="small" type="primary" @click="onDownload">
        <template #icon>
          <n-icon><download-outline /></n-icon>
        </template>
        下载
      </n-button>
    </n-card>

    <n-spin v-if="loading" size="large" style="display: block; text-align: center; padding: 80px 0">
      <template #description>
        <n-space vertical align="center">
          <n-text>正在加载文件内容…</n-text>
          <n-text v-if="slowHint" depth="3">文件较大，加载中…</n-text>
        </n-space>
      </template>
    </n-spin>

    <n-result v-else-if="loadError" status="error" title="无法预览" :description="loadError">
      <template #footer>
        <n-space justify="center">
          <n-button @click="load">重试</n-button>
          <n-button type="primary" @click="onDownload">下载文件</n-button>
        </n-space>
      </template>
    </n-result>

    <n-card v-else-if="info && (info.kind === 'unsupported' || info.kind === 'legacy-office')" :bordered="false" style="border-radius: 8px">
      <n-empty :description="info.note || '该格式不支持在线预览'" style="padding: 40px 0">
        <template #extra>
          <n-button type="primary" @click="onDownload">下载文件</n-button>
        </template>
      </n-empty>
    </n-card>

    <template v-else-if="info && blob">
      <!-- Word -->
      <DocxPreview v-if="info.kind === 'docx'" :blob="blob" />

      <!-- Excel -->
      <XlsxPreview v-else-if="info.kind === 'xlsx'" :blob="blob" />

      <!-- PowerPoint -->
      <PptxPreview v-else-if="info.kind === 'pptx'" :blob="blob" />

      <!-- PDF -->
      <iframe
        v-else-if="info.kind === 'pdf'"
        :src="objectUrl"
        class="frame"
        title="PDF 预览"
      />

      <!-- 图片 -->
      <div v-else-if="info.kind === 'image'" class="image-wrap">
        <img :src="objectUrl" :alt="info.name" class="image" />
      </div>

      <!-- 音视频 -->
      <div v-else-if="info.kind === 'video'" class="media-wrap">
        <video :src="objectUrl" controls preload="metadata" class="video" />
      </div>
      <div v-else-if="info.kind === 'audio'" class="media-wrap">
        <audio :src="objectUrl" controls preload="metadata" style="width: 100%" />
      </div>

      <!-- 文本 -->
      <TextPreview v-else-if="info.kind === 'text'" :blob="blob" :name="info.name" />
    </template>

    <n-empty v-else description="没有可预览的内容" style="padding: 60px 0" />
  </div>
</template>

<style scoped>
.preview-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.bar-left {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
}

.bar-name {
  min-width: 0;
  overflow: hidden;
  color: var(--color-text-strong);
  font-size: 15px;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 移动端：文件名独占一行，操作键降级到下一行 */
@media (max-width: 768px) {
  .preview-bar {
    flex-wrap: wrap;
  }

  .bar-left {
    flex: 1 1 100%;
  }
}

.preview-page {
  height: 100vh;
  padding: 12px;
  box-sizing: border-box;
  overflow: auto;
  background: #f5f7fa;
}
.frame {
  width: 100%;
  height: calc(100vh - 130px);
  border: none;
  border-radius: 8px;
  background: #fff;
}
.image-wrap {
  text-align: center;
  background: #fff;
  border-radius: 8px;
  padding: 12px;
}
.image {
  max-width: 100%;
  max-height: calc(100vh - 150px);
  border-radius: 6px;
}
.media-wrap {
  background: #000;
  border-radius: 8px;
  padding: 12px;
  text-align: center;
}
.video {
  max-width: 100%;
  max-height: calc(100vh - 170px);
}
</style>
