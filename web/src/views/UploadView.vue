<script setup lang="ts">
// 上传页：拖拽/选择文件 → 分片并发上传 → 断点续传 → 进度与结果。
//
// 文案取舍：不再罗列「支持分片 / 刷新可续传 / 24 小时」这类说明 ——
// 上传策略（体积上限、允许类型、分片大小）由顶部标签直接给出，
// 续传由状态标签（续传中）体现，界面本身说明功能。
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NIcon,
  NProgress,
  NTag,
  NUpload,
  NUploadDragger,
  useMessage,
  type UploadFileInfo
} from 'naive-ui'
import {
  CheckmarkCircleOutline,
  CloudUploadOutline,
  TrashOutline
} from '@vicons/ionicons5'
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
        tasks.value = [...tasks.value]
      },
      onDone: (t) => {
        message.success(`「${t.file.name}」已上传`)
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
  m.add(t.file)
  tasks.value = [...m.tasks]
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
  () =>
    tasks.value.filter(
      (t) => t.state === 'uploading' || t.state === 'hashing' || t.state === 'merging'
    ).length
)

const finishedCount = computed(
  () => tasks.value.filter((t) => t.state === 'done' || t.state === 'canceled').length
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
      return '合并中'
    case 'done':
      return '已完成'
    case 'error':
      return '失败'
    case 'canceled':
      return '已取消'
  }
}

function stateType(t: UploadTask): 'default' | 'success' | 'error' | 'warning' | 'info' {
  if (t.state === 'done') return 'success'
  if (t.state === 'error') return 'error'
  if (t.state === 'canceled') return 'warning'
  if (t.state === 'uploading') return 'info'
  return 'default'
}

/** 上传限制：拆成独立标签，比一整句拼接文字更好扫读。 */
const limitTags = computed(() => {
  if (!configLoaded.value) return []
  const tags = [`最大 ${uploadState.maxFileSizeMB} MB`]
  tags.push(uploadState.allowAll ? '不限类型' : uploadState.allowedExtensions.join(' / '))
  return tags
})

const uploadDisabled = computed(() => configLoaded.value && !uploadState.uploadEnabled)

onMounted(() => {
  sweepPersisted()
  void loadConfig()
})
</script>

