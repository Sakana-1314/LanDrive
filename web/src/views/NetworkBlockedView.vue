<script setup lang="ts">
// 网络不可用提示页：前端在公网、接口在内网时的主要兜底界面。
//
// 触发场景：员工在外网/家里打开前端，请求内网 API 超时或被浏览器 CORS 拦截。
// 文案按需求固定为「无法在此网络下使用，请更换网络再试！」。
import { NButton, NCard, NIcon, NSpace, NText } from 'naive-ui'
import { CloudOfflineOutline, RefreshOutline } from '@vicons/ionicons5'
import { apiEndpointInfo, NETWORK_UNAVAILABLE_MESSAGE } from '@/api'

const props = defineProps<{ detail?: string }>()
const emit = defineEmits<{ (e: 'retry'): void }>()

const info = apiEndpointInfo()
</script>

<template>
  <div class="net-page">
    <n-card class="net-card" :bordered="false">
      <n-space vertical align="center" :size="12">
        <n-icon size="52" color="#f0a020"><cloud-offline-outline /></n-icon>
        <n-text strong style="font-size: 19px">{{ NETWORK_UNAVAILABLE_MESSAGE }}</n-text>

        <n-text depth="3" style="font-size: 13px; text-align: center; line-height: 1.7">
          本系统的文件服务部署在公司内网，需要连接公司网络（或公司 VPN）才能使用。<br />
          请切换到公司网络后重试。
        </n-text>

        <n-space vertical :size="4" style="width: 100%; margin-top: 4px">
          <n-text depth="3" style="font-size: 12px">
            接口地址：<code>{{ info.base_url }}</code>
          </n-text>
          <n-text v-if="props.detail" depth="3" style="font-size: 12px; word-break: break-all">
            错误详情：{{ props.detail }}
          </n-text>
        </n-space>

        <n-button type="primary" size="large" style="margin-top: 6px" @click="emit('retry')">
          <template #icon>
            <n-icon><refresh-outline /></n-icon>
          </template>
          重新检测
        </n-button>

        <n-text depth="3" style="font-size: 12px">
          已连接公司网络仍无法使用？请联系管理员检查内网服务与跨域白名单配置。
        </n-text>
      </n-space>
    </n-card>
  </div>
</template>

<style scoped>
.net-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  box-sizing: border-box;
  background: linear-gradient(135deg, #fff8ec 0%, #f7f9fc 60%, #eef4ff 100%);
}
.net-card {
  width: 460px;
  max-width: 100%;
  box-shadow: 0 10px 40px rgba(240, 160, 32, 0.14);
  border-radius: 12px;
}
code {
  background: #f2f3f5;
  padding: 1px 5px;
  border-radius: 4px;
  font-size: 12px;
}
</style>
