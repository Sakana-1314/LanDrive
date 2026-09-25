<script setup lang="ts">
// 公开分享页（/s/:token）—— **免登录**。
//
// 这是拿到链接的人看到的页面，因此刻意不复用主框架（没有侧栏、顶栏），
// 也不假设访问者有账号。三种失效状态必须给出不同文案：
//   过期 → 「该分享链接已过期」
//   目标被删 → 「分享的文件已被删除」
//   链接无效 → 「分享链接无效或已被撤销」
// 混成一句会让用户无法判断该找谁处理。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { NButton, NIcon, NSpin, useMessage } from 'naive-ui'
import {
  AlertCircleOutline,
  CloudDownloadOutline,
  DocumentOutline,
  FolderOpenOutline,
  LinkOutline,
  TimeOutline
} from '@vicons/ionicons5'
import { errMsg, resolveShare, shareDownloadUrl, shareContentUrl } from '@/api'
import type { ShareResolved } from '@/api/types'
import { extLabel, formatBytes, formatTime } from '@/utils/format'

const route = useRoute()
const message = useMessage()

const token = computed(() => String(route.params.token || ''))
const info = ref<ShareResolved | null>(null)
const loading = ref(true)
// 请求本身失败（网络不通）与"分享已失效"是两回事，分开处理。
const loadError = ref('')

const contentUrl = computed(() => (info.value ? shareContentUrl(token.value) : ''))
const downloadUrl = computed(() => (info.value ? shareDownloadUrl(token.value) : ''))

/** 失效时的标题与说明。 */
const failText = computed(() => {
  switch (info.value?.status) {
    case 'expired':
      return { title: '该分享链接已过期', desc: '请联系分享者重新生成一个链接。' }
    case 'deleted':
      return {
        title: info.value.target_type === 'folder' ? '分享的文件夹已被删除' : '分享的文件已被删除',
        desc: '内容已不在服务器上，请联系分享者。'
      }
    default:
      return { title: '分享链接无效或已被撤销', desc: '请确认链接是否完整，或联系分享者。' }
  }
})

/** 有效期文案：永久 / 剩余天数 / 已过期。 */
const expireText = computed(() => {
  const v = info.value
  if (!v) return ''
  if (!v.expires_at) return '永久有效'
  const left = new Date(v.expires_at).getTime() - Date.now()
  if (left <= 0) return '已过期'
  const days = Math.ceil(left / 86400000)
  return days <= 1 ? '不到 1 天后过期' : `${days} 天后过期`
})

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    info.value = await resolveShare(token.value)
  } catch (e) {
    // 这里只会是网络类/服务端错误，因为失效状态是 200 + status。
    loadError.value = errMsg(e)
  } finally {
    loading.value = false
  }
}

/** 预览仅对服务端允许内联的类型开放；其余一律下载。 */
const canPreviewInline = computed(() => {
  const k = info.value?.kind
  return k === 'image' || k === 'pdf' || k === 'video' || k === 'audio'
})

function copyLink() {
  navigator.clipboard
    ?.writeText(window.location.href)
    .then(() => message.success('链接已复制'))
    .catch(() => message.error('复制失败，请手动复制地址栏链接'))
}

onMounted(load)
</script>

