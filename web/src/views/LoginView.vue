<script setup lang="ts">
// 登录页：工号 + 密码。
//
// 设计取舍：页面上不写「账号由管理员创建」「忘记密码联系管理员」这类说明文字 ——
// 用户在登录框前自然会尝试，遇到错误时再给出针对性提示即可。
// 只有「网络不可达」这一种情况必须显式说明（否则会被误认为密码错误）。
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NAlert, NButton, NForm, NIcon, NInput, useMessage } from 'naive-ui'
import { LockClosedOutline, PersonOutline, RefreshOutline } from '@vicons/ionicons5'
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
const failure = ref('')
const info = apiEndpointInfo()

/** 仅在出问题时展示接口地址，正常登录时不显示这行技术信息。 */
const showEndpoint = computed(() => reachable.value === false)

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
    // 登录后的落点：优先回跳原地址（守卫带过来的 ?redirect=），
    // 否则进「我的文件」——「全部人员混合视图」已下线，/files 只是按人查看的容器。
    const redirect = (route.query.redirect as string) || '/files/mine'
    router.replace(redirect)
  } catch (e) {
    failure.value = errMsg(e)
    if (isNetworkError(e)) {
      // 网络类失败：明确告知需更换网络，而不是让用户误以为密码错误。
      reachable.value = false
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
    <div class="login-card">
      <div class="brand-block">
        <div class="brand-mark">
          <img src="/logo.png" alt="局域网文件助手" width="54" height="54" />
        </div>
        <h1 class="brand-name">局域网文件助手</h1>
      </div>

      <!-- 网络不可达是唯一需要解释的情况：用户看到的不能是「密码错误」 -->
      <n-alert v-if="reachable === false" type="warning" class="net-alert" :show-icon="false">
        <div class="net-alert__title">无法在此网络下使用，请更换网络再试！</div>
        <div class="net-alert__hint">请连接公司网络或公司 VPN 后重试。</div>
        <div v-if="showEndpoint" class="net-alert__endpoint">{{ info.base_url }}</div>
        <div v-if="failure" class="net-alert__endpoint">{{ failure }}</div>
        <n-button size="small" quaternary type="primary" :loading="probing" @click="probe">
          <template #icon>
            <n-icon><refresh-outline /></n-icon>
          </template>
          重新检测
        </n-button>
      </n-alert>

      <n-form class="login-form" @submit.prevent="onSubmit">
        <n-input
          v-model:value="employeeNo"
          placeholder="工号"
          size="large"
          :input-props="{ autocomplete: 'username', 'aria-label': '工号' }"
          @keyup.enter="onSubmit"
        >
          <template #prefix>
            <n-icon><person-outline /></n-icon>
          </template>
        </n-input>

        <n-input
          v-model:value="password"
          type="password"
          show-password-on="click"
          placeholder="密码"
          size="large"
          :input-props="{ autocomplete: 'current-password', 'aria-label': '密码' }"
          @keyup.enter="onSubmit"
        >
          <template #prefix>
            <n-icon><lock-closed-outline /></n-icon>
          </template>
        </n-input>

        <n-button type="primary" size="large" block :loading="loading" attr-type="submit">
          登录
        </n-button>
      </n-form>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  display: grid;
  min-height: 100dvh;
  padding: 24px;
  place-items: center;
  background: var(--page-glow), var(--color-bg);
}

.login-card {
  width: 100%;
  max-width: 400px;
  padding: 30px 28px 32px;
  border: 1px solid var(--color-border-subtle);
  border-radius: 18px;
  background: var(--color-surface);
  box-shadow: var(--shadow-float);
}

.brand-block {
  display: grid;
  gap: 12px;
  margin-bottom: 24px;
  justify-items: center;
}

/* 新 Logo 的圆角已直接做进图片像素（透明圆角），此处只负责尺寸，
   不要再加 border-radius —— 那是给图片又套一层遮罩，会切出双圆角。 */
.brand-mark {
  display: grid;
  width: 54px;
  height: 54px;
  place-items: center;
}

.brand-mark img {
  display: block;
  width: 100%;
  height: 100%;
}

.brand-name {
  margin: 0;
  color: var(--color-text-strong);
  font-size: 20px;
  font-weight: 650;
  letter-spacing: 0.2px;
}

.net-alert {
  margin-bottom: 18px;
}

.net-alert__title {
  color: var(--color-text-strong);
  font-size: 14px;
  font-weight: 600;
}

.net-alert__hint {
  margin-top: 4px;
  font-size: 13px;
}

.net-alert__endpoint {
  margin-top: 6px;
  color: var(--color-text-muted);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  word-break: break-all;
}

.login-form {
  display: grid;
  gap: 14px;
}
</style>
