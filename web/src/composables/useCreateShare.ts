// 创建分享链接：文件列表、文件夹、我的文件共用同一套弹窗与逻辑。
//
// 为什么做成 composable：分享入口会出现在多处（文件行、目录行），
// 各写一份必然出现"某处忘了传有效期"或"文案不一致"。
import { h, ref } from 'vue'
import { NRadio, NRadioGroup, NSpace, useDialog, useMessage } from 'naive-ui'
import { createShare, errMsg, shareExpireOptions } from '@/api'
import type { ShareTargetType } from '@/api/types'

/** 有效期选项的中文标签。null 表示永久。 */
export function expireLabel(days: number | null): string {
  return days === null ? '永久（默认）' : `${days} 天`
}

/**
 * 打开「创建分享」弹窗并返回创建好的链接；用户取消时返回 null。
 *
 * 用 dialog 而不是独立页面：创建分享是一次轻量动作，
 * 跳页会打断"我正在看文件列表"的上下文。
 */
export function useCreateShare() {
  const dialog = useDialog()
  const message = useMessage()
  const options = ref<number[]>([])

  // 有效期清单由服务端给出，避免前端硬编码一套值与后端校验不一致。
  async function loadOptions() {
    if (options.value.length) return
    try {
      const res = await shareExpireOptions()
      options.value = res.expire_days
    } catch {
      // 拉取失败时退回与后端一致的内置值，不让功能整体不可用。
      options.value = [1, 3, 7, 30]
    }
  }

  async function create(targetType: ShareTargetType, targetID: number, targetName: string): Promise<string | null> {
    await loadOptions()

    return new Promise((resolve) => {
      // 选中值用 -1 代表"永久"，避免用 null 与"未选择"混淆。
      const picked = ref<number>(-1)

      dialog.create({
        title: '创建分享链接',
        content: () =>
          h('div', { class: 'share-dialog' }, [
            h('p', { class: 'share-dialog__target' }, `「${targetName}」`),
            h('p', { class: 'share-dialog__label' }, '链接有效期'),
            h(
              NRadioGroup,
              { value: picked.value, 'onUpdate:value': (v: number) => (picked.value = v) },
              {
                default: () =>
                  h(
                    NSpace,
                    { vertical: false, size: 8 },
                    {
                      default: () => [
                        h(NRadio, { value: -1 }, { default: () => expireLabel(null) }),
                        ...options.value.map((d) =>
                          h(NRadio, { value: d }, { default: () => expireLabel(d) })
                        )
                      ]
                    }
                  )
              }
            ),
            h('p', { class: 'share-dialog__hint' }, '拿到链接的人无需登录即可查看和下载。')
          ]),
        positiveText: '创建链接',
        negativeText: '取消',
        onPositiveClick: async () => {
          try {
            const sh = await createShare({
              target_type: targetType,
              target_id: targetID,
              // -1 → 永久（后端约定 null）
              expire_days: picked.value === -1 ? null : picked.value
            })
            const url = `${window.location.origin}/s/${sh.token}`
            await copyToClipboard(url)
            message.success('链接已复制，可直接发给别人')
            resolve(url)
          } catch (e) {
            message.error(errMsg(e))
            resolve(null)
          }
        },
        onNegativeClick: () => resolve(null),
        onClose: () => resolve(null)
      })
    })
  }

  return { create }
}

/** 复制到剪贴板。失败时不抛错，只返回 false（由调用方决定提示）。 */
export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}