<template>
  <div class="share-page">
    <div class="share-card">
      <div class="share-brand">
        <div class="share-brand__mark">
          <img src="/logo.png" alt="局域网文件助手" width="32" height="32" />
        </div>
        <span>局域网文件助手</span>
      </div>

      <div v-if="loading" class="share-center">
        <n-spin size="medium" />
      </div>

      <!-- 网络/服务端错误：与"分享失效"区分开 -->
      <div v-else-if="loadError" class="share-center">
        <n-icon :size="40" color="var(--color-warning)"><alert-circle-outline /></n-icon>
        <h2 class="share-title">无法加载分享内容</h2>
        <p class="share-desc">{{ loadError }}</p>
        <n-button type="primary" @click="load">重试</n-button>
      </div>

      <!-- 分享失效（过期 / 已删除 / 无效） -->
      <div v-else-if="info && info.status !== 'ok'" class="share-center">
        <n-icon :size="40" color="var(--color-warning)"><alert-circle-outline /></n-icon>
        <h2 class="share-title">{{ failText.title }}</h2>
        <p class="share-desc">{{ failText.desc }}</p>
      </div>

      <!-- 正常：显示目标并允许下载/预览 -->
      <div v-else-if="info" class="share-body">
        <div class="share-target">
          <n-icon :size="26" color="var(--color-primary)">
            <folder-open-outline v-if="info.target_type === 'folder'" />
            <document-outline v-else />
          </n-icon>
          <div class="share-target__meta">
            <h2 class="share-name" :title="info.name">{{ info.name }}</h2>
            <p class="share-sub">
              <template v-if="info.target_type === 'folder'">
                {{ info.file_count }} 个文件 · {{ formatBytes(info.total_bytes) }}
              </template>
              <template v-else>
                {{ extLabel(info.ext) }} · {{ formatBytes(info.size_bytes) }}
              </template>
            </p>
          </div>
        </div>

        <div class="share-tags">
          <span class="share-tag">
            <n-icon :size="13"><time-outline /></n-icon>
            {{ expireText }}
          </span>
          <span class="share-tag">
            <n-icon :size="13"><link-outline /></n-icon>
            由 {{ info.owner_name || '他人' }} 分享
          </span>
        </div>

        <!-- 图片/PDF 直接内联，省一次下载 -->
        <div v-if="canPreviewInline && info.kind === 'image'" class="share-preview">
          <img :src="contentUrl" :alt="info.name" />
        </div>
        <div v-else-if="canPreviewInline && info.kind === 'pdf'" class="share-preview share-preview--pdf">
          <iframe :src="contentUrl" :title="info.name" />
        </div>
        <div v-else-if="canPreviewInline && (info.kind === 'video' || info.kind === 'audio')" class="share-preview">
          <video v-if="info.kind === 'video'" :src="contentUrl" controls />
          <audio v-else :src="contentUrl" controls />
        </div>

        <div class="share-actions">
          <n-button tag="a" :href="downloadUrl" type="primary" size="large" block>
            <template #icon>
              <n-icon><cloud-download-outline /></n-icon>
            </template>
            {{ info.target_type === 'folder' ? '下载整个文件夹（zip）' : '下载文件' }}
          </n-button>
          <n-button size="small" quaternary block @click="copyLink">
            <template #icon>
              <n-icon><link-outline /></n-icon>
            </template>
            复制链接
          </n-button>
        </div>

        <p class="share-foot">
          分享于 {{ formatTime(info.created_at || '', true) }}
          <template v-if="info.view_count">· 已被查看 {{ info.view_count }} 次</template>
        </p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.share-page {
  display: grid;
  min-height: 100dvh;
  padding: 24px 16px;
  place-items: center;
  background: var(--page-glow), var(--color-bg);
}

.share-card {
  width: 100%;
  max-width: 520px;
  padding: 22px;
  border: 1px solid var(--color-border-subtle);
  border-radius: var(--radius-card);
  background: var(--color-surface);
  box-shadow: var(--shadow-card);
}

.share-brand {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 20px;
  color: var(--color-text-strong);
  font-size: 15px;
  font-weight: 650;
}

/* 圆角已在 logo.png 的像素里切好，这里不要再叠 border-radius */
.share-brand__mark {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
}

.share-brand__mark img {
  display: block;
  width: 100%;
  height: 100%;
}

.share-center {
  display: grid;
  gap: 10px;
  justify-items: center;
  padding: 24px 0 12px;
  text-align: center;
}

.share-title {
  margin: 6px 0 0;
  color: var(--color-text-strong);
  font-size: 17px;
  font-weight: 650;
}

.share-desc {
  margin: 0 0 8px;
  color: var(--color-text-muted);
  font-size: 14px;
}

.share-target {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.share-target__meta {
  min-width: 0;
}

.share-name {
  margin: 0;
  overflow: hidden;
  color: var(--color-text-strong);
  font-size: 17px;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.share-sub {
  margin: 3px 0 0;
  color: var(--color-text-muted);
  font-size: 13px;
}

.share-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin: 16px 0;
}

.share-tag {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 3px 9px;
  border-radius: var(--radius-pill);
  color: var(--color-text-muted);
  font-size: 12px;
  background: var(--color-surface-soft);
}

.share-preview {
  display: grid;
  margin-bottom: 16px;
  place-items: center;
  border-radius: var(--radius-control);
  background: var(--color-surface-soft);
  overflow: hidden;
}

.share-preview img,
.share-preview video {
  max-width: 100%;
  max-height: 46vh;
}

.share-preview audio {
  width: 100%;
}

.share-preview--pdf iframe {
  width: 100%;
  height: 46vh;
  border: 0;
}

.share-actions {
  display: grid;
  gap: 8px;
}

.share-foot {
  margin: 14px 0 0;
  color: var(--color-text-muted);
  font-size: 12px;
  text-align: center;
}
</style>
