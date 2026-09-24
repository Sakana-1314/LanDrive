<script setup lang="ts">
// 文件列表：桌面端用表格，移动端用卡片。
//
// 为什么两套而不是让表格横向滚动：手机上左右拖动一张 6 列表格很难用，
// 而文件列表的核心信息（名称、大小、时间、操作）在卡片上更易点。
// 两套视图共用 useFileActions，增删改查逻辑只有一份。
import { computed, h, ref } from 'vue'
import {
  NButton,
  NDataTable,
  NEmpty,
  NIcon,
  NInput,
  NPagination,
  NPopconfirm,
  NSpace,
  NTag,
  type DataTableColumns
} from 'naive-ui'
import {
  CreateOutline,
  DownloadOutline,
  EyeOutline,
  LinkOutline,
  RefreshOutline,
  SearchOutline,
  TrashOutline
} from '@vicons/ionicons5'
import type { FileItem } from '@/api/types'
import { extLabel, formatBytes, formatDaysLeft, formatTime, shorten } from '@/utils/format'
import { extTagType } from '@/utils/theme'
import { useFileActions } from '@/composables/useFileActions'
import { useCreateShare } from '@/composables/useCreateShare'

const props = withDefaults(
  defineProps<{
    items: FileItem[]
    loading?: boolean
    total: number
    page: number
    pageSize: number
    /** 是否显示上传者列 */
    showOwner?: boolean
    /** 是否显示到期列 */
    showExpiry?: boolean
    /** 管理端：显示恢复/彻底删除 */
    adminMode?: boolean
    /** 只读（预览/下载） */
    readonly?: boolean
    /** 关键字（v-model） */
    keyword?: string
    /** 搜索框占位文案 */
    searchPlaceholder?: string
    /** 当前列表归属（选中的用户名 / 全部人员）；为空则不显示 */
    title?: string
  }>(),
  {
    loading: false,
    showOwner: true,
    showExpiry: true,
    adminMode: false,
    readonly: false,
    keyword: '',
    searchPlaceholder: '搜索文件名、姓名或工号',
    title: ''
  }
)

const emit = defineEmits<{
  (e: 'update:page', v: number): void
  (e: 'update:pageSize', v: number): void
  (e: 'update:keyword', v: string): void
  (e: 'refresh'): void
  (e: 'sort', payload: { sort: string; order: 'asc' | 'desc' }): void
}>()

const actions = useFileActions({
  adminMode: props.adminMode,
  onRefresh: () => emit('refresh')
})
const share = useCreateShare()

/**
 * 分享入口只在**自己上传且未删除**的文件上出现。
 * 需求规定"只能分享自己的文件"；管理员也走同一条前端规则，
 * 服务端另有兜底（管理员可分享任意）。
 */
function canShare(row: FileItem): boolean {
  return row.is_mine && row.status === 'active'
}

function onShare(row: FileItem) {
  void share.create('file', row.id, row.original_name)
}

const sortKey = ref('created_at')
const sortOrder = ref<'asc' | 'desc'>('desc')

/** Naive UI 用 ascend/descend，后端契约用 asc/desc，这里做双向转换。 */
type NSortOrder = 'ascend' | 'descend' | false
function toNSort(order: 'asc' | 'desc', key: string, currentKey: string): NSortOrder {
  if (key !== currentKey) return false
  return order === 'asc' ? 'ascend' : 'descend'
}

function onSort(sort: string, order: 'asc' | 'desc') {
  sortKey.value = sort
  sortOrder.value = order
  emit('sort', { sort, order })
}

/** 距到期不足 3 天时用醒目色，无需额外文字解释。 */
function expiryType(days: number): 'error' | 'warning' | 'default' {
  if (days <= 3) return 'error'
  if (days <= 7) return 'warning'
  return 'default'
}

