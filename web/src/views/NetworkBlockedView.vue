<script setup lang="ts">
// 网络不可用页：前端在公网、接口在内网时的主要兜底界面。
//
// 触发场景：员工在外网打开页面，请求内网 API 超时或被浏览器 CORS 拦截。
// 文案按需求固定为「无法在此网络下使用，请更换网络再试！」。
//
// 这里保留说明文字是刻意的：用户看到页面却用不了，必须知道「为什么」和「怎么办」。
// 这属于异常态提示，不是「功能靠小字解释」。
import { NButton, NIcon } from 'naive-ui'
import { CloudOfflineOutline, RefreshOutline } from '@vicons/ionicons5'
import { apiEndpointInfo, NETWORK_UNAVAILABLE_MESSAGE } from '@/api'

const props = defineProps<{ detail?: string }>()
const emit = defineEmits<{ (e: 'retry'): void }>()

const info = apiEndpointInfo()
</script>

<template>
  <div class="net-page">
    <div class="net-card">
      <div class="net-icon" aria-hidden="true">
        <n-icon :size="34"><cloud-offline-outline /></n-icon>
      </div>
      <h1 class="net-title">{{ NETWORK_UNAVAILABLE_MESSAGE }}</h1>
      <p class="net-text">文件服务部署在公司内网，请连接公司网络或 VPN 后重试。</p>

      <n-button type="primary" size="large" @click="emit('retry')">
        <template #icon>
          <n-icon><refresh-outline /></n-icon>
        </template>
        重新检测
      </n-button>

      <div class="net-detail">
        <div>{{ info.base_url }}</div>
        <div v-if="props.detail">{{ props.detail }}</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.net-page {
  display: grid;
  min-height: 100dvh;
  padding: 24px;
  place-items: center;
  background: var(--page-glow), var(--color-bg);
}

.net-card {
  display: grid;
  width: 100%;
  max-width: 420px;
  padding: 34px 28px;
  border: 1px solid var(--color-border-subtle);
  border-radius: 18px;
  background: var(--color-surface);
  box-shadow: var(--shadow-float);
  justify-items: center;
  text-align: center;
}

.net-icon {
  display: grid;
  width: 62px;
  height: 62px;
  margin-bottom: 16px;
  place-items: center;
  border-radius: 18px;
  color: var(--color-warning);
  background: color-mix(in srgb, var(--color-warning) 14%, transparent);
}

.net-title {
  margin: 0 0 10px;
  color: var(--color-text-strong);
  font-size: 18px;
  font-weight: 650;
  line-height: 1.5;
}

.net-text {
  margin: 0 0 22px;
  color: var(--color-text-muted);
  font-size: 14px;
  line-height: 1.7;
}

.net-detail {
  margin-top: 20px;
  color: var(--color-text-muted);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  line-height: 1.8;
  word-break: break-all;
}
</style>
