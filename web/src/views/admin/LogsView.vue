<script setup lang="ts">
// 审计日志：按动作、关键字与时间范围筛选。
import { onMounted, ref } from 'vue'
import {
  NButton,
  NCard,
  NDataTable,
  NIcon,
  NInput,
  NSelect,
  NSpace,
  NTag,
  NText,
  useMessage,
  type DataTableColumns
} from 'naive-ui'
import { RefreshOutline, SearchOutline } from '@vicons/ionicons5'
import { adminListLogs, errMsg } from '@/api'
import type { LogEntry } from '@/api/types'
import { formatTime } from '@/utils/format'

const message = useMessage()

const items = ref<LogEntry[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(30)
const keyword = ref('')
const action = ref<string | null>(null)
const days = ref(7)
const loading = ref(false)

const actionOptions = [
  { label: '全部操作', value: '' },
  { label: '登录', value: 'login' },
  { label: '登录失败', value: 'login_failed' },
  { label: '上传', value: 'upload' },
  { label: '下载', value: 'download' },
  { label: '重命名', value: 'rename' },
  { label: '删除（进回收站）', value: 'delete' },
  { label: '恢复', value: 'restore' },
  { label: '彻底删除', value: 'purge' },
  { label: '创建用户', value: 'user_create' },
  { label: '更新用户', value: 'user_update' },
  { label: '删除用户', value: 'user_delete' },
  { label: '修改密码', value: 'password_change' },
  { label: '重置密码', value: 'password_reset' },
  { label: '修改配置', value: 'settings_update' },
  { label: '到期标记', value: 'maintain_expire' },
  { label: '到期清理', value: 'maintain_purge' },
  { label: '孤儿归档', value: 'maintain_orphan' },
  { label: '一致性扫描', value: 'storage_scan' }
]

const dayOptions = [
  { label: '今天', value: 1 },
  { label: '近 7 天', value: 7 },
  { label: '近 30 天', value: 30 },
  { label: '近 90 天', value: 90 },
  { label: '全部', value: 0 }
]

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

function actionType(a: string): 'default' | 'info' | 'success' | 'warning' | 'error' {
  if (a === 'login_failed') return 'error'
  if (a.startsWith('maintain')) return 'warning'
  if (a === 'purge' || a === 'user_delete') return 'error'
  if (a === 'upload' || a === 'login') return 'success'
  return 'default'
}

async function load() {
  loading.value = true
  try {
    const res = await adminListLogs({
      action: action.value || undefined,
      q: keyword.value.trim() || undefined,
      days: days.value || undefined,
      page: page.value,
      page_size: pageSize.value
    })
    items.value = res.items
    total.value = res.total
  } catch (e) {
    message.error(errMsg(e))
  } finally {
    loading.value = false
  }
}

function search() {
  page.value = 1
  load()
}

const columns: DataTableColumns<LogEntry> = [
  { title: '时间', key: 'created_at', width: 170, render: (r) => formatTime(r.created_at, true) },
  {
    title: '操作',
    key: 'action',
    width: 120,
    render: (r) =>
      h(NTag, { size: 'small', type: actionType(r.action), bordered: false }, {
        default: () => actionLabels[r.action] || r.action
      })
  },
  { title: '操作人', key: 'employee_no', width: 120, render: (r) => r.employee_no || '系统' },
  { title: '对象', key: 'target_id', width: 120, render: (r) => r.target_id || '—' },
  { title: '详情', key: 'detail', ellipsis: { tooltip: true } },
  { title: 'IP', key: 'ip', width: 140 }
]

import { h } from 'vue'

onMounted(load)
</script>

<template>
  <n-card :bordered="false" size="small" style="border-radius: 8px">
    <template #header>
      <n-space justify="space-between" align="center" style="width: 100%">
        <n-space align="center" :size="8">
          <n-text strong style="font-size: 16px">审计日志</n-text>
          <n-tag size="small" :bordered="false">共 {{ total }} 条</n-tag>
          <n-text depth="3" style="font-size: 12px">默认保留 90 天，超期自动清理</n-text>
        </n-space>
        <n-space :size="8">
          <n-select
            v-model:value="action"
            :options="actionOptions"
            style="width: 170px"
            size="small"
            @update:value="search"
          />
          <n-select v-model:value="days" :options="dayOptions" style="width: 110px" size="small" @update:value="search" />
          <n-input
            v-model:value="keyword"
            placeholder="搜索工号 / 详情"
            clearable
            size="small"
            style="width: 200px"
            @keyup.enter="search"
          >
            <template #prefix>
              <n-icon><search-outline /></n-icon>
            </template>
          </n-input>
          <n-button size="small" @click="search">
            <template #icon>
              <n-icon><refresh-outline /></n-icon>
            </template>
            查询
          </n-button>
        </n-space>
      </n-space>
    </template>

    <n-data-table
      :columns="columns"
      :data="items"
      :loading="loading"
      :row-key="(r: LogEntry) => r.id"
      remote
      size="small"
      :scroll-x="1100"
      :pagination="{
        page,
        pageSize,
        itemCount: total,
        showSizePicker: true,
        pageSizes: [20, 30, 50, 100]
      }"
      @update:page="
        (v: number) => {
          page = v
          load()
        }
      "
      @update:page-size="
        (v: number) => {
          pageSize = v
          page = 1
          load()
        }
      "
    />
  </n-card>
</template>
