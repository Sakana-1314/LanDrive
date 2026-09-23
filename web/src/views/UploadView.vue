<script setup lang="ts">
// 上传页：拖拽/选择文件 → 分片并发上传 → 断点续传 → 进度与结果。
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NIcon,
  NProgress,
  NSpace,
  NTag,
  NText,
  NUpload,
  NUploadDragger,
  useMessage,
  type UploadFileInfo
} from 'naive-ui'
import { CloudUploadOutline, DocumentOutline, TrashOutline } from '@vicons/ionicons5'
import { errMsg, uploadConfig } from '@/api'
import { applyUploadConfig, state as userState, uploadState, validateFile } from '@/stores/user'
import { UploadManager, sweepPersisted, type UploadTask } from '@/utils/upload'
import { formatBytes, formatDuration, formatSpeed } from '@/utils/format'

const router = useRouter()
const message = useMessage()

const tasks = ref<UploadTask[]>([])
let manager: UploadManager | null = null

const configLoaded = ref(false)

function ensureManager(): UploadManager {
  if (!manager) {
    manager = new UploadManager(userState.user?.employee_no || 'anon', {
      onUpdate: () => {
        // 触发响应式更新（tasks 内的对象是同一引用）。
        tasks.value = [...tasks.value]
      },
      onDone: (t) => {
        message.success(`「${t.file.name}」上传完成`)
        tasks.value = [...tasks.value]
      },
      onError: (t) => {
        message.error(`「${t.file.name}」上传失败：${t.error}`)
        tasks.value = [...tasks.value]
      }
    })
  }
  return manager
}

async function loadConfig() {
  try {
    applyUploadConfig(await uploadConfig())
  } catch (e) {
    message.error(errMsg(e))
  } finally {
    configLoaded.value = true
  }
}

/** 处理 n-upload 选中的文件：前端即时校验，不合格直接拦截。 */
function onBeforeUpload(data: { file: UploadFileInfo }): boolean {
  const raw = data.file.file
  if (!raw) return false
  const err = validateFile(raw)
  if (err) {
    message.error(`${raw.name}：${err}`)
    return false
  }
  ensureManager().add(raw)
  tasks.value = [...tasks.value]
  return false // 完全自行控制上传，禁用组件内建行为
}

async function cancel(t: UploadTask) {
  await ensureManager().cancel(t)
  tasks.value = [...tasks.value]
}

function removeTask(t: UploadTask) {
  ensureManager().remove(t)
  tasks.value = [...tasks.value]
}

function retry(t: UploadTask) {
  if (t.state !== 'error') return
  const m = ensureManager()
  m.remove(t)
  const nt = m.add(t.file)
  tasks.value = [...m.tasks]
  void nt
}

function previewFile(t: UploadTask) {
  if (t.result) {
    window.open(router.resolve({ name: 'preview', params: { id: t.result.id } }).href, '_blank')
  }
}

function clearFinished() {
  const m = ensureManager()
  m.tasks.filter((t) => t.state === 'done' || t.state === 'canceled').forEach((t) => m.remove(t))
  tasks.value = [...m.tasks]
}

const activeCount = computed(
  () => tasks.value.filter((t) => t.state === 'uploading' || t.state === 'hashing' || t.state === 'merging').length
)

function progressStatus(t: UploadTask): 'default' | 'success' | 'error' | 'warning' {
  if (t.state === 'done') return 'success'
  if (t.state === 'error') return 'error'
  if (t.state === 'canceled') return 'warning'
  return 'default'
}

function stateLabel(t: UploadTask): string {
  switch (t.state) {
    case 'pending':
      return '等待中'
    case 'hashing':
      return '校验中'
    case 'uploading':
      return t.resumed ? '续传中' : '上传中'
    case 'merging':
      return '服务器合并中'
    case 'done':
      return '已完成'
    case 'error':
      return '失败'
    case 'canceled':
      return '已取消'
  }
}

const allowHint = computed(() => {
  if (!configLoaded.value) return '正在读取上传策略…'
  const parts = [`单文件最大 ${uploadState.maxFileSizeMB} MB`, `分片 ${uploadState.chunkSizeMB} MB`]
  if (uploadState.allowAll) {
    parts.push('允许所有文件类型')
  } else {
    parts.push(`允许类型：${uploadState.allowedExtensions.join('、')}`)
  }
  return parts.join(' · ')
})

onMounted(() => {
  sweepPersisted()
  void loadConfig()
})
</script>

