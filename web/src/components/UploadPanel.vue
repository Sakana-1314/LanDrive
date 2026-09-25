<script setup lang="ts">
// 右下角上传进度浮窗：常驻在各页面之上，实时显示每个文件的上传进度。
//
// 为什么做成浮窗而不是页面内的一块：上传是个"人走了它还在跑"的操作。
// 之前队列嵌在「我的文件」列表上方，一离开该页面就看不见进度，也没法取消。
// 浮窗挂在主框架上，与所在地页面无关；收起后只剩标题一行，不挡内容。
//
// 交互约定：
//   - 队列为空时完全不渲染（不占位、不显示空浮层）；
//   - 全部传完后自动收起为一行（结果仍可展开回看），
//     但有失败时**保持展开** —— 失败要立刻被看见，不能自己缩起来；
//   - 折叠状态记在 localStorage，刷新页面后保持用户上次的选择。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NButton, NIcon, NProgress, useMessage } from 'naive-ui'
import { CheckmarkCircleOutline, ChevronDownOutline, ChevronUpOutline, CloudUploadOutline } from '@vicons/ionicons5'
import UploadQueue from '@/components/UploadQueue.vue'
import {
  cancelUploadTask,
  clearFinishedUploads,
  onUploadDone,
  onUploadError,
  removeUploadTask,
  retryUploadTask,
  uploadQueueState,
  uploadsActive
} from '@/stores/upload'
import type { UploadTask } from '@/utils/upload'
import { formatDuration, formatSpeed } from '@/utils/format'
import { panelTitle, summarizeUploads, summaryStatus } from '@/utils/uploadSummary'

const COLLAPSE_KEY = 'lanfs-upload-panel-collapsed'

const message = useMessage()

// 完成/失败的提示放在浮窗上而不是「我的文件」页面里：浮窗是常驻的，
// 用户切到别的页面时上传照样会结束，提示不能跟着页面一起消失
// （两处都发则会重复提示，因此只在这里发）。
const offDone = onUploadDone((t) => message.success(`「${t.file.name}」已上传`))
const offError = onUploadError((t) => message.error(`「${t.file.name}」上传失败：${t.error}`))
onBeforeUnmount(() => {
  offDone()
  offError()
})

/** 有任务才渲染：没有上传时页面上不该多出任何东西。 */
const visible = computed(() => uploadQueueState.tasks.length > 0)

const summary = computed(() => summarizeUploads(uploadQueueState.tasks))
const title = computed(() => panelTitle(summary.value))

const collapsed = ref(false)

function readCollapsed(): boolean {
  try {
    return localStorage.getItem(COLLAPSE_KEY) === '1'
  } catch {
    return false
  }
}

function writeCollapsed(v: boolean): void {
  try {
    localStorage.setItem(COLLAPSE_KEY, v ? '1' : '0')
  } catch {
    /* 隐私模式下忽略 */
  }
}

onMounted(() => {
  collapsed.value = readCollapsed()
})

// 全部传完（且没有失败）自动收起：传完的瞬间用户多半已经不看这一块了，
// 留一行"已完成 n 个上传"比摊开一屏已完成列表更省地方。
// 失败时保持展开，否则错误会被自动折叠藏起来。
watch(uploadsActive, (active, was) => {
  if (was && !active && summary.value.failed === 0) {
    collapsed.value = true
    writeCollapsed(true)
  } else if (!was && active) {
    // 又有新的一批开始传：自动展开，否则用户看着"已完成"的一行
    // 根本不知道新文件正在传（点开才知道，等于没说）。
    collapsed.value = false
    writeCollapsed(false)
  }
})

function toggle(): void {
  collapsed.value = !collapsed.value
  writeCollapsed(collapsed.value)
}

/** 标题右侧的一行概览：进行中优先给百分比/速度/剩余，结束后给完成计数。 */
const headMeta = computed(() => {
  const s = summary.value
  if (s.active > 0) {
    const parts = [`${s.percent}%`]
    if (s.speed > 0) parts.push(formatSpeed(s.speed))
    if (s.eta > 0) parts.push(`剩余 ${formatDuration(s.eta)}`)
    return parts.join(' · ')
  }
  if (s.failed > 0) return `${s.finished}/${s.total} 已完成`
  return `共 ${s.total} 个`
})

const progressStatus = computed(() => summaryStatus(summary.value))

