// 外观（明暗）与响应式断点。
//
// 为什么用 reactive 单例而不是 Pinia：本仓库约定不引入 Pinia（见 AGENTS.md），
// 全局状态用 reactive 单例即可，这里只存一个外观档位。
import { computed, reactive, watchEffect } from 'vue'
import { useMediaQuery, usePreferredDark } from '@vueuse/core'

export type ThemeMode = 'auto' | 'light' | 'dark'

const STORAGE_KEY = 'lanfs-theme-mode'

function readStoredMode(): ThemeMode {
  const raw = localStorage.getItem(STORAGE_KEY)
  return raw === 'light' || raw === 'dark' || raw === 'auto' ? raw : 'auto'
}

/** 外观档位：auto（跟随系统）/ light / dark。只存浏览器本地，不产生请求。 */
export const themeState = reactive<{ mode: ThemeMode }>({ mode: readStoredMode() })

/** 系统是否偏好深色（auto 档跟随它，系统切换后实时生效）。 */
const preferredDark = usePreferredDark()

/** 实际生效的明暗。 */
export const isDark = computed(() =>
  themeState.mode === 'auto' ? preferredDark.value : themeState.mode === 'dark'
)

export function setThemeMode(mode: ThemeMode): void {
  themeState.mode = mode
  localStorage.setItem(STORAGE_KEY, mode)
}

/** 菜单里显示当前档位。 */
export const themeModeLabel = computed(() => {
  if (themeState.mode === 'auto') return '跟随系统'
  return themeState.mode === 'dark' ? '深色' : '浅色'
})

// 生效的明暗写到 <html data-theme>：styles.css 的令牌据此切换；
// color-scheme 让滚动条与原生控件一起跟随，避免深色页面顶着白色滚动条。
// immediate 保证首屏就与系统一致，不会先闪浅色再变深色。
watchEffect(() => {
  const root = document.documentElement
  const resolved = isDark.value ? 'dark' : 'light'
  root.dataset.theme = resolved
  root.style.colorScheme = resolved
})

/**
 * 移动端（≤768px）。
 * 与 styles.css 的 --breakpoint-mobile 保持一致；改一处要同步另一处。
 */
export function useIsMobile() {
  return useMediaQuery('(max-width: 768px)')
}

/** 窄屏（≤1100px）：桌面侧栏自动收起，给小屏笔记本留出内容宽度。 */
export function useIsNarrow() {
  return useMediaQuery('(max-width: 1100px)')
}