<template>
  <n-space vertical :size="14">
    <n-alert v-if="!uploadState.uploadEnabled && configLoaded" type="warning" title="上传已暂停">
      管理员已暂停上传功能，请联系管理员开启后再试。
    </n-alert>

    <n-card :bordered="false" size="small" style="border-radius: 8px">
      <template #header>
        <n-space align="center" :size="8">
          <n-icon color="#1f6feb"><cloud-upload-outline /></n-icon>
          <n-text strong style="font-size: 16px">上传文件</n-text>
          <n-tag size="small" :bordered="false">{{ allowHint }}</n-tag>
        </n-space>
      </template>

      <n-upload
        multiple
        :show-file-list="false"
        :disabled="configLoaded && !uploadState.uploadEnabled"
        :custom-request="() => {}"
        @before-upload="onBeforeUpload"
      >
        <n-upload-dragger>
          <n-space vertical align="center" :size="8" style="padding: 18px 0">
            <n-icon size="46" color="#1f6feb"><cloud-upload-outline /></n-icon>
            <n-text style="font-size: 15px">点击选择文件，或将文件拖拽到此区域</n-text>
            <n-text depth="3" style="font-size: 12px">
              支持大文件分片上传；刷新页面或断网后可自动续传（未完成的任务保留 24 小时）
            </n-text>
          </n-space>
        </n-upload-dragger>
      </n-upload>
    </n-card>

    <n-card :bordered="false" size="small" style="border-radius: 8px">
      <template #header>
        <n-space align="center" justify="space-between" style="width: 100%">
          <n-space align="center" :size="8">
            <n-icon><document-outline /></n-icon>
            <n-text strong>上传队列</n-text>
            <n-tag v-if="activeCount > 0" size="small" type="info" :bordered="false">
              {{ activeCount }} 个进行中
            </n-tag>
          </n-space>
          <n-button size="small" quaternary :disabled="tasks.length === 0" @click="clearFinished">
            清除已完成
          </n-button>
        </n-space>
      </template>

      <n-empty v-if="tasks.length === 0" description="暂无上传任务" style="padding: 20px 0" />

      <n-space v-else vertical :size="12">
        <div v-for="t in tasks" :key="t.id" class="task-row">
          <n-space justify="space-between" align="center" :size="8">
            <n-space align="center" :size="8" style="min-width: 0">
              <n-text strong style="max-width: 420px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">
                {{ t.file.name }}
              </n-text>
              <n-tag size="tiny" :bordered="false" :type="t.state === 'done' ? 'success' : t.state === 'error' ? 'error' : 'default'">
                {{ stateLabel(t) }}
              </n-tag>
              <n-tag v-if="t.resumed && t.state === 'uploading'" size="tiny" type="info" :bordered="false">
                断点续传
              </n-tag>
            </n-space>
            <n-space :size="6" align="center">
              <n-text depth="3" style="font-size: 12px">
                {{ formatBytes(t.loaded) }} / {{ formatBytes(t.total) }}
                <template v-if="t.state === 'uploading' && t.speed > 0">
                  · {{ formatSpeed(t.speed) }} · 剩余约 {{ formatDuration(t.eta) }}
                </template>
              </n-text>
              <n-button
                v-if="t.state === 'done' && t.result"
                size="tiny"
                quaternary
                type="primary"
                @click="previewFile(t)"
              >
                预览
              </n-button>
              <n-button v-if="t.state === 'error'" size="tiny" quaternary type="primary" @click="retry(t)">
                重试
              </n-button>
              <n-button
                v-if="t.state === 'uploading' || t.state === 'hashing' || t.state === 'merging'"
                size="tiny"
                quaternary
                type="warning"
                @click="cancel(t)"
              >
                取消
              </n-button>
              <n-button
                v-if="t.state === 'done' || t.state === 'error' || t.state === 'canceled'"
                size="tiny"
                quaternary
                @click="removeTask(t)"
              >
                <template #icon>
                  <n-icon><trash-outline /></n-icon>
                </template>
              </n-button>
            </n-space>
          </n-space>

          <n-progress
            type="line"
            :percentage="t.percent"
            :status="progressStatus(t)"
            :height="8"
            :border-radius="4"
            style="margin-top: 6px"
          />
          <n-text v-if="t.error" type="error" style="font-size: 12px">{{ t.error }}</n-text>
          <n-text v-if="t.state === 'done' && t.result" depth="3" style="font-size: 12px">
            已保存到你的目录，{{ formatBytes(t.result.size_bytes) }}，
            {{ new Date(t.result.expires_at).toLocaleDateString() }} 到期
          </n-text>
        </div>
      </n-space>
    </n-card>
  </n-space>
</template>

<style scoped>
.task-row {
  padding: 10px 12px;
  border: 1px solid #efefef;
  border-radius: 8px;
  background: #fafbfc;
}
</style>
