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
    // 不能预览的类型不必下载内容，直接提示：超限的文件连"下载到内存"这一步
    // 都要避免（这正是体积闸要防的事），只在用户点「下载文件」时才去取。
    if (meta.kind === 'unsupported' || meta.kind === 'legacy-office' || meta.kind === 'too-large') {
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
  else router.push('/files/mine')
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
      v-else-if="
        info &&
        (info.kind === 'unsupported' || info.kind === 'legacy-office' || info.kind === 'too-large')
      "
      class="preview-state"
    >
      <n-empty :description="info.note || '该格式不支持在线预览'">
        <template #extra>
          <n-button type="primary" @click="onDownload">下载文件</n-button>
        </template>
      </n-empty>
    </div>

    <template v-else-if="info && blob">
      <!--
        统一骨架：**一个舞台 + 一张内容卡**。
        此前每个预览器各自决定舞台底色、内边距、圆角与「谁来滚动」，
        于是同一份文件在不同类型下观感割裂（docx 浅灰舞台、pptx 深灰舞台、
        xlsx/text 全白、pdf 干脆没有舞台；滚动还分别挂在 wrap / 库内部 /
        带 calc(100dvh - 魔数) 的子元素上）。
        现在舞台与内边距由这里唯一提供，各预览只负责「内容面」本身。
      -->
      <div class="preview-stage">
        <!-- Word / Excel / PowerPoint / 文本：都是「一面内容」，由各自组件渲染 -->
        <DocxPreview v-if="info.kind === 'docx'" :blob="blob" />
        <XlsxPreview v-else-if="info.kind === 'xlsx'" :blob="blob" />
        <PptxPreview v-else-if="info.kind === 'pptx'" :blob="blob" />
        <TextPreview v-else-if="info.kind === 'text'" :blob="blob" :name="info.name" />

        <!-- PDF：浏览器原生查看器，自带白底与工具栏，不需要我们再包一层内容面 -->
        <iframe
          v-else-if="info.kind === 'pdf'"
          :src="objectUrl"
          class="frame"
          title="PDF 预览"
        />

        <!-- 图片 / 音视频：内容居中，舞台色由 .preview-stage 统一给 -->
        <div v-else-if="info.kind === 'image'" class="image-wrap">
          <img :src="objectUrl" :alt="info.name" class="image" />
        </div>
        <div v-else-if="info.kind === 'video'" class="media-wrap">
          <video :src="objectUrl" controls preload="metadata" class="video" />
        </div>
        <div v-else-if="info.kind === 'audio'" class="media-wrap">
          <audio :src="objectUrl" controls preload="metadata" style="width: 100%" />
        </div>
      </div>
    </template>

    <n-empty v-else class="preview-state" description="没有可预览的内容" />
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

/* 独立访问时的返回键：悬浮在左上角，不给预览面加整条标题栏。 */
.preview-back {
  position: absolute;
  top: 10px;
  left: 10px;
  z-index: 2;
  background: var(--color-surface-translucent);
  box-shadow: var(--shadow-card);
}

/* 加载/错误/空态统一居中：这些是整页状态，不该缩在顶上、也不该各写一套内边距。 */
.preview-state {
  align-self: center;
  justify-self: center;
  width: 100%;
  padding: var(--preview-gutter) var(--preview-pad);
  text-align: center;
}

/**
 * 统一舞台：所有可渲染类型的唯一外层。
 *
 * 契约（改这里就等于改所有类型的观感，各预览器不要再自己写舞台）：
 *   - 底色唯一：--color-preview-stage（明暗两档各一个值）；
 *   - 内边距唯一：左右下 --preview-pad，顶部 --preview-gutter
 *     （顶部更大是为了给悬浮的返回键/关闭键让位，否则它会盖住内容自己的
 *     右上角控件 —— 文本预览的「复制全部」就被盖过）；
 *   - 滚动归舞台：内容更高时在这里滚，各预览内部不再各写一套 calc(100dvh - 魔数)。
 */
.preview-stage {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr);
  min-height: 0;
  padding: var(--preview-gutter) var(--preview-pad) var(--preview-pad);
  overflow: auto;
  background: var(--color-preview-stage);
}

/* 舞台里的每一样东西都占满这一格：预览器自己决定高度是否撑满。 */
.preview-stage > * {
  min-height: 0;
}

/* PDF：浏览器原生查看器自带白底与工具栏，直接铺满舞台即可，
   不需要再包一层「内容卡」（包了反而在深色档下多出一圈浅色边框）。 */
.frame {
  width: 100%;
  height: 100%;
  min-height: 0;
  border: none;
  background: var(--color-preview-paper);
}

.image-wrap {
  display: grid;
  place-items: center;
  min-height: 0;
  text-align: center;
}

.image {
  max-width: 100%;
  max-height: 100%;
  border-radius: var(--radius-control);
}

.media-wrap {
  display: grid;
  place-items: center;
  min-height: 0;
  text-align: center;
}

.video {
  max-width: 100%;
  max-height: 100%;
}
</style>
