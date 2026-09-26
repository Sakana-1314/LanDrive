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
  permanentStatus,
  renameFile,
  setFilePermanent
} from '@/api'
import type { FileItem, PermanentStatus } from '@/api/types'
import { formatBytes } from '@/utils/format'
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

  /**
   * 设为永久：**必须二次确认**，并在确认框里提示永久空间只有多少、还剩多少。
   *
   * 为什么确认框要先拉一次永久空间状况：需求要求"提示用户永久空间仅有 100G"，
   * 但一个写死的数字对用户没用 —— 他想知道的是"我这次设下去还剩多少"。
   * 因此这里先取实时用量，把「已用 / 上限 / 剩余」和本次文件大小一起摆出来，
   * 用户能自己判断要不要设。取不到（接口失败）时退化成静态提示，不阻断操作。
   */
  async function confirmSetPermanent(row: FileItem) {
    let usage: PermanentStatus | null = null
    try {
      usage = await permanentStatus()
    } catch {
      // 用量取不到不该挡着用户操作；确认框里少一行数字而已。
    }
    if (usage && !usage.enabled) {
      message.warning('管理员未开放永久保存，请联系管理员')
      return
    }
    const lines = [
      `「${row.original_name}」（${formatBytes(row.size_bytes)}）将设为永久保存，`,
      '不会随保留期自动清理，需要手动删除。',
    ]
    if (usage) {
      lines.push(
        '',
        `永久空间共 ${formatBytes(usage.quota_bytes)}，已用 ${formatBytes(usage.used_bytes)}，`,
        `本次之后还剩 ${formatBytes(Math.max(0, usage.free_bytes - row.size_bytes))}。`
      )
    } else {
      lines.push('', '提示：永久空间有限，请谨慎使用。')
    }
    dialog.warning({
      title: '设为永久保存',
      content: () => h('div', { style: { whiteSpace: 'pre-line', lineHeight: '1.7' } }, lines.join('\n')),
      positiveText: '确认设为永久',
      negativeText: '取消',
      onPositiveClick: async () => {
        try {
          await setFilePermanent(row.id, true)
          message.success('已设为永久保存')
          options.onRefresh()
        } catch (e) {
          // 配额不足时后端会返回带具体差额的文案，直接展示即可。
          message.error(errMsg(e))
          return false
        }
        return true
      }
    })
  }

  /** 改回有期限（按当前保留天数重新计时）。误设永久后的补救入口。 */
  async function onSetDated(row: FileItem) {
    dialog.warning({
      title: '改回有期限',
      content: `「${row.original_name}」将按当前的保留天数重新计算到期时间，不再永久保存。`,
      positiveText: '确认',
      negativeText: '取消',
      onPositiveClick: async () => {
        try {
          await setFilePermanent(row.id, false)
          message.success('已改回有期限')
          options.onRefresh()
        } catch (e) {
          message.error(errMsg(e))
          return false
        }
        return true
      }
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
    confirmPurge,
    confirmSetPermanent,
    onSetDated
  }
}