const columns = computed<DataTableColumns<FileItem>>(() => {
  const cols: DataTableColumns<FileItem> = [
    {
      title: '文件名',
      key: 'original_name',
      sorter: true,
      sortOrder: toNSort(sortOrder.value, 'original_name', sortKey.value),
      minWidth: 260,
      render(row) {
        return h(NSpace, { align: 'center', size: 8, wrap: false }, {
          default: () => [
            h(NTag, { size: 'small', type: extTagType(row.ext), bordered: false }, {
              default: () => extLabel(row.ext)
            }),
            h(
              'a',
              {
                class: 'file-name',
                title: row.original_name,
                onClick: () => actions.openPreview(row)
              },
              shorten(row.original_name, 44)
            )
          ]
        })
      }
    }
  ]

  if (props.showOwner) {
    cols.push({
      title: '上传者',
      key: 'owner',
      width: 130,
      render: (row) => row.owner_name
    })
  }

  cols.push(
    {
      title: '大小',
      key: 'size_bytes',
      width: 96,
      sorter: true,
      sortOrder: toNSort(sortOrder.value, 'size_bytes', sortKey.value),
      render: (row) => formatBytes(row.size_bytes)
    },
    {
      title: '上传时间',
      key: 'created_at',
      width: 150,
      sorter: true,
      sortOrder: toNSort(sortOrder.value, 'created_at', sortKey.value),
      render: (row) => formatTime(row.created_at)
    }
  )

  if (props.showExpiry && !props.adminMode) {
    cols.push({
      title: '到期',
      key: 'expires_at',
      width: 150,
      sorter: true,
      sortOrder: toNSort(sortOrder.value, 'expires_at', sortKey.value),
      render(row) {
        return h(
          NTag,
          { size: 'small', type: expiryType(row.days_left), bordered: false },
          { default: () => formatDaysLeft(row.days_left) }
        )
      }
    })
  }

  if (props.adminMode) {
    cols.push({
      title: '删除时间',
      key: 'deleted_at',
      width: 150,
      render: (row) => (row.deleted_at ? formatTime(row.deleted_at) : '—')
    })
    cols.push({
      title: '彻底删除',
      key: 'purge_at',
      width: 150,
      render: (row) => (row.purge_at ? formatTime(row.purge_at) : '—')
    })
  }

  cols.push({
    title: '操作',
    key: 'actions',
    width: props.adminMode ? 210 : props.readonly ? 120 : 215,
    fixed: 'right',
    render(row) {
      const buttons = [
        h(NButton, { size: 'small', quaternary: true, onClick: () => actions.openPreview(row) }, {
          icon: () => h(NIcon, null, { default: () => h(EyeOutline) }),
          default: () => '预览'
        }),
        h(NButton, { size: 'small', quaternary: true, onClick: () => actions.onDownload(row) }, {
          icon: () => h(NIcon, null, { default: () => h(DownloadOutline) }),
          default: () => '下载'
        })
      ]

      if (canShare(row)) {
        buttons.push(
          h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => onShare(row) }, {
            icon: () => h(NIcon, null, { default: () => h(LinkOutline) }),
            default: () => '分享'
          })
        )
      }

      if (props.adminMode) {
        if (row.status === 'trashed') {
          buttons.push(
            h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => actions.onRestore(row) }, {
              icon: () => h(NIcon, null, { default: () => h(RefreshOutline) }),
              default: () => '恢复'
            }),
            h(
              NPopconfirm,
              { onPositiveClick: () => actions.confirmPurge(row) },
              {
                trigger: () =>
                  h(NButton, { size: 'small', quaternary: true, type: 'error' }, {
                    icon: () => h(NIcon, null, { default: () => h(TrashOutline) }),
                    default: () => '彻底删除'
                  }),
                default: () => `永久删除「${row.original_name}」？`
              }
            )
          )
        } else {
          buttons.push(
            h(
              NPopconfirm,
              { onPositiveClick: () => actions.confirmDelete(row) },
              {
                trigger: () =>
                  h(NButton, { size: 'small', quaternary: true, type: 'error' }, {
                    icon: () => h(NIcon, null, { default: () => h(TrashOutline) }),
                    default: () => '删除'
                  }),
                default: () => `删除「${row.original_name}」？`
              }
            )
          )
        }
      } else if (!props.readonly && row.can_edit) {
        buttons.push(
          h(NButton, { size: 'small', quaternary: true, onClick: () => actions.onRename(row) }, {
            icon: () => h(NIcon, null, { default: () => h(CreateOutline) }),
            default: () => '重命名'
          }),
          h(
            NPopconfirm,
            { onPositiveClick: () => actions.confirmDelete(row) },
            {
              trigger: () =>
                h(NButton, { size: 'small', quaternary: true, type: 'error' }, {
                  icon: () => h(NIcon, null, { default: () => h(TrashOutline) }),
                  default: () => '删除'
                }),
              default: () => `删除「${row.original_name}」？`
            }
          )
        )
      }
      return h(NSpace, { size: 2, wrap: false }, { default: () => buttons })
    }
  })

  return cols
})

