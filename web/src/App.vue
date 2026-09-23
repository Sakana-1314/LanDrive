<script setup lang="ts">
// 应用根组件：Naive UI 全局 Provider（中文语言包 + 明暗主题 + 消息/对话框/通知 API）。
//
// 全局基础样式在 main.ts 引入 styles.css；这里不再放样式，
// 避免出现两处都定义 html/body 的重复。
import { computed } from 'vue'
import {
  NConfigProvider,
  NDialogProvider,
  NLoadingBarProvider,
  NMessageProvider,
  NNotificationProvider,
  darkTheme,
  zhCN,
  dateZhCN
} from 'naive-ui'
import { darkThemeOverrides, lightThemeOverrides } from '@/utils/theme'
import { isDark } from '@/utils/themeState'

// 明暗切换：令牌（styles.css）由 themeState 写到 <html data-theme>；
// Naive UI 组件自身由这里按同一状态切换 darkTheme 与对应覆盖值。
const naiveTheme = computed(() => (isDark.value ? darkTheme : null))
const overrides = computed(() => (isDark.value ? darkThemeOverrides : lightThemeOverrides))
</script>

<template>
  <n-config-provider
    :locale="zhCN"
    :date-locale="dateZhCN"
    :theme="naiveTheme"
    :theme-overrides="overrides"
  >
    <n-loading-bar-provider>
      <n-message-provider>
        <n-notification-provider>
          <n-dialog-provider>
            <router-view />
          </n-dialog-provider>
        </n-notification-provider>
      </n-message-provider>
    </n-loading-bar-provider>
  </n-config-provider>
</template>
