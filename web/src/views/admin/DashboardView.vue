<script setup lang="ts">
// 统计看板：用户数、文件数、占用、到期提醒与存储一致性扫描。
import { onMounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NGrid,
  NGridItem,
  NIcon,
  NList,
  NListItem,
  NSpace,
  NStatistic,
  NTag,
  NText,
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

const message = useMessage()
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
    if (total === 0) message.success('存储一致性检查通过：数据库与磁盘完全对应')
    else message.warning(`发现 ${total} 处不一致，请查看下方明细`)
  } catch (e) {
    message.error(errMsg(e))
  } finally {
    scanning.value = false
  }
}

const logColumns: DataTableColumns<LogEntry> = [
  { title: '时间', key: 'created_at', width: 160, render: (r) => formatTime(r.created_at, true) },
  { title: '操作', key: 'action', width: 110, render: (r) => actionLabels[r.action] || r.action },
  { title: '操作人', key: 'employee_no', width: 110, render: (r) => r.employee_no || '系统' },
  { title: '详情', key: 'detail', ellipsis: { tooltip: true } },
  { title: 'IP', key: 'ip', width: 130 }
]

onMounted(load)
</script>

<template>
  <n-space vertical :size="14">
    <n-grid :cols="4" :x-gap="12" :y-gap="12" responsive="screen" item-responsive>
      <n-grid-item span="4 s:2 m:1">
        <n-card :bordered="false" size="small" style="border-radius: 8px">
          <n-statistic label="账号总数">
            <n-space align="center" :size="6">
              <n-icon color="#1f6feb" size="20"><people-outline /></n-icon>
              <span>{{ stats?.users ?? '—' }}</span>
            </n-space>
          </n-statistic>
        </n-card>
      </n-grid-item>
      <n-grid-item span="4 s:2 m:1">
        <n-card :bordered="false" size="small" style="border-radius: 8px">
          <n-statistic label="有效文件数">
            <n-space align="center" :size="6">
              <n-icon color="#18a058" size="20"><document-outline /></n-icon>
              <span>{{ stats?.files ?? '—' }}</span>
            </n-space>
          </n-statistic>
        </n-card>
      </n-grid-item>
      <n-grid-item span="4 s:2 m:1">
        <n-card :bordered="false" size="small" style="border-radius: 8px">
          <n-statistic label="占用空间">
            <n-space align="center" :size="6">
              <n-icon color="#f0a020" size="20"><server-outline /></n-icon>
              <span>{{ stats ? formatBytes(stats.total_bytes) : '—' }}</span>
            </n-space>
          </n-statistic>
        </n-card>
      </n-grid-item>
      <n-grid-item span="4 s:2 m:1">
        <n-card :bordered="false" size="small" style="border-radius: 8px">
          <n-statistic label="回收站占用">
            <n-space align="center" :size="6">
              <n-icon color="#d03050" size="20"><trash-outline /></n-icon>
              <span>{{ stats ? `${stats.trashed} 个 · ${formatBytes(stats.trashed_bytes)}` : '—' }}</span>
            </n-space>
          </n-statistic>
        </n-card>
      </n-grid-item>
    </n-grid>

    <n-alert v-if="stats && stats.expiring_7d > 0" type="warning" :show-icon="false">
      <n-space align="center" :size="6">
        <n-icon><alert-circle-outline /></n-icon>
        <span>
          未来 7 天内有 <b>{{ stats.expiring_7d }}</b> 个文件到期，到期后将自动删除（回收站保留一段时间后彻底清理）。
        </span>
      </n-space>
    </n-alert>
    <n-alert v-if="stats && stats.expired_not_purged > 0" type="info" :show-icon="false">
      有 {{ stats.expired_not_purged }} 个文件已到彻底删除时间，等待清理任务执行（最多 10 分钟一次）。
    </n-alert>

    <n-card :bordered="false" size="small" style="border-radius: 8px">
      <template #header>
        <n-space align="center" justify="space-between" style="width: 100%">
          <n-space align="center" :size="8">
            <n-icon><cloud-done-outline /></n-icon>
            <n-text strong>存储一致性检查</n-text>
            <n-tag size="small" :bordered="false">数据库 ↔ 磁盘</n-tag>
          </n-space>
          <n-button size="small" type="primary" :loading="scanning" @click="scan">开始扫描</n-button>
        </n-space>
      </template>
      <n-text depth="3" style="font-size: 12px">
        检查数据库中记录的文件是否真实存在（缺失）、磁盘上是否存在没有数据库记录的文件（孤儿）、以及非法相对路径。
        扫描为只读操作；孤儿文件由每日任务自动归档到 tmp/orphans 并保留 7 天。
      </n-text>
      <template v-if="report">
        <n-space :size="10" style="margin-top: 10px">
          <n-tag :type="report.orphans.length ? 'warning' : 'success'" :bordered="false">
            孤儿文件 {{ report.orphans.length }}
          </n-tag>
          <n-tag :type="report.missing.length ? 'error' : 'success'" :bordered="false">
            缺失文件 {{ report.missing.length }}
          </n-tag>
          <n-tag :type="report.invalid.length ? 'error' : 'success'" :bordered="false">
            非法路径 {{ report.invalid.length }}
          </n-tag>
          <n-text depth="3" style="font-size: 12px; line-height: 30px">
            扫描时间 {{ formatTime(report.scanned_at, true) }} · 磁盘文件总数 {{ report.total_files_on_disk }}
          </n-text>
        </n-space>
        <n-list v-if="report.orphans.length || report.missing.length || report.invalid.length" bordered style="margin-top: 10px; max-height: 300px; overflow: auto">
          <n-list-item v-for="p in report.orphans.slice(0, 100)" :key="'o' + p">
            <n-thing :title="p">
              <template #description><n-tag size="tiny" type="warning" :bordered="false">孤儿文件</n-tag></template>
            </n-thing>
          </n-list-item>
          <n-list-item v-for="p in report.missing.slice(0, 100)" :key="'m' + p">
            <n-thing :title="p">
              <template #description><n-tag size="tiny" type="error" :bordered="false">数据库有记录但磁盘缺失</n-tag></template>
            </n-thing>
          </n-list-item>
          <n-list-item v-for="p in report.invalid.slice(0, 100)" :key="'i' + p">
            <n-thing :title="p">
              <template #description><n-tag size="tiny" type="error" :bordered="false">非法相对路径</n-tag></template>
            </n-thing>
          </n-list-item>
        </n-list>
      </template>
    </n-card>

    <n-card :bordered="false" size="small" style="border-radius: 8px">
      <template #header>
        <n-space align="center" justify="space-between" style="width: 100%">
          <n-text strong>最近操作</n-text>
          <n-button size="small" quaternary @click="load">刷新</n-button>
        </n-space>
      </template>
      <n-data-table :columns="logColumns" :data="logs" :loading="loading" size="small" :bordered="false" />
    </n-card>
  </n-space>
</template>
