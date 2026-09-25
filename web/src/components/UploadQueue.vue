<script setup lang="ts">
// 上传队列：只负责展示「正在传什么」，不再承担上传入口。
//
// 为什么把入口从这里挪走：「我的文件」整页任意位置都能拖入上传，
// 单独再摆一个虚线拖拽框，既抢版面又让人以为只有框里能拖。
// 入口改到页面工具条的「上传文件」按钮 + 整页拖放（FilesView 里），
// 组件本身则退化成纯粹的队列视图。
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NIcon, NProgress, NTag } from 'naive-ui'
import { CheckmarkCircleOutline, TrashOutline } from '@vicons/ionicons5'
import type { UploadTask } from '@/utils/upload'
import { formatBytes, formatDuration, formatSpeed } from '@/utils/format'
import { uploadStateLabel } from '@/utils/uploadSummary'

const props = defineProps<{
  tasks: UploadTask[]
}>()

const emit = defineEmits<{
  (e: 'cancel', task: UploadTask): void
  (e: 'retry', task: UploadTask): void
  (e: 'remove', task: UploadTask): void
  (e: 'clear-finished'): void
}>()

const router = useRouter()

const activeCount = computed(
  () =>
    props.tasks.filter(
      (t) => t.state === 'uploading' || t.state === 'hashing' || t.state === 'merging'
    ).length
)

const finishedCount = computed(
  () => props.tasks.filter((t) => t.state === 'done' || t.state === 'canceled').length
)

function progressStatus(t: UploadTask): 'default' | 'success' | 'error' | 'warning' {
  if (t.state === 'done') return 'success'
  if (t.state === 'error') return 'error'
  if (t.state === 'canceled') return 'warning'
  return 'default'
}

/** 状态文案与浮窗共用同一份口径（utils/uploadSummary.ts），避免两处各写一套。 */
function stateLabel(t: UploadTask): string {
  return uploadStateLabel(t.state, t.resumed)
}

function stateType(t: UploadTask): 'default' | 'success' | 'error' | 'warning' | 'info' {
  if (t.state === 'done') return 'success'
  if (t.state === 'error') return 'error'
  if (t.state === 'canceled') return 'warning'
  if (t.state === 'uploading') return 'info'
  return 'default'
}

function previewFile(t: UploadTask) {
  if (t.result) {
    window.open(router.resolve({ name: 'preview', params: { id: t.result.id } }).href, '_blank')
  }
}

/**
 * 队列里显示「相对目录/文件名」：拖入文件夹时同一批会有很多同名文件
 * （如多个 1.xlsx），只显示文件名根本分不清是哪一个。
 */
function displayName(t: UploadTask): string {
  return t.dirs.length ? `${t.dirs.join('/')}/${t.file.name}` : t.file.name
}
</script>

<template>
  <div v-if="tasks.length" class="queue">
    <div class="queue__head">
      <span class="queue__title">
        上传队列
        <n-tag v-if="activeCount" size="small" type="info" :bordered="false">
          {{ activeCount }} 进行中
        </n-tag>
      </span>
      <n-button size="small" quaternary :disabled="!finishedCount" @click="emit('clear-finished')">
        清除已完成
      </n-button>
    </div>

    <ul class="task-list">
      <li v-for="t in tasks" :key="t.id" class="task">
        <div class="task__head">
          <span class="task__name" :title="displayName(t)">{{ t.file.name }}</span>
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
          <n-button
            v-if="t.state === 'error'"
            size="small"
            quaternary
            type="primary"
            @click="emit('retry', t)"
          >
            重试
          </n-button>
          <n-button
            v-if="t.state === 'uploading' || t.state === 'hashing' || t.state === 'merging'"
            size="small"
            quaternary
            @click="emit('cancel', t)"
          >
            取消
          </n-button>
          <n-button
            v-if="t.state === 'done' || t.state === 'error' || t.state === 'canceled'"
            size="small"
            quaternary
            aria-label="移除记录"
            @click="emit('remove', t)"
          >
            <template #icon>
              <n-icon><trash-outline /></n-icon>
            </template>
          </n-button>
        </div>
      </li>
    </ul>
  </div>
</template>

<style scoped>
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
