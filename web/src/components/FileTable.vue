<script setup lang="ts">
// 文件列表：桌面端用表格，移动端用卡片。
//
// 为什么两套而不是让表格横向滚动：手机上左右拖动一张 6 列表格很难用，
// 而文件列表的核心信息（名称、大小、时间、操作）在卡片上更易点。
// 两套视图共用 useFileActions，增删改查逻辑只有一份。
//
// **文件夹与文件在同一个列表里**（Windows 资源管理器的行为）：此前子文件夹是
// 表格上方单独的一坨卡片，同一个目录的两类东西被拆在两处，用户得上下扫两遍。
// 现在由 utils/fileRows.ts 合成行（文件夹在前），表格与移动端卡片都渲染同一份行。
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
  ChevronForwardOutline,
  CreateOutline,
  DownloadOutline,
  EyeOutline,
  FolderOpenOutline,
  LinkOutline,
  RefreshOutline,
  SearchOutline,
  TrashOutline
} from '@vicons/ionicons5'
import type { FileItem, Folder } from '@/api/types'
import { extLabel, formatBytes, formatDaysLeft, formatTime, shorten } from '@/utils/format'
import { extTagType } from '@/utils/theme'
import {
  buildRows,
  filterFolders,
  folderItemCount,
  listSummary,
  type BrowserRow
} from '@/utils/fileRows'
import { useFileActions } from '@/composables/useFileActions'
import { useCreateShare } from '@/composables/useCreateShare'