const pagination = computed(() => ({
  page: props.page,
  pageSize: props.pageSize,
  itemCount: props.total,
  showSizePicker: true,
  pageSizes: [10, 20, 50, 100],
  prefix: (info: { itemCount?: number }) => `共 ${info.itemCount ?? 0} 个`
}))
</script>

<template>
  <div class="file-list">
    <!-- 当前归属：从菜单子 tab 进来时，顶栏只显示"全部文件"，
         这里补上具体是谁，否则用户不知道自己在看谁的目录 -->
    <div v-if="title" class="list-title">{{ title }}</div>

    <!-- 工具栏：搜索 + 刷新。移动端自动折成两行（搜索占满一行）。 -->
    <div class="toolbar">
      <n-input
        :value="keyword"
        :placeholder="searchPlaceholder"
        clearable
        @update:value="(v: string) => emit('update:keyword', v)"
        @keyup.enter="emit('refresh')"
      >
        <template #prefix>
          <n-icon><search-outline /></n-icon>
        </template>
      </n-input>
      <div class="toolbar-actions">
        <n-button :loading="loading" @click="emit('refresh')">
          <template #icon>
            <n-icon><refresh-outline /></n-icon>
          </template>
          刷新
        </n-button>
        <slot name="toolbar" />
      </div>
    </div>

    <!-- 桌面端表格（≥769px 由 CSS 控制显隐，避免依赖 JS 断点造成首帧跳动） -->
    <div class="desktop-only">
      <n-data-table
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: FileItem) => row.id"
        remote
        size="small"
        :scroll-x="props.showOwner ? 1180 : 1050"
        @update:page="(v: number) => emit('update:page', v)"
        @update:page-size="(v: number) => emit('update:pageSize', v)"
        @update:sorter="
          (s: any) => {
            // Naive UI 传出 ascend / descend，转换为后端契约的 asc / desc。
            if (s && s.order) onSort(String(s.columnKey), s.order === 'ascend' ? 'asc' : 'desc')
          }
        "
      >
        <template #empty>
          <n-empty description="暂无文件" style="padding: 32px 0" />
        </template>
      </n-data-table>
    </div>

    <!-- 移动端卡片列表（≤768px） -->
    <div class="mobile-only">
      <n-empty v-if="!items.length && !loading" description="暂无文件" style="padding: 32px 0" />
      <ul v-else class="card-list">
        <li v-for="row in items" :key="row.id" class="file-card">
          <div class="file-card__head" @click="actions.openPreview(row)">
            <n-tag size="small" :type="extTagType(row.ext)" :bordered="false">
              {{ extLabel(row.ext) }}
            </n-tag>
            <span class="file-card__name">{{ row.original_name }}</span>
          </div>

          <div class="file-card__meta">
            <span>{{ formatBytes(row.size_bytes) }}</span>
            <span v-if="props.showOwner">{{ row.owner_name }}</span>
            <span>{{ formatTime(row.created_at) }}</span>
          </div>

          <div class="file-card__foot">
            <n-tag
              v-if="props.showExpiry && !props.adminMode"
              size="small"
              :type="expiryType(row.days_left)"
              :bordered="false"
            >
              {{ formatDaysLeft(row.days_left) }}
            </n-tag>
            <n-tag v-else-if="props.adminMode && row.status === 'trashed'" size="small" type="error" :bordered="false">
              待清理
            </n-tag>
            <n-tag v-if="props.adminMode && row.purge_at" size="small" :bordered="false">
              {{ formatTime(row.purge_at) }} 清理
            </n-tag>
            <span class="file-card__spacer" />
            <n-button size="small" quaternary @click="actions.openPreview(row)">
              <template #icon>
                <n-icon><eye-outline /></n-icon>
              </template>
            </n-button>
            <n-button size="small" quaternary @click="actions.onDownload(row)">
              <template #icon>
                <n-icon><download-outline /></n-icon>
              </template>
            </n-button>
            <template v-if="props.adminMode">
              <n-button
                v-if="row.status === 'trashed'"
                size="small"
                quaternary
                type="primary"
                @click="actions.onRestore(row)"
              >
                恢复
              </n-button>
              <n-button
                v-if="row.status === 'trashed'"
                size="small"
                quaternary
                type="error"
                @click="actions.confirmPurge(row)"
              >
                彻底删除
              </n-button>
              <n-button v-else size="small" quaternary type="error" @click="actions.confirmDelete(row)">
                删除
              </n-button>
            </template>
            <template v-else-if="!props.readonly && row.can_edit">
              <n-button size="small" quaternary @click="actions.onRename(row)">
                <template #icon>
                  <n-icon><create-outline /></n-icon>
                </template>
              </n-button>
              <n-button size="small" quaternary type="error" @click="actions.confirmDelete(row)">
                <template #icon>
                  <n-icon><trash-outline /></n-icon>
                </template>
              </n-button>
            </template>
          </div>
        </li>
      </ul>

      <n-pagination
        v-if="total > pageSize"
        class="mobile-pager"
        :page="page"
        :page-size="pageSize"
        :item-count="total"
        :page-slot="5"
        @update:page="(v: number) => emit('update:page', v)"
      />
    </div>
  </div>
