<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NAlert,
  NButton,
  NCard,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NSpace,
  NText,
  useMessage
} from 'naive-ui'
import { CloudOutline, LockClosedOutline, PersonOutline } from '@vicons/ionicons5'
import {
  apiEndpointInfo,
  errMsg,
  isNetworkError,
  login,
  probeHealth,
  setToken,
  uploadConfig
} from '@/api'
import { applyUploadConfig, setUser } from '@/stores/user'

const router = useRouter()
const route = useRoute()
const message = useMessage()

const employeeNo = ref('')
const password = ref('')
const loading = ref(false)
/** 内网接口是否可达：null 表示尚未探测。 */
const reachable = ref<boolean | null>(null)
const probing = ref(false)
/** 失败原因（网络类或业务类）。 */
const failure = ref('')
const info = apiEndpointInfo()

/** 探测内网接口连通性（页面加载与手动重试时调用）。 */
async function probe() {
  probing.value = true
  failure.value = ''
  try {
    reachable.value = await probeHealth()
  } finally {
    probing.value = false
  }
}

async function onSubmit() {
  if (!employeeNo.value.trim()) {
    message.warning('请输入工号')
    return
  }
  if (!password.value) {
    message.warning('请输入密码')
    return
  }
  loading.value = true
  failure.value = ''
  try {
    const res = await login(employeeNo.value.trim(), password.value)
    setToken(res.token)
    setUser(res.user)
    reachable.value = true
    // 登录后立刻取一次上传策略，供上传页即时校验。
    try {
      applyUploadConfig(await uploadConfig())
    } catch {
      /* 策略拉取失败不阻塞登录 */
    }
    message.success(`欢迎，${res.user.name}`)
    const redirect = (route.query.redirect as string) || '/files'
    router.replace(redirect)
  } catch (e) {
    failure.value = errMsg(e)
    if (isNetworkError(e)) {
      // 网络类失败：明确告知需更换网络，而不是让用户误以为密码错误。
      reachable.value = false
      message.error(errMsg(e))
      return
    }
    reachable.value = true
    message.error(errMsg(e))
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void probe()
})
</script>

<template>
  <div class="login-page">
    <n-card class="login-card" :bordered="false">
      <n-space vertical :size="6" align="center" style="margin-bottom: 18px">
        <n-icon size="42" color="#1f6feb"><cloud-outline /></n-icon>
        <n-text strong style="font-size: 20px">局域网文件助手</n-text>
        <n-text depth="3" style="font-size: 13px">公司内网文件共享平台</n-text>
      </n-space>

      <!-- 内网接口不可达：优先给出明确提示，避免用户误以为密码错误 -->
      <n-alert
        v-if="reachable === false"
        type="warning"
        title="无法在此网络下使用，请更换网络再试！"
        style="margin-bottom: 14px"
      >
        <div style="font-size: 12px; line-height: 1.7">
          文件服务部署在公司内网，请连接公司网络（或公司 VPN）后再试。
          <div style="margin-top: 2px">
            当前接口地址：<code>{{ info.base_url }}</code>
          </div>
          <div v-if="failure" style="word-break: break-all">错误详情：{{ failure }}</div>
          <n-button
            size="tiny"
            quaternary
            type="primary"
            :loading="probing"
            style="margin-top: 4px"
            @click="probe"
          >
            重新检测
          </n-button>
        </div>
      </n-alert>

      <n-form @submit.prevent="onSubmit">
        <n-form-item label="工号" path="employeeNo">
          <n-input
            v-model:value="employeeNo"
            placeholder="请输入工号"
            size="large"
            :input-props="{ autocomplete: 'username' }"
            @keyup.enter="onSubmit"
          >
            <template #prefix>
              <n-icon><person-outline /></n-icon>
            </template>
          </n-input>
        </n-form-item>

        <n-form-item label="密码" path="password">
          <n-input
            v-model:value="password"
            type="password"
            show-password-on="click"
            placeholder="请输入密码"
            size="large"
            :input-props="{ autocomplete: 'current-password' }"
            @keyup.enter="onSubmit"
          >
            <template #prefix>
              <n-icon><lock-closed-outline /></n-icon>
            </template>
          </n-input>
        </n-form-item>

        <n-button
          type="primary"
          size="large"
          block
          :loading="loading"
          attr-type="submit"
          style="margin-top: 6px"
        >
          登录
        </n-button>
      </n-form>

      <n-text depth="3" style="font-size: 12px; display: block; margin-top: 14px; text-align: center">
        账号由管理员统一创建；忘记密码请联系管理员重置
      </n-text>
      <n-text
        v-if="reachable === true"
        depth="3"
        style="font-size: 11px; display: block; margin-top: 6px; text-align: center"
      >
        内网服务已连通（{{ info.base_url }}）
      </n-text>
    </n-card>
  </div>
</template>

<style scoped>
.login-page {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #eef4ff 0%, #f7f9fc 60%, #eaf1ff 100%);
}

.login-card {
  width: 400px;
  max-width: 100%;
  box-shadow: 0 10px 40px rgba(31, 111, 235, 0.12);
  border-radius: 12px;
}

code {
  background: #f2f3f5;
  padding: 1px 5px;
  border-radius: 4px;
  font-size: 11px;
}
</style>