const props = withDefaults(
  defineProps<{
    items: FileItem[]
    /**
     * 当前目录的子文件夹。与 items 合成同一个列表（文件夹在前），
     * 不再是表格上方另一块卡片。
     */
    folders?: Folder[]
    /** 是否显示文件夹的分享/改名/删除按钮（只有自己的目录才有） */
    folderEditable?: boolean
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
    folders: () => [],
    folderEditable: false,
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
  /** 进入某个子目录 */
  (e: 'open-folder', folder: Folder): void
  (e: 'share-folder', folder: Folder): void
  (e: 'rename-folder', folder: Folder): void
  (e: 'delete-folder', folder: Folder): void
}>()

/**
 * 合成后的行：文件夹在前、文件在后。
 *
 * 搜索时由 filterFolders 按**文件夹名**做本地匹配（服务端的关键字只作用于文件），
 * 这样搜「报表」时既能看到匹配的文件、也还能进那个叫「报表」的目录。
 */
const rows = computed<BrowserRow[]>(() =>
  buildRows({
    folders: filterFolders(props.folders, props.keyword),
    files: props.items,
    page: props.page
  })
)

/** 当前目录参与展示的文件夹数（用于分页条左侧的汇总文案）。 */
const folderCount = computed(() => filterFolders(props.folders, props.keyword).length)

const summaryText = computed(() => listSummary(folderCount.value, props.total))

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

const columns = computed<DataTableColumns<BrowserRow>>(() => {
  const cols: DataTableColumns<BrowserRow> = [
    {
      title: '名称',
      // key 必须是后端白名单里的 `original_name`：排序键会原样发给服务端
      // （internal/store/files.go 的 sortColumn），改名会静默掉回 created_at。
      key: 'original_name',
      minWidth: 260,
      /**
       * 文件夹不参与服务端排序：`sort`/`order` 只是**文件**查询参数，
       * 让文件夹跟着变会与服务端返回的顺序对不上（表格显示的箭头也会骗人）。
       */
      sorter: (a, b) => {
        if (a.kind === 'folder' || b.kind === 'folder') return 0
        return a.file.original_name.localeCompare(b.file.original_name, 'zh-Hans-CN')
      },
      sortOrder: toNSort(sortOrder.value, 'original_name', sortKey.value),
      render(row) {
        if (row.kind === 'folder') {
          return h(NSpace, { align: 'center', size: 8, wrap: false }, {
            default: () => [
              h(NIcon, { size: 18, color: 'var(--color-primary)' }, {
                default: () => h(FolderOpenOutline)
              }),
              h(
                'a',
                {
                  class: 'file-name folder-name',
                  title: row.folder.name,
                  onClick: () => emit('open-folder', row.folder)
                },
                shorten(row.folder.name, 44)
              )
            ]
          })
        }
        return h(NSpace, { align: 'center', size: 8, wrap: false }, {
          default: () => [
            h(NTag, { size: 'small', type: extTagType(row.file.ext), bordered: false }, {
              default: () => extLabel(row.file.ext)
            }),
            h(
              'a',
              {
                class: 'file-name',
                title: row.file.original_name,
                onClick: () => actions.openPreview(row.file)
              },
              shorten(row.file.original_name, 44)
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
      render: (row) => (row.kind === 'folder' ? row.folder.owner_name : row.file.owner_name)
    })
  }

  cols.push(
    {
      // 文件夹没有"大小"：显示它直接包含的项数，与资源管理器的"X 个项目"一致。
      title: '大小',
      key: 'size_bytes',
      width: 104,
      sorter: (a, b) => {
        if (a.kind === 'folder' || b.kind === 'folder') return 0
        return a.file.size_bytes - b.file.size_bytes
      },
      sortOrder: toNSort(sortOrder.value, 'size_bytes', sortKey.value),
      render: (row) =>
        row.kind === 'folder'
          ? h('span', { class: 'folder-count' }, `${folderItemCount(row.folder)} 项`)
          : formatBytes(row.file.size_bytes)
    },
    {
      title: '上传时间',
      key: 'created_at',
      width: 150,
      sorter: (a, b) => {
        if (a.kind === 'folder' || b.kind === 'folder') return 0
        return a.file.created_at.localeCompare(b.file.created_at)
      },
      sortOrder: toNSort(sortOrder.value, 'created_at', sortKey.value),
      render: (row) =>
        row.kind === 'folder' ? '—' : formatTime(row.file.created_at)
    }
  )

  if (props.showExpiry && !props.adminMode) {
    cols.push({
      title: '到期',
      key: 'expires_at',
      width: 150,
      sorter: (a, b) => {
        if (a.kind === 'folder' || b.kind === 'folder') return 0
        return a.file.days_left - b.file.days_left
      },
      sortOrder: toNSort(sortOrder.value, 'expires_at', sortKey.value),
      render(row) {
        if (row.kind === 'folder') return '—'
        return h(
          NTag,
          { size: 'small', type: expiryType(row.file.days_left), bordered: false },
          { default: () => formatDaysLeft(row.file.days_left) }
        )
      }
    })
  }

  if (props.adminMode) {
    cols.push({
      title: '删除时间',
      key: 'deleted_at',
      width: 150,
      render: (row) =>
        row.kind === 'folder' || !row.file.deleted_at ? '—' : formatTime(row.file.deleted_at)
    })
    cols.push({
      title: '彻底删除',
      key: 'purge_at',
      width: 150,
      render: (row) =>
        row.kind === 'folder' || !row.file.purge_at ? '—' : formatTime(row.file.purge_at)
    })
  }

  cols.push({
    title: '操作',
    key: 'actions',
    width: props.adminMode ? 210 : props.readonly ? 120 : 215,
    fixed: 'right',
    render(row) {
      if (row.kind === 'folder') {
        const folderButtons = [
          h(NButton, { size: 'small', quaternary: true, onClick: () => emit('open-folder', row.folder) }, {
            icon: () => h(NIcon, null, { default: () => h(FolderOpenOutline) }),
            default: () => '打开'
          })
        ]
        if (props.folderEditable) {
          folderButtons.push(
            h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => emit('share-folder', row.folder) }, {
              icon: () => h(NIcon, null, { default: () => h(LinkOutline) }),
              default: () => '分享'
            }),
            h(NButton, { size: 'small', quaternary: true, onClick: () => emit('rename-folder', row.folder) }, {
              icon: () => h(NIcon, null, { default: () => h(CreateOutline) }),
              default: () => '重命名'
            }),
            h(
              NPopconfirm,
              { onPositiveClick: () => emit('delete-folder', row.folder) },
              {
                trigger: () =>
                  h(NButton, { size: 'small', quaternary: true, type: 'error' }, {
                    icon: () => h(NIcon, null, { default: () => h(TrashOutline) }),
                    default: () => '删除'
                  }),
                default: () => `删除「${row.folder.name}」？`
              }
            )
          )
        }
        return h(NSpace, { size: 2, wrap: false }, { default: () => folderButtons })
      }

      const file = row.file
      const buttons = [
        h(NButton, { size: 'small', quaternary: true, onClick: () => actions.openPreview(file) }, {
          icon: () => h(NIcon, null, { default: () => h(EyeOutline) }),
          default: () => '预览'
        }),
        h(NButton, { size: 'small', quaternary: true, onClick: () => actions.onDownload(file) }, {
          icon: () => h(NIcon, null, { default: () => h(DownloadOutline) }),
          default: () => '下载'
        })
      ]

      if (canShare(file)) {
        buttons.push(
          h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => onShare(file) }, {
            icon: () => h(NIcon, null, { default: () => h(LinkOutline) }),
            default: () => '分享'
          })
        )
      }

      if (props.adminMode) {
        if (file.status === 'trashed') {
          buttons.push(
            h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => actions.onRestore(file) }, {
              icon: () => h(NIcon, null, { default: () => h(RefreshOutline) }),
              default: () => '恢复'
            }),
            h(
              NPopconfirm,
              { onPositiveClick: () => actions.confirmPurge(file) },
              {
                trigger: () =>
                  h(NButton, { size: 'small', quaternary: true, type: 'error' }, {
                    icon: () => h(NIcon, null, { default: () => h(TrashOutline) }),
                    default: () => '彻底删除'
                  }),
                default: () => `永久删除「${file.original_name}」？`
              }
            )
          )
        } else {
          buttons.push(
            h(
              NPopconfirm,
              { onPositiveClick: () => actions.confirmDelete(file) },
              {
                trigger: () =>
                  h(NButton, { size: 'small', quaternary: true, type: 'error' }, {
                    icon: () => h(NIcon, null, { default: () => h(TrashOutline) }),
                    default: () => '删除'
                  }),
                default: () => `删除「${file.original_name}」？`
              }
            )
          )
        }
      } else if (!props.readonly && file.can_edit) {
        buttons.push(
          h(NButton, { size: 'small', quaternary: true, onClick: () => actions.onRename(file) }, {
            icon: () => h(NIcon, null, { default: () => h(CreateOutline) }),
            default: () => '重命名'
          }),
          h(
            NPopconfirm,
            { onPositiveClick: () => actions.confirmDelete(file) },
            {
              trigger: () =>
                h(NButton, { size: 'small', quaternary: true, type: 'error' }, {
                  icon: () => h(NIcon, null, { default: () => h(TrashOutline) }),
                  default: () => '删除'
                }),
              default: () => `删除「${file.original_name}」？`
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
  // 汇总文案要区分文件与文件夹：分页只对**文件**生效（文件夹只在第 1 页列出）
  prefix: () => summaryText.value
}))
</script>

<template>
  <div class="file-list">
    <!-- 当前归属：从菜单子 tab 进来时，顶栏只显示"全部文件"，
         这里补上具体是谁，否则用户不知道自己在看谁的目录 -->
    <div v-if="title" class="list-title">{{ title }}</div>

    <!-- 工具栏：左=搜索（筛选），右=刷新与调用方插入的按钮。
         与各页面头部共用 section-head 一族，保证高度与间距一致。 -->
    <div class="section-head toolbar">
      <div class="section-head__main toolbar__search">
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
      </div>
      <div class="section-head__actions toolbar-actions">
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
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: BrowserRow) => row.key"
        :row-props="
          (row: BrowserRow) => (row.kind === 'folder' ? { class: 'row-folder' } : {})
        "
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
      <n-empty v-if="!rows.length && !loading" description="暂无文件" style="padding: 32px 0" />
      <ul v-else class="card-list">
        <li v-for="row in rows" :key="row.key" class="file-card">
          <!-- 文件夹卡片：点整行进入目录 -->
          <template v-if="row.kind === 'folder'">
            <div class="file-card__head" @click="emit('open-folder', row.folder)">
              <n-icon :size="20" color="var(--color-primary)"><folder-open-outline /></n-icon>
              <span class="file-card__name">{{ row.folder.name }}</span>
              <!-- 箭头明示"可进入"：移动端没有静态"打开"按钮，光有下划线看不出来 -->
              <n-icon class="file-card__enter" :size="16"><chevron-forward-outline /></n-icon>
            </div>

            <div class="file-card__meta">
              <span>{{ folderItemCount(row.folder) }} 项</span>
              <span v-if="props.showOwner">{{ row.folder.owner_name }}</span>
            </div>

            <div v-if="props.folderEditable" class="file-card__foot">
              <span class="file-card__spacer" />
              <n-button size="small" quaternary type="primary" @click="emit('share-folder', row.folder)">
                <template #icon>
                  <n-icon><link-outline /></n-icon>
                </template>
              </n-button>
              <n-button size="small" quaternary @click="emit('rename-folder', row.folder)">
                <template #icon>
                  <n-icon><create-outline /></n-icon>
                </template>
              </n-button>
              <n-button size="small" quaternary type="error" @click="emit('delete-folder', row.folder)">
                <template #icon>
                  <n-icon><trash-outline /></n-icon>
                </template>
              </n-button>
            </div>
          </template>

          <template v-else>
          <div class="file-card__head" @click="actions.openPreview(row.file)">
            <n-tag size="small" :type="extTagType(row.file.ext)" :bordered="false">
              {{ extLabel(row.file.ext) }}
            </n-tag>
            <span class="file-card__name">{{ row.file.original_name }}</span>
          </div>

          <div class="file-card__meta">
            <span>{{ formatBytes(row.file.size_bytes) }}</span>
            <span v-if="props.showOwner">{{ row.file.owner_name }}</span>
            <span>{{ formatTime(row.file.created_at) }}</span>
          </div>

          <div class="file-card__foot">
            <n-tag
              v-if="props.showExpiry && !props.adminMode"
              size="small"
              :type="expiryType(row.file.days_left)"
              :bordered="false"
            >
              {{ formatDaysLeft(row.file.days_left) }}
            </n-tag>
            <n-tag v-else-if="props.adminMode && row.file.status === 'trashed'" size="small" type="error" :bordered="false">
              待清理
            </n-tag>
            <n-tag v-if="props.adminMode && row.file.purge_at" size="small" :bordered="false">
              {{ formatTime(row.file.purge_at) }} 清理
            </n-tag>
            <span class="file-card__spacer" />
            <n-button size="small" quaternary @click="actions.openPreview(row.file)">
              <template #icon>
                <n-icon><eye-outline /></n-icon>
              </template>
            </n-button>
            <n-button size="small" quaternary @click="actions.onDownload(row.file)">
              <template #icon>
                <n-icon><download-outline /></n-icon>
              </template>
            </n-button>
            <template v-if="props.adminMode">
              <n-button
                v-if="row.file.status === 'trashed'"
                size="small"
                quaternary
                type="primary"
                @click="actions.onRestore(row.file)"
              >
                恢复
              </n-button>
              <n-button
                v-if="row.file.status === 'trashed'"
                size="small"
                quaternary
                type="error"
                @click="actions.confirmPurge(row.file)"
              >
                彻底删除
              </n-button>
              <n-button v-else size="small" quaternary type="error" @click="actions.confirmDelete(row.file)">
                删除
              </n-button>
            </template>
            <template v-else-if="!props.readonly && row.file.can_edit">
              <n-button size="small" quaternary @click="actions.onRename(row.file)">
                <template #icon>
                  <n-icon><create-outline /></n-icon>
                </template>
              </n-button>
              <n-button size="small" quaternary type="error" @click="actions.confirmDelete(row.file)">
                <template #icon>
                  <n-icon><trash-outline /></n-icon>
                </template>
              </n-button>
            </template>
          </div>
          </template>
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

/* 结构由全局 .section-head 一族提供，这里只补本组件特有的部分：
   搜索框占满左区剩余宽度，窄屏整行折行。 */
.toolbar__search {
  flex: 1;
  min-width: 0;
}

.toolbar__search :deep(.n-input) {
  width: 100%;
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

/* 文件夹名比文件名更重：列表里两类行混排时，一眼能分出"能进去的"和"能打开的" */
.file-list :deep(.folder-name) {
  font-weight: 600;
}

/* 文件夹不显示"大小"而是项数，弱化一档，避免与文件大小抢注意力 */
.file-list :deep(.folder-count) {
  color: var(--color-text-muted);
}

/* 文件夹行给一点底色：资源管理器里文件夹也是与文件区分开的 */
.file-list :deep(.row-folder td) {
  background: var(--color-surface-soft);
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

/* 移动端文件夹卡片的"可进入"箭头：靠右、弱化，不与文件名抢注意力 */
.file-card__enter {
  flex: none;
  margin-left: auto;
  color: var(--color-text-muted);
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

  .toolbar__search {
    width: 100%;
  }

  .toolbar-actions {
    justify-content: flex-end;
    margin-left: 0;
  }
}
</style>
