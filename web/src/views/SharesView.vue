<script setup lang="ts">
// 分享管理：**所有人都能看到所有人创建的分享链接**（需求明确要求），
// 因此默认列出全部，并显示是谁分享的；打开「只看我的」可筛出自己创建的。
// 撤销仅限自己创建的（管理员可撤销任意，由服务端判定）。
import { computed, h, onMounted, ref, watch } from 'vue'
import {
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NIcon,
  NPagination,
  NRadioButton,
  NRadioGroup,
  NTag,
  useDialog,
  useMessage,
  type DataTableColumns
} from 'naive-ui'
import {
  CheckmarkCircleOutline,
  CopyOutline,
  DocumentOutline,
  FolderOpenOutline,
  LinkOutline,
  TrashOutline,
  WarningOutline
} from '@vicons/ionicons5'
import { errMsg, listShares, revokeShare } from '@/api'
import type { Share } from '@/api/types'
import { formatBytes, formatTime } from '@/utils/format'
import { useIsMobile } from '@/utils/themeState'

const message = useMessage()
const dialog = useDialog()
const isMobile = useIsMobile()

const items = ref<Share[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
/** 'all' = 所有人的分享（默认）；'mine' = 只看我的。 */
const scope = ref<'all' | 'mine'>('all')

async function load() {
  loading.value = true
  try {
    const res = await listShares({
      mine: scope.value === 'mine',
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

/**
 * 分享链接的完整地址。
 *
 * 本项目用的是 history 路由（createWebHistory），所以**不能**拼 #/。
 * 分享页挂在站点根路径下，直接用 origin + /s/<token> 即可。
 */
function shareUrl(s: Share): string {
  return `${window.location.origin}/s/${s.token}`
}

function copy(s: Share) {
  navigator.clipboard
    ?.writeText(shareUrl(s))
    .then(() => message.success('链接已复制，可直接发给别人'))
    .catch(() => message.error('复制失败，请手动选择文本复制'))
}

/** 状态标签：永久 / 剩余天数 / 已过期；目标被删另有一枚标签。 */
function expireLabel(s: Share): string {
  if (!s.expires_at) return '永久'
  if (s.expired) return '已过期'
  const left = new Date(s.expires_at).getTime() - Date.now()
  const days = Math.max(1, Math.ceil(left / 86400000))
  return `剩 ${days} 天`
}

function expireType(s: Share): 'success' | 'warning' | 'error' | 'default' {
  if (!s.expires_at) return 'success'
  if (s.expired) return 'error'
  const left = new Date(s.expires_at).getTime() - Date.now()
  return left < 86400000 * 3 ? 'warning' : 'default'
}

function confirmRevoke(s: Share) {
  dialog.warning({
    title: '撤销分享',
    content: `撤销后「${s.target_name}」的这个链接会立即失效，已发出去的链接都无法再打开。`,
    positiveText: '撤销',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await revokeShare(s.id)
        message.success('已撤销')
        load()
      } catch (e) {
        message.error(errMsg(e))
      }
    }
  })
}

const columns = computed<DataTableColumns<Share>>(() => [
  {
    title: '内容',
    key: 'target_name',
    minWidth: 180,
    render: (s) =>
      h('div', { class: 'cell-target' }, [
        h(NIcon, { size: 16, color: 'var(--color-primary)' }, {
          default: () => h(s.target_type === 'folder' ? FolderOpenOutline : DocumentOutline)
        }),
        h('span', { class: 'cell-target__name', title: s.target_name }, s.target_name),
        s.target_deleted
          ? h(NTag, { size: 'tiny', type: 'error', bordered: false }, { default: () => '已删除' })
          : null
      ])
  },
  {
    title: '分享者',
    key: 'owner_name',
    width: 120,
    render: (s) => s.owner_name || s.owner_employee_no || '—'
  },
  { title: '有效期', key: 'expire', width: 96, render: (s) => h(
      NTag,
      { size: 'small', type: expireType(s), bordered: false },
      { default: () => expireLabel(s) }
    ) },
  ...(isMobile.value
    ? []
    : [
        { title: '大小', key: 'size', width: 100, render: (s: Share) => formatBytes(s.target_size_bytes) },
        { title: '查看', key: 'view_count', width: 76, render: (s: Share) => `${s.view_count} 次` },
        { title: '创建时间', key: 'created_at', width: 150, render: (s: Share) => formatTime(s.created_at, true) }
      ]),
  {
    title: '操作',
    key: 'actions',
    width: 150,
    render: (s) =>
      h('div', { class: 'cell-actions' }, [
        h(
          NButton,
          { size: 'small', quaternary: true, type: 'primary', onClick: () => copy(s) },
          { icon: () => h(NIcon, null, { default: () => h(CopyOutline) }), default: () => '复制' }
        ),
        h(
          NButton,
          {
            size: 'small',
            quaternary: true,
            type: 'error',
            onClick: () => confirmRevoke(s)
          },
          { icon: () => h(NIcon, null, { default: () => h(TrashOutline) }), default: () => '撤销' }
        )
      ])
  }
])

// 移动端用卡片列表，共用同一份状态判断（避免两套逻辑各写一遍）。
function statusOf(s: Share) {
  if (s.target_deleted) {
    return { text: '内容已删除', type: 'error' as const, icon: WarningOutline }
  }
  if (s.expired) return { text: '已过期', type: 'error' as const, icon: WarningOutline }
  return { text: expireLabel(s), type: 'success' as const, icon: CheckmarkCircleOutline }
}

watch(scope, () => {
  page.value = 1
  load()
})
watch([page, pageSize], () => load())

onMounted(load)
</script>

<template>
  <div class="shares-page">
    <n-card class="card-surface" :bordered="false">
      <div class="section-head">
        <n-radio-group v-model:value="scope" size="small">
          <n-radio-button value="all">全部人的分享</n-radio-button>
          <n-radio-button value="mine">只看我的</n-radio-button>
        </n-radio-group>
        <n-button size="small" quaternary :loading="loading" @click="load">刷新</n-button>
      </div>

      <n-empty
        v-if="!loading && !items.length"
        description="还没有人创建分享链接"
        style="padding: 32px 0"
      />

      <!-- 桌面端表格 -->
      <div v-else class="desktop-only">
        <n-data-table
          :columns="columns"
          :data="items"
          :loading="loading"
          :row-key="(s: Share) => s.id"
          :bordered="false"
          size="small"
          :scroll-x="820"
        />
      </div>

      <!-- 移动端卡片 -->
      <ul v-if="items.length" class="mobile-only share-cards">
        <li v-for="s in items" :key="s.id" class="share-card">
          <div class="share-card__head">
            <n-icon :size="16" color="var(--color-primary)">
              <folder-open-outline v-if="s.target_type === 'folder'" />
              <document-outline v-else />
            </n-icon>
            <span class="share-card__name" :title="s.target_name">{{ s.target_name }}</span>
            <n-tag size="tiny" :type="statusOf(s).type" :bordered="false">
              {{ statusOf(s).text }}
            </n-tag>
          </div>
          <div class="share-card__meta">
            {{ s.owner_name || '—' }} 分享
            <template v-if="s.target_size_bytes"> · {{ formatBytes(s.target_size_bytes) }}</template>
            <template v-if="s.view_count"> · 查看 {{ s.view_count }} 次</template>
          </div>
          <div class="share-card__actions">
            <n-button size="small" quaternary type="primary" @click="copy(s)">
              <template #icon><n-icon><link-outline /></n-icon></template>
              复制链接
            </n-button>
            <n-button size="small" quaternary type="error" @click="confirmRevoke(s)">
              <template #icon><n-icon><trash-outline /></n-icon></template>
              撤销
            </n-button>
          </div>
        </li>
      </ul>

      <div v-if="total > pageSize" class="pager">
        <n-pagination
          v-model:page="page"
          v-model:page-size="pageSize"
          :item-count="total"
          :page-sizes="[20, 50]"
          show-size-picker
        />
      </div>
    </n-card>
  </div>
</template>

<style scoped>
.shares-page {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  min-width: 0;
}

.desktop-only,
.mobile-only {
  min-width: 0;
}

.share-cards {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 10px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.share-card {
  padding: 12px 13px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--color-surface);
}

.share-card__head {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.share-card__name {
  min-width: 0;
  overflow: hidden;
  color: var(--color-text-strong);
  font-size: 14px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.share-card__meta {
  margin-top: 6px;
  color: var(--color-text-muted);
  font-size: 12px;
}

.share-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: var(--space-md);
}
</style>