<template>
  <div class="upload-page">
    <n-alert v-if="uploadDisabled" type="warning" title="上传已暂停">
      管理员已暂停上传功能。
    </n-alert>

    <n-card class="card-surface" :bordered="false">
      <div class="section-head">
        <div class="section-head__title">上传文件</div>
        <div class="limit-tags">
          <n-tag v-for="tag in limitTags" :key="tag" size="small" :bordered="false">{{ tag }}</n-tag>
        </div>
      </div>

      <n-upload
        multiple
        :show-file-list="false"
        :disabled="uploadDisabled"
        :custom-request="() => {}"
        @before-upload="onBeforeUpload"
      >
        <n-upload-dragger class="dragger">
          <div class="dragger-inner">
            <div class="dragger-icon">
              <n-icon :size="30"><cloud-upload-outline /></n-icon>
            </div>
            <div class="dragger-title">点击或拖拽文件到此处</div>
          </div>
        </n-upload-dragger>
      </n-upload>
    </n-card>

    <n-card class="card-surface" :bordered="false">
      <div class="section-head">
        <div class="section-head__title">
          上传队列
          <n-tag v-if="activeCount" size="small" type="info" :bordered="false">
            {{ activeCount }} 进行中
          </n-tag>
        </div>
        <n-button size="small" quaternary :disabled="!finishedCount" @click="clearFinished">
          清除已完成
        </n-button>
      </div>

      <n-empty v-if="!tasks.length" description="暂无上传任务" style="padding: 24px 0" />

      <ul v-else class="task-list">
        <li v-for="t in tasks" :key="t.id" class="task">
          <div class="task__head">
            <span class="task__name" :title="t.file.name">{{ t.file.name }}</span>
            <n-tag size="small" :type="stateType(t)" :bordered="false">{{ stateLabel(t) }}</n-tag>
            <span class="task__spacer" />
            <span class="task__size">{{ formatBytes(t.loaded) }} / {{ formatBytes(t.total) }}</span>
          </div>

          <n-progress
            type="line"
            :percentage="t.percent"
            :status="progressStatus(t)"
            :height="6"
            :border-radius="3"
            :show-indicator="false"
            class="task__bar"
          />

          <div class="task__foot">
            <span v-if="t.state === 'uploading' && t.speed > 0" class="task__speed">
              {{ formatSpeed(t.speed) }} · 剩余 {{ formatDuration(t.eta) }}
            </span>
            <span v-else-if="t.state === 'done' && t.result" class="task__speed">
              <n-icon color="var(--color-success)" :size="14"><checkmark-circle-outline /></n-icon>
              {{ formatBytes(t.result.size_bytes) }}
            </span>
            <span v-else-if="t.error" class="task__error">{{ t.error }}</span>
            <span class="task__spacer" />

            <n-button v-if="t.state === 'done' && t.result" size="small" quaternary type="primary" @click="previewFile(t)">
              预览
            </n-button>
            <n-button v-if="t.state === 'error'" size="small" quaternary type="primary" @click="retry(t)">
              重试
            </n-button>
            <n-button
              v-if="t.state === 'uploading' || t.state === 'hashing' || t.state === 'merging'"
              size="small"
              quaternary
              @click="cancel(t)"
            >
              取消
            </n-button>
            <n-button
              v-if="t.state === 'done' || t.state === 'error' || t.state === 'canceled'"
              size="small"
              quaternary
              aria-label="移除记录"
              @click="removeTask(t)"
            >
              <template #icon>
                <n-icon><trash-outline /></n-icon>
              </template>
            </n-button>
          </div>
        </li>
      </ul>
    </n-card>
  </div>
</template>

<style scoped>
.upload-page {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 14px;
}

.limit-tags {
  display: flex;
  flex: none;
  flex-wrap: wrap;
  gap: 6px;
  justify-content: flex-end;
}

.dragger :deep(.n-upload-dragger) {
  padding: 26px 16px;
}

.dragger-inner {
  display: grid;
  gap: 12px;
  justify-items: center;
}

.dragger-icon {
  display: grid;
  width: 54px;
  height: 54px;
  place-items: center;
  border-radius: 16px;
  color: var(--color-primary);
  background: var(--color-primary-soft);
}

.dragger-title {
  color: var(--color-text-strong);
  font-size: 15px;
  font-weight: 600;
}

.task-list {
  display: grid;
  /* minmax(0,…) 防止 nowrap 内容把网格列撑宽（grid 子项默认 min-width:auto） */
  grid-template-columns: minmax(0, 1fr);
  min-width: 0;
  gap: 10px;
  margin: 12px 0 0;
  padding: 0;
  list-style: none;
}

.task {
  padding: 12px 13px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--color-surface-soft);
}

.task__head,
.task__foot {
  display: flex;
  align-items: center;
  gap: 8px;
}

.task__name {
  min-width: 0;
  overflow: hidden;
  color: var(--color-text-strong);
  font-size: 14px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task__spacer {
  flex: 1;
}

.task__size,
.task__speed {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--color-text-muted);
  font-size: 12px;
  white-space: nowrap;
}

.task__error {
  min-width: 0;
  overflow: hidden;
  color: var(--color-danger);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task__bar {
  margin: 10px 0;
}

@media (max-width: 768px) {
  .section-head {
    align-items: flex-start;
    flex-direction: column;
  }

  .limit-tags {
    justify-content: flex-start;
  }

  /* 移动端：进度信息换行到操作键上方，避免挤压 */
  .task__foot {
    flex-wrap: wrap;
  }

  .task__size {
    display: none;
  }
}
</style>
