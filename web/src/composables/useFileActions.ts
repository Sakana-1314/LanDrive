// 文件操作逻辑：桌面表格与移动卡片共用，避免两套视图各写一份增删改查。
//
// 组件只负责「拿到一行文件后能做什么」，不关心渲染形态。
import { h, ref } from 'vue'
import { useDialog, useMessage, NInput } from 'naive-ui'
import {
  adminPurgeFile,
  adminRestoreFile,
  deleteFile,
  downloadFile,
  errMsg,
  renameFile
} from '@/api'
import type { FileItem } from '@/api/types'
import { openPreview } from '@/stores/preview'

export interface FileActionsOptions {
  adminMode?: boolean
  /** 刷新列表。 */
  onRefresh: () => void
}

export function useFileActions(options: FileActionsOptions) {
  const message = useMessage()
  const dialog = useDialog()

  /**
   * 打开预览：在列表页内弹层呈现（iframe），不再 window.open 新标签。
   * 弹层挂在 MainLayout 上，这里只负责把「要看哪个文件」写进全局状态。
   */
  function onOpenPreview(row: FileItem) {
    openPreview(row.id)
  }

  async function onDownload(row: FileItem) {
    try {
      await downloadFile(row.id, row.original_name)
    } catch (e) {
      message.error(errMsg(e))
    }
  }

  function onRename(row: FileItem) {
    // 扩展名由后端固定，只让用户改主名，避免「改了扩展名却与实际类型不符」。
    const name = ref(row.original_name)
    dialog.create({
      title: '重命名',
      content: () =>
        h(NInput, {
          value: name.value,
          autofocus: true,
          placeholder: '新的文件名',
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
          message.success('已重命名')
          options.onRefresh()
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
      options.onRefresh()
    } catch (e) {
      message.error(errMsg(e))
    }
  }

  function confirmDelete(row: FileItem) {
    dialog.warning({
      title: '删除文件',
      content: `「${row.original_name}」将移入回收站，普通用户将无法再看到它。`,
      positiveText: '移入回收站',
      negativeText: '取消',
      onPositiveClick: () => onDelete(row)
    })
  }

  async function onRestore(row: FileItem) {
    try {
      await adminRestoreFile(row.id)
      message.success('已恢复')
      options.onRefresh()
    } catch (e) {
      message.error(errMsg(e))
    }
  }

  function confirmPurge(row: FileItem) {
    dialog.error({
      title: '彻底删除',
      content: `「${row.original_name}」将从磁盘与数据库中永久删除，无法恢复。`,
      positiveText: '永久删除',
      negativeText: '取消',
      onPositiveClick: () => onPurge(row)
    })
  }

  async function onPurge(row: FileItem) {
    try {
      await adminPurgeFile(row.id)
      message.success('已彻底删除')
      options.onRefresh()
    } catch (e) {
      message.error(errMsg(e))
    }
  }

  return {
    onOpenPreview,
    onDownload,
    onRename,
    confirmDelete,
    onDelete,
    onRestore,
    confirmPurge
  }
}
