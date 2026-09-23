<script setup lang="ts">
// 文件表格：全部文件 / 我的文件 / 管理端回收站共用。
//
// 通过 props 控制列的显隐与操作按钮，避免三处重复实现。
import { computed, h, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  NButton,
  NDataTable,
  NEmpty,
  NIcon,
  NInput,
  NPopconfirm,
  NSpace,
  NTag,
  NText,
  NTooltip,
  useDialog,
  useMessage,
  type DataTableColumns
} from 'naive-ui'
import {
  CreateOutline,
  DownloadOutline,
  EyeOutline,
  RefreshOutline,
  SearchOutline,
  TrashOutline
} from '@vicons/ionicons5'
import {
  adminPurgeFile,
  adminRestoreFile,
  deleteFile,
  downloadFile,
  errMsg,
  renameFile
} from '@/api'
import type { FileItem } from '@/api/types'
import { extLabel, formatBytes, formatDaysLeft, formatTime, shorten } from '@/utils/format'
import { extTagType } from '@/utils/theme'

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
  }>(),
  {
    loading: false,
    showOwner: true,
    showExpiry: true,
    adminMode: false,
    readonly: false,
    keyword: ''
  }
)

const emit = defineEmits<{
  (e: 'update:page', v: number): void
  (e: 'update:pageSize', v: number): void
  (e: 'update:keyword', v: string): void
  (e: 'refresh'): void
  (e: 'sort', payload: { sort: string; order: 'asc' | 'desc' }): void
}>()

const router = useRouter()
const message = useMessage()
const dialog = useDialog()

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

function openPreview(row: FileItem) {
  const url = router.resolve({ name: 'preview', params: { id: row.id } })
  window.open(url.href, '_blank')
}

async function onDownload(row: FileItem) {
  try {
    await downloadFile(row.id, row.original_name)
  } catch (e) {
    message.error(errMsg(e))
  }
}

function onRename(row: FileItem) {
  const name = ref(row.original_name)
  dialog.create({
    title: '重命名文件',
    content: () =>
      h(NInput, {
        value: name.value,
        autofocus: true,
        placeholder: '请输入新的文件名（扩展名不可修改）',
        'onUpdate:value': (v: string) => {
          name.value = v
        }
      }),
    positiveText: '保存',
    negativeText: '取消',
    onPositiveClick: async () => {
      if (!name.value.trim()) {
        message.warning('文件名不能为空')
        return false
      }
      try {
        await renameFile(row.id, name.value.trim())
        message.success('重命名成功')
        emit('refresh')
      } catch (e) {
        message.error(errMsg(e))
        return false
      }
      return true
    }
  })
}

async function onDelete(row: FileItem) {
  try {
    await deleteFile(row.id)
    message.success('已移入回收站')
    emit('refresh')
  } catch (e) {
    message.error(errMsg(e))
  }
}

async function onRestore(row: FileItem) {
  try {
    await adminRestoreFile(row.id)
    message.success('已恢复')
    emit('refresh')
  } catch (e) {
    message.error(errMsg(e))
  }
}