</template>

<style scoped>
.list-title {
  margin-bottom: var(--space-md);
  color: var(--color-text-strong);
  font-size: 16px;
  font-weight: 650;
}

.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}

.toolbar-actions {
  display: flex;
  flex: none;
  align-items: center;
  gap: 8px;
}

/* 文件名做成链接样式，表意「可点击预览」，不需要额外提示文字 */
.file-list :deep(.file-name) {
  color: var(--color-primary);
  cursor: pointer;
}

.file-list :deep(.file-name:hover) {
  text-decoration: underline;
}

/* ---------- 移动端卡片 ---------- */
.card-list {
  display: grid;
  /* minmax(0,…) 防止 nowrap 内容把网格列撑宽（grid 子项默认 min-width:auto） */
  grid-template-columns: minmax(0, 1fr);
  min-width: 0;
  gap: 10px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.file-card {
  padding: 12px 13px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--color-surface-soft);
}

.file-card__head {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
  cursor: pointer;
}

.file-card__name {
  min-width: 0;
  overflow: hidden;
  color: var(--color-text-strong);
  font-size: 14px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-card__meta {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  gap: 4px 12px;
  margin-top: 6px;
  color: var(--color-text-muted);
  font-size: 12px;
}

.file-card__foot {
  display: flex;
  /* 允许换行：操作键较多（管理端 4 个）时在窄屏排到下一行，
   * 不换行会把卡片撑得比屏幕还宽。 */
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
  margin-top: 10px;
}

.file-card__spacer {
  flex: 1;
}

.mobile-pager {
  display: flex;
  justify-content: center;
  margin-top: 16px;
}

/* ---------- 视图切换 ----------
 * 用 CSS 而非 JS 断点控制显隐：首帧就正确，不会先渲染错的那套再纠正。 */
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

  /* 移动端搜索占一整行，按钮降到下一行 */
  .toolbar {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
  }

  .toolbar-actions {
    justify-content: flex-end;
  }
}
</style>
