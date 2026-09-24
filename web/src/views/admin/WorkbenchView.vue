<script setup lang="ts">
// 工作台：账号 / 文件 / 占用 / 回收站四项关键指标 + 到期提醒。
//
// 改名与瘦身说明：
//   - 「统计看板」→「工作台」：它给的是值班时想一眼看到的数字，不是分析报表；
//   - 「最近操作」随审计日志一起移除（日志功能已整体下线）；
//   - 「存储一致性」移到「系统管理」：那是低频的运维动作，
//     与改配置放在一起更顺手，也不占工作台的位置。
import { computed, onMounted, ref } from 'vue'
import { NAlert, NCard, NIcon, NStatistic, useMessage } from 'naive-ui'
import {
  AlertCircleOutline,
  DocumentOutline,
  PeopleOutline,
  ServerOutline,
  TrashOutline
} from '@vicons/ionicons5'
import { adminStats, errMsg } from '@/api'
import type { Stats } from '@/api/types'
import { formatBytes } from '@/utils/format'

const message = useMessage()

const stats = ref<Stats | null>(null)
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    stats.value = await adminStats()
  } catch (e) {
    message.error(errMsg(e))
  } finally {
    loading.value = false
  }
}

const metrics = computed(() => [
  {
    key: 'users',
    label: '账号',
    icon: PeopleOutline,
    color: 'var(--color-primary)',
    value: stats.value?.users ?? '—',
    sub: ''
  },
  {
    key: 'files',
    label: '有效文件',
    icon: DocumentOutline,
    color: 'var(--color-success)',
    value: stats.value?.files ?? '—',
    sub: ''
  },
  {
    key: 'bytes',
    label: '占用空间',
    icon: ServerOutline,
    color: 'var(--color-warning)',
    value: stats.value ? formatBytes(stats.value.total_bytes) : '—',
    sub: ''
  },
  {
    key: 'trashed',
    label: '回收站',
    icon: TrashOutline,
    color: 'var(--color-danger)',
    value: stats.value ? `${stats.value.trashed} 个` : '—',
    sub: stats.value ? formatBytes(stats.value.trashed_bytes) : ''
  }
])

onMounted(load)
</script>

<template>
  <div class="workbench-page">
    <!-- 指标卡：移动端两列，桌面四列 -->
    <div class="metric-grid">
      <div v-for="m in metrics" :key="m.key" class="metric-card">
        <n-statistic :label="m.label">
          <div class="metric-value">
            <n-icon :size="20" :color="m.color"><component :is="m.icon" /></n-icon>
            <span>{{ m.value }}</span>
          </div>
          <span v-if="m.sub" class="metric-sub">{{ m.sub }}</span>
        </n-statistic>
      </div>
    </div>

    <n-card class="card-surface" :bordered="false" :loading="loading">
      <div class="section-head">
        <div class="section-head__main">
          <div class="section-head__title">到期提醒</div>
        </div>
      </div>
      <n-alert v-if="stats && stats.expiring_7d > 0" type="warning" :show-icon="false">
        <n-icon :size="16" class="alert-icon"><alert-circle-outline /></n-icon>
        7 天内有 {{ stats.expiring_7d }} 个文件到期
      </n-alert>
      <div v-else class="all-clear">
        <n-icon color="var(--color-success)" :size="18"><document-outline /></n-icon>
        近 7 天没有文件到期
      </div>
    </n-card>
  </div>
</template>

<style scoped>
.workbench-page {
  display: grid;
  /* minmax(0,1fr)：grid 子项默认 min-width:auto，会被内容撑宽 */
  grid-template-columns: minmax(0, 1fr);
  min-width: 0;
  gap: var(--space-md);
}

.metric-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.metric-card {
  padding: 16px 18px;
  border: 1px solid var(--color-border-subtle);
  border-radius: var(--radius-card);
  background: var(--color-surface);
  box-shadow: var(--shadow-card);
}

.metric-value {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--color-text-strong);
  font-size: 24px;
  font-weight: 700;
  line-height: 1.2;
}

.metric-sub {
  color: var(--color-text-muted);
  font-size: 12px;
}

.alert-icon {
  margin-right: 6px;
  vertical-align: -3px;
}

.all-clear {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--color-text-muted);
  font-size: 14px;
}

/* 移动端：两列，窄屏不再挤压数字 */
@media (max-width: 768px) {
  .metric-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .metric-value {
    font-size: 20px;
  }
}
</style>
