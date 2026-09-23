// Naive UI 主题覆盖与共享样式常量。
import type { GlobalThemeOverrides } from 'naive-ui'

export const themeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: '#1f6feb',
    primaryColorHover: '#3b82f6',
    primaryColorPressed: '#1a5fd0',
    primaryColorSuppl: '#3b82f6',
    borderRadius: '6px'
  }
}

/** 文件类型对应的标签颜色（用于 n-tag）。 */
export function extTagType(ext: string): 'default' | 'info' | 'success' | 'warning' | 'error' {
  const e = (ext || '').toLowerCase()
  if (['.doc', '.docx', '.rtf'].includes(e)) return 'info'
  if (['.xls', '.xlsx', '.csv'].includes(e)) return 'success'
  if (['.ppt', '.pptx'].includes(e)) return 'warning'
  if (['.pdf'].includes(e)) return 'error'
  return 'default'
}
