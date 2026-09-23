// Naive UI 主题覆盖（明 / 暗两套）与共享样式常量。
//
// 维护约定（照搬同组织 Electrical-Manager 的做法）：
// - 两套覆盖是同一结构的两个调色板，由 createThemeOverrides(palette) 生成，
//   改一处即明暗同步生效。不允许只在一套里加属性 —— 否则切到另一套时会掉回内置值。
// - 调色板的语义色与 styles.css 的 --color-* 保持一致；
//   深色档是浅色档的提亮版本（深底上保证对比度），不代表新的语义。
import type { GlobalThemeOverrides } from 'naive-ui'

interface ThemePalette {
  primaryColor: string
  primaryColorHover: string
  primaryColorPressed: string
  primaryColorSuppl: string
  successColor: string
  textColorBase: string
  textColor1: string
  textColor2: string
  textColor3: string
  placeholderColor: string
  dividerColor: string
  borderColor: string
  bodyColor: string
  cardColor: string
  cardBorderColor: string
  tableHeaderColor: string
  tableHeaderTextColor: string
  hoverColor: string
  tableColorHover: string
  boxShadow1: string
  boxShadow2: string
  menuItemColorHover: string
  menuItemColorActive: string
  menuItemColorActiveHover: string
  menuTextColorActive: string
}

const light: ThemePalette = {
  primaryColor: '#3f63d8',
  primaryColorHover: '#5376e4',
  primaryColorPressed: '#3151bd',
  primaryColorSuppl: '#5376e4',
  successColor: '#229b6b',
  textColorBase: '#172033',
  textColor1: '#172033',
  textColor2: '#4b5565',
  textColor3: '#7d8798',
  placeholderColor: '#a1a9b7',
  dividerColor: '#edf0f5',
  borderColor: '#e2e7ef',
  bodyColor: '#f4f6fa',
  cardColor: '#ffffff',
  cardBorderColor: '#e7ebf2',
  tableHeaderColor: '#f7f9fc',
  tableHeaderTextColor: '#3d4758',
  hoverColor: '#f3f6ff',
  tableColorHover: '#f4f7ff',
  boxShadow1: '0 8px 24px rgba(15, 23, 42, 0.05)',
  boxShadow2: '0 14px 36px rgba(15, 23, 42, 0.08)',
  menuItemColorHover: '#f4f6fb',
  menuItemColorActive: '#edf2ff',
  menuItemColorActiveHover: '#e7edff',
  menuTextColorActive: '#3658c7'
}

const dark: ThemePalette = {
  primaryColor: '#6b8cf0',
  primaryColorHover: '#86a2f5',
  primaryColorPressed: '#5578dd',
  primaryColorSuppl: '#86a2f5',
  successColor: '#3fbf8a',
  textColorBase: '#e6eaf2',
  textColor1: '#e6eaf2',
  textColor2: '#aeb8c9',
  textColor3: '#8791a3',
  placeholderColor: '#6e7889',
  dividerColor: '#2a313b',
  borderColor: '#333b47',
  bodyColor: '#13171d',
  cardColor: '#191e26',
  cardBorderColor: '#2a313b',
  tableHeaderColor: '#1e242d',
  tableHeaderTextColor: '#c9d1de',
  hoverColor: '#212936',
  tableColorHover: '#212936',
  boxShadow1: '0 8px 24px rgba(0, 0, 0, 0.32)',
  boxShadow2: '0 14px 36px rgba(0, 0, 0, 0.42)',
  menuItemColorHover: '#212936',
  menuItemColorActive: '#232f4a',
  menuItemColorActiveHover: '#2a3757',
  menuTextColorActive: '#9fb5ff'
}

function createThemeOverrides(p: ThemePalette): GlobalThemeOverrides {
  return {
    common: {
      primaryColor: p.primaryColor,
      primaryColorHover: p.primaryColorHover,
      primaryColorPressed: p.primaryColorPressed,
      primaryColorSuppl: p.primaryColorSuppl,
      successColor: p.successColor,
      textColorBase: p.textColorBase,
      textColor1: p.textColor1,
      textColor2: p.textColor2,
      textColor3: p.textColor3,
      placeholderColor: p.placeholderColor,
      dividerColor: p.dividerColor,
      borderColor: p.borderColor,
      bodyColor: p.bodyColor,
      cardColor: p.cardColor,
      hoverColor: p.hoverColor,
      borderRadius: '9px',
      boxShadow1: p.boxShadow1,
      boxShadow2: p.boxShadow2
    },
    Card: {
      borderRadius: '14px',
      borderColor: p.cardBorderColor
    },
    DataTable: {
      thColor: p.tableHeaderColor,
      thTextColor: p.tableHeaderTextColor,
      tdColorHover: p.tableColorHover,
      borderRadius: '10px'
    },
    Menu: {
      itemColorHover: p.menuItemColorHover,
      itemColorActive: p.menuItemColorActive,
      itemColorActiveHover: p.menuItemColorActiveHover,
      itemTextColorActive: p.menuTextColorActive,
      itemTextColorActiveHover: p.menuTextColorActive,
      borderRadius: '9px'
    },
    Layout: {
      color: p.bodyColor,
      siderColor: p.cardColor,
      headerColor: p.cardColor
    }
  }
}

export const lightThemeOverrides = createThemeOverrides(light)
export const darkThemeOverrides = createThemeOverrides(dark)

/** 文件类型对应的标签颜色（用于 n-tag）。 */
export function extTagType(ext: string): 'default' | 'info' | 'success' | 'warning' | 'error' {
  const e = (ext || '').toLowerCase()
  if (['.doc', '.docx', '.rtf'].includes(e)) return 'info'
  if (['.xls', '.xlsx', '.csv'].includes(e)) return 'success'
  if (['.ppt', '.pptx'].includes(e)) return 'warning'
  if (['.pdf'].includes(e)) return 'error'
  return 'default'
}