async function onCancel(t: UploadTask) {
  await cancelUploadTask(t)
}
</script>

<template>
  <section
    v-if="visible"
    class="upload-panel"
    :class="{ 'is-collapsed': collapsed }"
    aria-label="上传进度"
  >
    <!-- 标题栏即折叠开关：收起后不挡视线，展开后是完整队列 -->
    <button
      type="button"
      class="upload-panel__head"
      :aria-expanded="!collapsed"
      :title="collapsed ? '展开上传进度' : '收起上传进度'"
      @click="toggle"
    >
      <span class="upload-panel__mark" :class="`is-${progressStatus}`">
        <n-icon :size="16">
          <checkmark-circle-outline v-if="!uploadsActive && summary.failed === 0" />
          <cloud-upload-outline v-else />
        </n-icon>
      </span>
      <span class="upload-panel__text">
        <span class="upload-panel__title">{{ title }}</span>
        <span class="upload-panel__meta">{{ headMeta }}</span>
      </span>
      <n-icon class="upload-panel__chevron" :size="16">
        <chevron-up-outline v-if="collapsed" />
        <chevron-down-outline v-else />
      </n-icon>
    </button>

    <div v-if="!collapsed" class="upload-panel__body">
      <!-- 总体进度：按字节汇总，且与标题的百分比同源（一个口径，两处显示） -->
      <n-progress
        type="line"
        :percentage="summary.percent"
        :status="progressStatus"
        :height="6"
        :border-radius="3"
        :show-indicator="false"
        class="upload-panel__bar"
      />
      <UploadQueue
        :tasks="uploadQueueState.tasks"
        @cancel="onCancel"
        @retry="retryUploadTask"
        @remove="removeUploadTask"
        @clear-finished="clearFinishedUploads"
      />
      <n-button size="small" quaternary block @click="toggle">收起面板</n-button>
    </div>
  </section>
</template>

<style scoped>
.upload-panel {
  position: fixed;
  /* 右下角常驻：与具体页面无关，因此挂在主框架上 */
  right: var(--space-lg);
  bottom: var(--space-lg);
  /* 低于整页拖放提示（z-index: 50），高于内容区 */
  z-index: 40;
  width: 360px;
  max-width: calc(100vw - 2 * var(--space-lg));
  border: 1px solid var(--color-border);
  border-radius: var(--radius-card);
  background: var(--color-surface);
  box-shadow: var(--shadow-float);
}

.upload-panel__head {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 0;
  border-radius: var(--radius-card);
  color: inherit;
  text-align: left;
  background: transparent;
  cursor: pointer;
}

.upload-panel__head:hover {
  background: var(--color-surface-soft);
}

.upload-panel__head:focus-visible {
  outline: 2px solid var(--color-primary-border);
  outline-offset: -2px;
}

/* 状态标记：进行中=主色，全部完成=成功色，有失败=危险色 */
.upload-panel__mark {
  display: grid;
  flex: none;
  width: 28px;
  height: 28px;
  place-items: center;
  border-radius: var(--radius-pill);
  color: var(--color-primary);
  background: var(--color-primary-soft);
}

.upload-panel__mark.is-success {
  color: var(--color-success);
  background: var(--color-primary-soft);
}

.upload-panel__mark.is-error {
  color: var(--color-danger);
  background: var(--color-primary-soft);
}

.upload-panel__text {
  display: grid;
  flex: 1;
  min-width: 0;
  gap: 1px;
}

.upload-panel__title {
  overflow: hidden;
  color: var(--color-text-strong);
  font-size: 13px;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.upload-panel__meta {
  overflow: hidden;
  color: var(--color-text-muted);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.upload-panel__chevron {
  flex: none;
  color: var(--color-text-muted);
}

.upload-panel__body {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 10px;
  max-height: min(52vh, 420px);
  /* 任务多时在面板内滚动，不要顶掉页面本身的高度 */
  overflow-y: auto;
  padding: 0 12px 12px;
}

.upload-panel__bar {
  margin-top: 2px;
}

/* 窄屏占满整行：390px 下 360px 的浮窗贴着边缘会显得局促 */
@media (max-width: 768px) {
  .upload-panel {
    right: 12px;
    left: 12px;
    bottom: 12px;
    width: auto;
    max-width: none;
  }

  .upload-panel__body {
    max-height: 46vh;
  }
}
</style>
