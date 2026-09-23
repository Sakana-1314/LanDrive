<script setup lang="ts">
// 统计看板：关键指标、到期提醒、存储一致性扫描、最近操作。
//
// 文案取舍：不再用整段小字解释扫描在做什么 —— 结果用三个彩色角标直接给出
// （孤儿 / 缺失 / 非法路径），一眼就能判断是否正常。
import { computed, onMounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NIcon,
  NList,
  NListItem,
  NStatistic,
  NTag,
  NThing,
  useMessage,
  type DataTableColumns
} from 'naive-ui'
import {
  AlertCircleOutline,
  CloudDoneOutline,
  DocumentOutline,
  PeopleOutline,
  ServerOutline,
  TrashOutline
} from '@vicons/ionicons5'
import { adminListLogs, adminScanStorage, adminStats, errMsg } from '@/api'
import type { LogEntry, OrphanReport, Stats } from '@/api/types'
import { formatBytes, formatTime } from '@/utils/format'
import { useIsMobile } from '@/utils/themeState'

const message = useMessage()
const isMobile = useIsMobile()

const stats = ref<Stats | null>(null)
const logs = ref<LogEntry[]>([])
const scanning = ref(false)
const report = ref<OrphanReport | null>(null)
const loading = ref(false)

const actionLabels: Record<string, string> = {
  login: '登录',
  login_failed: '登录失败',
  upload: '上传',
  rename: '重命名',
  delete: '删除',
  restore: '恢复',
  purge: '彻底删除',
  download: '下载',
  user_create: '创建用户',
  user_update: '更新用户',
  user_delete: '删除用户',
  password_change: '修改密码',
  password_reset: '重置密码',
  settings_update: '修改配置',
  maintain_expire: '到期标记',
  maintain_purge: '到期清理',
  maintain_orphan: '孤儿归档',
  storage_scan: '一致性扫描'
}

async function load() {
  loading.value = true
  try {
    const [s, l] = await Promise.all([adminStats(), adminListLogs({ page: 1, page_size: 12 })])
    stats.value = s
    logs.value = l.items
  } catch (e) {
    message.error(errMsg(e))
  } finally {
    loading.value = false
  }
}

async function scan() {
  scanning.value = true
  try {
    report.value = await adminScanStorage()
    const total = report.value.orphans.length + report.value.missing.length + report.value.invalid.length
    if (total === 0) message.success('数据库与磁盘完全一致')
    else message.warning(`发现 ${total} 处不一致`)
  } catch (e) {
    message.error(errMsg(e))
  } finally {
    scanning.value = false
  }
}

/** 扫描结果汇总：三项全 0 即健康。 */
const reportTotal = computed(() => {
  if (!report.value) return 0
  return report.value.orphans.length + report.value.missing.length + report.value.invalid.length
})

const metrics = computed(() => [
  { key: 'users', label: '账号', icon: PeopleOutline, color: 'var(--color-primary)', value: stats.value?.users ?? '—', sub: '' },
  { key: 'files', label: '有效文件', icon: DocumentOutline, color: 'var(--color-success)', value: stats.value?.files ?? '—', sub: '' },
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

const logColumns = computed<DataTableColumns<LogEntry>>(() => {
  const cols: DataTableColumns<LogEntry> = [
    { title: '时间', key: 'created_at', width: 160, render: (r) => formatTime(r.created_at, true) },
    { title: '操作', key: 'action', width: 100, render: (r) => actionLabels[r.action] || r.action },
    { title: '操作人', key: 'employee_no', width: 100, render: (r) => r.employee_no || '系统' }
  ]
  if (!isMobile.value) {
    cols.push({ title: '详情', key: 'detail', ellipsis: { tooltip: true } })
    cols.push({ title: 'IP', key: 'ip', width: 130 })
  }
  return cols
})

onMounted(load)
</script>

<template>
  <div class="dash-page">
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

    <n-alert v-if="stats && stats.expiring_7d > 0" type="warning" :show-icon="false">
      <n-icon :size="16" class="alert-icon"><alert-circle-outline /></n-icon>
      7 天内有 {{ stats.expiring_7d }} 个文件到期
    </n-alert>

    <n-card class="card-surface" :bordered="false">
      <div class="section-head">
        <div class="section-head__title">
          <n-icon color="var(--color-primary)" :size="18"><cloud-done-outline /></n-icon>
          存储一致性
        </div>
        <n-button size="small" type="primary" :loading="scanning" @click="scan">扫描</n-button>
      </div>

      <div v-if="report" class="scan-result">
        <n-tag :type="reportTotal === 0 ? 'success' : 'warning'" :bordered="false" size="small">
          {{ reportTotal === 0 ? '数据库与磁盘一致' : `发现 ${reportTotal} 处差异` }}
        </n-tag>
        <n-tag :type="report.orphans.length ? 'warning' : 'success'" :bordered="false" size="small">
          孤儿 {{ report.orphans.length }}
        </n-tag>
        <n-tag :type="report.missing.length ? 'error' : 'success'" :bordered="false" size="small">
          缺失 {{ report.missing.length }}
        </n-tag>
        <n-tag :type="report.invalid.length ? 'error' : 'success'" :bordered="false" size="small">
          非法路径 {{ report.invalid.length }}
        </n-tag>
        <span class="scan-time">{{ formatTime(report.scanned_at, true) }}</span>
      </div>

      <n-list v-if="report && reportTotal > 0" bordered class="scan-list">
        <n-list-item v-for="p in report.orphans.slice(0, 50)" :key="'o' + p">
          <n-thing :title="p">
            <template #description>
              <n-tag size="small" type="warning" :bordered="false">孤儿文件</n-tag>
            </template>
          </n-thing>
        </n-list-item>
        <n-list-item v-for="p in report.missing.slice(0, 50)" :key="'m' + p">
          <n-thing :title="p">
            <template #description>
              <n-tag size="small" type="error" :bordered="false">磁盘缺失</n-tag>
            </template>
          </n-thing>
        </n-list-item>
        <n-list-item v-for="p in report.invalid.slice(0, 50)" :key="'i' + p">
          <n-thing :title="p">
            <template #description>
              <n-tag size="small" type="error" :bordered="false">非法路径</n-tag>
            </template>
          </n-thing>
        </n-list-item>
      </n-list>
    </n-card>

    <n-card class="card-surface" :bordered="false">
      <div class="section-head">
        <div class="section-head__title">最近操作</div>
        <n-button size="small" quaternary :loading="loading" @click="load">刷新</n-button>
      </div>
      <n-empty v-if="!logs.length" description="暂无操作记录" style="padding: 24px 0" />
      <n-data-table
        v-else
        class="log-table"
        :columns="logColumns"
        :data="logs"
        :loading="loading"
        size="small"
        :bordered="false"
      />
    </n-card>
  </div>
</template>

<style scoped>
.dash-page {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 14px;
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
  font-size: 22px;
  font-weight: 650;
  line-height: 1.3;
}

.metric-sub {
  display: block;
  color: var(--color-text-muted);
  font-size: 12px;
}

.alert-icon {
  margin-right: 6px;
  vertical-align: -3px;
}

.scan-result {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-top: 14px;
}

.scan-time {
  color: var(--color-text-muted);
  font-size: 12px;
}

.scan-list {
  margin-top: 12px;
  max-height: 280px;
  overflow: auto;
}

.log-table {
  margin-top: 12px;
}

/* 移动端：指标两列，避免单列太长 */
@media (max-width: 768px) {
  .metric-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .metric-card {
    padding: 13px 14px;
  }

  .metric-value {
    font-size: 19px;
  }
}
</style>
