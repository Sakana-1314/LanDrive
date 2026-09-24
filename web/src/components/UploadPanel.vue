<script setup lang="ts">
// 上传面板：内嵌在「我的文件」里，不再单独成页。
//
// 为什么做成组件而不是页面：上传的目标目录就是"我的文件"，单独一个 tab
// 意味着选中目录后还要再切一次页面。现在拖到文件列表上方即可。
//
// 文案取舍：不显示「最大 500 MB / 不限类型」这类策略角标 —— 超限时校验会
// 直接报错，平时不需要占位。仅"上传已暂停"属于异常态，必须说清。
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  NAlert,
  NButton,
  NIcon,
  NProgress,
  NTag,
  NUpload,
  NUploadDragger,
  useMessage,
  type UploadFileInfo
} from 'naive-ui'
import { CheckmarkCircleOutline, CloudUploadOutline, TrashOutline } from '@vicons/ionicons5'
import { errMsg, uploadConfig } from '@/api'
import { applyUploadConfig, state as userState, uploadState, validateFile } from '@/stores/user'
import { UploadManager, sweepPersisted, type UploadTask } from '@/utils/upload'
import { formatBytes, formatDuration, formatSpeed } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    /**
     * 上传目标文件夹（0 = 用户根目录）。
     * 由 FilesView 传入当前所在目录：在目录里拖文件就上传到该目录。
     */
    folderId?: number
  }>(),
  { folderId: 0 }
)

const emit = defineEmits<{ (e: 'uploaded'): void }>()

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
        // 通知父级刷新列表，新文件立刻可见
        emit('uploaded')
      },
      onError: (t) => {
        message.error(`「${t.file.name}」上传失败：${t.error}`)
        tasks.value = [...tasks.value]
      }
    })
  }
  // 首次创建时同步一次当前目录。
  manager.setFolderId(props.folderId || 0)
  return manager
}

// 用户在目录间切换时更新目标目录；不重建 manager，避免丢掉进行中的队列。
watch(
  () => props.folderId,
  (v) => ensureManager().setFolderId(v || 0)
)

async function loadConfig() {
  try {
    applyUploadConfig(await uploadConfig())
  } catch (e) {
    message.error(errMsg(e))
  } finally {
    configLoaded.value = true
  }
}

/** 处理选中的文件：前端即时校验，不合格直接拦截。 */
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

const uploadDisabled = computed(() => configLoaded.value && !uploadState.uploadEnabled)

onMounted(() => {
  sweepPersisted()
  void loadConfig()
})
</script>

<template>
  <div class="upload-panel">
    <!-- 异常态：必须说明原因，否则用户不知道为何拖不进去 -->
    <n-alert v-if="uploadDisabled" type="warning" title="上传已暂停">
      管理员已暂停上传功能。
    </n-alert>

    <n-upload
      multiple
      :show-file-list="false"
      :disabled="uploadDisabled"
      :custom-request="() => {}"
      @before-upload="onBeforeUpload"
    >
      <!-- 紧凑单行拖拽条：整块文件区域都是上传入口，不额外占一屏 -->
      <n-upload-dragger class="drop">
        <div class="drop__inner">
          <n-icon :size="20"><cloud-upload-outline /></n-icon>
          <span class="drop__text">拖入文件，或点击选择</span>
        </div>
      </n-upload-dragger>
    </n-upload>

    <!-- 队列仅在真的有任务时出现，平时不占空间 -->
    <div v-if="tasks.length" class="queue">
      <div class="queue__head">
        <span class="queue__title">
          上传队列
          <n-tag v-if="activeCount" size="small" type="info" :bordered="false">
            {{ activeCount }} 进行中
          </n-tag>
        </span>
        <n-button size="small" quaternary :disabled="!finishedCount" @click="clearFinished">
          清除已完成
        </n-button>
      </div>

      <ul class="task-list">
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

            <n-button
              v-if="t.state === 'done' && t.result"
              size="small"
              quaternary
              type="primary"
              @click="previewFile(t)"
            >
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
    </div>
  </div>
</template>

<style scoped>
.upload-panel {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 12px;
  min-width: 0;
}

/* 紧凑拖拽条：单行高度，不抢文件列表的视觉重心 */
.drop :deep(.n-upload-dragger) {
  padding: 10px 14px;
}

.drop__inner {
  display: flex;
  align-items: center;
  gap: 8px;
  justify-content: center;
  color: var(--color-text-muted);
}

.drop__text {
  font-size: 14px;
  font-weight: 550;
}

.queue {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  min-width: 0;
  gap: 10px;
}

.queue__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
}

.queue__title {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--color-text-strong);
  font-size: 14px;
  font-weight: 650;
}

.task-list {
  display: grid;
  /* minmax(0,…) 防止 nowrap 内容把网格列撑宽（grid 子项默认 min-width:auto） */
  grid-template-columns: minmax(0, 1fr);
  min-width: 0;
  gap: 10px;
  margin: 0;
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
  /* 移动端：进度信息换行到操作键上方，避免挤压 */
  .task__foot {
    flex-wrap: wrap;
  }

  .task__size {
    display: none;
  }
}
</style>