async function onPurge(row: FileItem) {
  try {
    await adminPurgeFile(row.id)
    message.success('已彻底删除')
    emit('refresh')
  } catch (e) {
    message.error(errMsg(e))
  }
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
              'span',
              {
                style: 'cursor: pointer; color: #1f6feb;',
                title: row.original_name,
                onClick: () => openPreview(row)
              },
              shorten(row.original_name, 46)
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
      width: 140,
      render(row) {
        return h(NSpace, { size: 4, align: 'center' }, {
          default: () => [
            h(NText, null, { default: () => row.owner_name }),
            h(NText, { depth: 3, style: 'font-size: 12px' }, {
              default: () => row.owner_employee_no
            })
          ]
        })
      }
    })
  }

  cols.push(
    {
      title: '大小',
      key: 'size_bytes',
      width: 100,
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
      title: '到期时间',
      key: 'expires_at',
      width: 190,
      sorter: true,
      sortOrder: toNSort(sortOrder.value, 'expires_at', sortKey.value),
      render(row) {
        const danger = row.days_left <= 3
        return h(NSpace, { size: 6, align: 'center' }, {
          default: () => [
            h(NText, { depth: 3 }, { default: () => formatTime(row.expires_at) }),
            h(NTag, { size: 'tiny', type: danger ? 'error' : 'default', bordered: false }, {
              default: () => formatDaysLeft(row.days_left)
            })
          ]
        })
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
      title: '彻底删除时间',
      key: 'purge_at',
      width: 160,
      render: (row) => (row.purge_at ? formatTime(row.purge_at) : '—')
    })
  }

  cols.push({
    title: '操作',
    key: 'actions',
    width: props.adminMode ? 220 : props.readonly ? 130 : 230,
    fixed: 'right',
    render(row) {
      const buttons = [
        h(
          NTooltip,
          { trigger: 'hover' },
          {
            trigger: () => h(NButton, { size: 'small', quaternary: true, onClick: () => openPreview(row) }, {
              icon: () => h(NIcon, null, { default: () => h(EyeOutline) }),
              default: () => '预览'
            }),
            default: () => (row.ext === '.doc' || row.ext === '.xls' || row.ext === '.ppt'
              ? '该格式不支持在线预览，将提示下载'
              : '在新标签页中预览')
          }
        ),
        h(NButton, { size: 'small', quaternary: true, onClick: () => onDownload(row) }, {
          icon: () => h(NIcon, null, { default: () => h(DownloadOutline) }),
          default: () => '下载'
        })
      ]

      if (props.adminMode) {
        if (row.status === 'trashed') {
          buttons.push(
            h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => onRestore(row) }, {
              icon: () => h(NIcon, null, { default: () => h(RefreshOutline) }),
              default: () => '恢复'
            }),
            h(
              NPopconfirm,
              { onPositiveClick: () => onPurge(row) },
              {
                trigger: () =>
                  h(NButton, { size: 'small', quaternary: true, type: 'error' }, {
                    icon: () => h(NIcon, null, { default: () => h(TrashOutline) }),
                    default: () => '彻底删除'
                  }),
                default: () => `确定立即彻底删除「${row.original_name}」？此操作不可恢复。`
              }
            )
          )
        } else {
          buttons.push(
            h(
              NPopconfirm,
              { onPositiveClick: () => onDelete(row) },
              {
                trigger: () =>
                  h(NButton, { size: 'small', quaternary: true, type: 'error' }, {
                    icon: () => h(NIcon, null, { default: () => h(TrashOutline) }),
                    default: () => '删除'
                  }),
                default: () => `确定删除「${row.original_name}」？将进入回收站。`
              }
            )
          )
        }
      } else if (!props.readonly && row.can_edit) {
        buttons.push(
          h(NButton, { size: 'small', quaternary: true, onClick: () => onRename(row) }, {
            icon: () => h(NIcon, null, { default: () => h(CreateOutline) }),
            default: () => '重命名'
          }),
          h(
            NPopconfirm,
            { onPositiveClick: () => onDelete(row) },
            {
              trigger: () =>
                h(NButton, { size: 'small', quaternary: true, type: 'error' }, {
                  icon: () => h(NIcon, null, { default: () => h(TrashOutline) }),
                  default: () => '删除'
                }),
              default: () =>
                `确定删除「${row.original_name}」？删除后普通用户将无法再看到该文件。`
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
  prefix: (info: { itemCount?: number }) => `共 ${info.itemCount ?? 0} 个文件`
}))

function onPageChange(p: number) {
  emit('update:page', p)
}
function onPageSizeChange(ps: number) {
  emit('update:pageSize', ps)
}
</script>

<template>
  <div>
    <n-space justify="space-between" align="center" style="margin-bottom: 12px">
      <n-input
        :value="keyword"
        placeholder="搜索文件名 / 上传者姓名 / 工号"
        clearable
        style="max-width: 320px"
        @update:value="(v: string) => emit('update:keyword', v)"
        @keyup.enter="emit('refresh')"
      >
        <template #prefix>
          <n-icon><search-outline /></n-icon>
        </template>
      </n-input>
      <slot name="toolbar" />
    </n-space>

    <n-data-table
      :columns="columns"
      :data="items"
      :loading="loading"
      :pagination="pagination"
      :row-key="(row: FileItem) => row.id"
      remote
      size="small"
      :scroll-x="props.showOwner ? 1200 : 1000"
      @update:page="onPageChange"
      @update:page-size="onPageSizeChange"
      @update:sorter="
        (s: any) => {
          // Naive UI 传出 ascend / descend，转换为后端契约的 asc / desc。
          if (s && s.order) onSort(String(s.columnKey), s.order === 'ascend' ? 'asc' : 'desc')
        }
      "
    >
      <template #empty>
        <n-empty description="暂无文件" style="padding: 24px 0" />
      </template>
    </n-data-table>
  </div>
</template>
