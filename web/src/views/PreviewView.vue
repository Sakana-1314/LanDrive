<script setup lang="ts">
// 预览面：按后端 /preview 给出的 kind 选择渲染方式。
//
// 这里**不再有顶部栏**（文件名、类型/大小、下载按钮都按要求删掉了）。
// 本页被两种方式使用：
//   - 嵌在列表页的预览弹层里（`?embed=1`）：整页铺满 iframe，不渲染任何自身 chrome；
//   - 直接访问 `/preview/:id`（旧链接/收藏）：留一个最小的返回入口，避免用户困住。
//
// 图片 / PDF / 音视频走浏览器原生能力（blob URL），
// docx / xlsx / pptx 用动态导入的纯前端库，旧版 Office 格式提示下载。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NEmpty, NIcon, NResult, NSpace, NSpin, NText, useMessage } from 'naive-ui'
import { ArrowBackOutline } from '@vicons/ionicons5'
import { downloadFile, errMsg, fetchFileBlob, getPreviewInfo } from '@/api'
import type { PreviewInfo } from '@/api/types'
import { closePreview } from '@/stores/preview'

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

/**
 * 是否被弹层嵌入。
 *
 * 嵌入时不渲染返回键（外层已有悬浮关闭键），页面也必须铺满，
 * 否则 iframe 里会露出一圈自身背景色，看着像预览面没对齐。
 */
const embedded = computed(() => route.query.embed === '1')

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

/** 不支持预览时不去下载内容，但用户可以主动要原件。 */
async function onDownload() {
  if (!info.value) return
  try {
    await downloadFile(info.value.id, info.value.name)
  } catch (e) {
    message.error(errMsg(e))
  }
}

/** 返回上一页；没有历史时回文件列表。 */
function goBack() {
  if (window.history.length > 1) router.back()
  else router.push('/files')
}

/**
 * Esc 关闭：嵌在弹层里时直接关弹层（同源，可与外层共用 store）；
 * 独立访问时退回文件列表。
 */
function onKeydown(e: KeyboardEvent) {
  if (e.key !== 'Escape') return
  if (embedded.value) closePreview()
  else goBack()
}

let slowTimer: number | undefined

onMounted(() => {
  window.addEventListener('keydown', onKeydown)
  slowTimer = window.setTimeout(() => (slowHint.value = true), 8000)
  void load().finally(() => {
    if (slowTimer) window.clearTimeout(slowTimer)
    slowHint.value = false
  })
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
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
  <div class="preview-page" :class="{ 'preview-page--embedded': embedded }">
    <!-- 独立访问时的返回入口：嵌入态由外层弹层的悬浮键负责，这里不再重复。 -->
    <n-button v-if="!embedded" class="preview-back" quaternary circle aria-label="返回" @click="goBack">
      <template #icon>
        <n-icon><arrow-back-outline /></n-icon>
      </template>
    </n-button>

    <n-spin v-if="loading" size="large" class="preview-state">
      <template #description>
        <n-space vertical align="center">
          <n-text>正在加载文件内容…</n-text>
          <n-text v-if="slowHint" depth="3">文件较大，加载中…</n-text>
        </n-space>
      </template>
    </n-spin>

    <n-result
      v-else-if="loadError"
      class="preview-state"
      status="error"
      title="无法预览"
      :description="loadError"
    >
      <template #footer>
        <n-space justify="center">
          <n-button @click="load">重试</n-button>
          <n-button type="primary" @click="onDownload">下载文件</n-button>
        </n-space>
      </template>
    </n-result>

    <div
      v-else-if="info && (info.kind === 'unsupported' || info.kind === 'legacy-office')"
      class="preview-state"
    >
      <n-empty :description="info.note || '该格式不支持在线预览'" style="padding: 40px 0">
        <template #extra>
          <n-button type="primary" @click="onDownload">下载文件</n-button>
        </template>
      </n-empty>
    </div>

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

    <n-empty v-else class="preview-state" description="没有可预览的内容" style="padding: 60px 0" />
  </div>
</template>

<style scoped>
.preview-page {
  position: relative;
  display: grid;
  /* 单行铺满：预览面是「占满视口的一块」，不给子项留 auto 行
     （auto 行只按内容高度撑开，100% 高度在子项里会解析不出来，
     docx/pptx 这类自带滚动区的预览就滚不动了）。 */
  grid-template-rows: minmax(0, 1fr);
  height: 100dvh;
  padding: 12px;
  box-sizing: border-box;
  overflow: auto;
  background: var(--page-glow), var(--color-bg);
}

/* 嵌入弹层时铺满：iframe 内不留内边距与背景光晕，否则四周会露出一圈异色边。 */
.preview-page--embedded {
  padding: 0;
  background: var(--color-preview-stage);
}

/* 弹层的悬浮关闭键压在本页右上角，因此嵌入态要给右上角留出安全区：
   否则会盖住预览内容自己的右上角控件 —— 文本预览的「复制全部」就正好在
   那个位置，被盖住后点不动（截图里发现的）。 */
.preview-page--embedded .text-wrap {
  padding-right: 44px;
}

/* 独立访问时的返回键：悬浮在左上角，不给预览面加整条标题栏。 */
.preview-back {
  position: absolute;
  top: 10px;
  left: 10px;
  z-index: 2;
  background: var(--color-surface-translucent);
  box-shadow: var(--shadow-card);
}

/* 加载/错误/空态统一居中：这些是整页状态，不该缩在顶上。 */
.preview-state {
  align-self: center;
  justify-self: center;
  width: 100%;
  padding: 40px 16px;
  text-align: center;
}

.frame {
  width: 100%;
  height: 100%;
  min-height: 0;
  border: none;
  border-radius: var(--radius-card);
  background: var(--color-preview-paper);
}

/* 嵌入态没有外边距可留：PDF 直接铺满 iframe。 */
.preview-page--embedded .frame {
  height: 100dvh;
  border-radius: 0;
}

.image-wrap {
  display: grid;
  place-items: center;
  text-align: center;
  background: var(--color-preview-stage);
  border-radius: var(--radius-card);
  padding: 12px;
}

.preview-page--embedded .image-wrap {
  border-radius: 0;
  padding: 0;
}

.image {
  max-width: 100%;
  max-height: 100%;
  border-radius: var(--radius-control);
}

.media-wrap {
  background: var(--color-preview-slide-backdrop);
  border-radius: var(--radius-card);
  padding: 12px;
  text-align: center;
}

.preview-page--embedded .media-wrap {
  border-radius: 0;
  padding: 0;
}

.video {
  max-width: 100%;
  max-height: 100%;
}
</style>
