<script setup lang="ts">
// 审计日志：按动作、关键字与时间范围筛选。
import { h, onMounted, ref } from 'vue'
import {
  NButton,
  NCard,
  NDataTable,
  NIcon,
  NInput,
  NSelect,
  NTag,
  useMessage,
  type DataTableColumns
} from 'naive-ui'
import { RefreshOutline, SearchOutline } from '@vicons/ionicons5'
import { adminListLogs, errMsg } from '@/api'
import type { LogEntry } from '@/api/types'
import { formatTime } from '@/utils/format'
import { useIsMobile } from '@/utils/themeState'

const message = useMessage()
const isMobile = useIsMobile()

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

onMounted(load)
</script>

<template>
  <n-card :bordered="false" size="small" style="border-radius: 8px">
    <div class="section-head" :class="{ 'section-head--stack': isMobile }">
      <div class="section-head__title">
        <n-tag size="small" :bordered="false">{{ total }} 条</n-tag>
      </div>
      <div class="log-filters">
        <n-select
          v-model:value="action"
          :options="actionOptions"
          class="filter-action"
          size="small"
          @update:value="search"
        />
        <n-select v-model:value="days" :options="dayOptions" class="filter-days" size="small" @update:value="search" />
        <n-input
          v-model:value="keyword"
          placeholder="搜索工号或详情"
          clearable
          size="small"
          class="filter-keyword"
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
      </div>
    </div>

    <div class="desktop-only">
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
    </div>

    <!-- 移动端：日志条目列表（宽表格在手机上无法阅读） -->
    <div class="mobile-only">
      <n-empty v-if="!items.length" description="暂无日志" style="padding: 28px 0" />
      <ul v-else class="log-cards">
        <li v-for="r in items" :key="r.id" class="log-card">
          <div class="log-card__top">
            <n-tag size="small" :type="actionType(r.action)" :bordered="false">
              {{ actionLabels[r.action] || r.action }}
            </n-tag>
            <span class="log-card__who">{{ r.employee_no || '系统' }}</span>
            <span class="log-card__spacer" />
            <span class="log-card__time">{{ formatTime(r.created_at, true) }}</span>
          </div>
          <div v-if="r.detail" class="log-card__detail">{{ r.detail }}</div>
          <div v-if="r.ip" class="log-card__ip">{{ r.ip }}</div>
        </li>
      </ul>
      <n-pagination
        v-if="total > pageSize"
        class="mobile-pager"
        :page="page"
        :page-size="pageSize"
        :item-count="total"
        :page-slot="5"
        @update:page="
          (v: number) => {
            page = v
            load()
          }
        "
      />
    </div>
  </n-card>
</template>

<style scoped>
.log-filters {
  display: flex;
  flex: none;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.filter-action {
  width: 170px;
}

.filter-days {
  width: 110px;
}

.filter-keyword {
  width: 200px;
}

.log-cards {
  display: grid;
  /* minmax(0,…) 防止 nowrap 内容把网格列撑宽（grid 子项默认 min-width:auto） */
  grid-template-columns: minmax(0, 1fr);
  min-width: 0;
  gap: 10px;
  margin: 14px 0 0;
  padding: 0;
  list-style: none;
}

.log-card {
  padding: 11px 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--color-surface-soft);
}

.log-card__top {
  display: flex;
  align-items: center;
  gap: 8px;
}

.log-card__who {
  color: var(--color-text);
  font-size: 13px;
  font-weight: 600;
}

.log-card__spacer {
  flex: 1;
}

.log-card__time {
  color: var(--color-text-muted);
  font-size: 12px;
}

.log-card__detail {
  margin-top: 6px;
  color: var(--color-text);
  font-size: 13px;
  word-break: break-all;
}

.log-card__ip {
  margin-top: 2px;
  color: var(--color-text-muted);
  font-size: 12px;
}

.mobile-pager {
  display: flex;
  justify-content: center;
  margin-top: 16px;
}

.mobile-only {
  display: none;
}

@media (max-width: 768px) {
  .desktop-only {
    display: none;
  }

  .mobile-only {
    display: block;
  }

  .section-head--stack {
    align-items: stretch;
    flex-direction: column;
  }

  .log-filters {
    width: 100%;
  }

  .filter-action,
  .filter-days {
    flex: 1;
    width: auto;
  }

  .filter-keyword {
    flex: 1 1 100%;
    width: auto;
  }
}
</style>
