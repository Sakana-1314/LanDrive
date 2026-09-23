<script setup lang="ts">
// 个人设置：查看本人信息、修改密码。
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  NAlert,
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NForm,
  NFormItem,
  NInput,
  NSpace,
  NTag,
  NText,
  useMessage
} from 'naive-ui'
import { changePassword, clearToken, errMsg } from '@/api'
import { clearUser, state as userState } from '@/stores/user'
import { formatBytes, formatTime } from '@/utils/format'

const router = useRouter()
const message = useMessage()

const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const saving = ref(false)

async function onSubmit() {
  if (!oldPassword.value || !newPassword.value) {
    message.warning('请填写原密码与新密码')
    return
  }
  if (newPassword.value.length < 6) {
    message.warning('新密码长度至少 6 位')
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    message.warning('两次输入的新密码不一致')
    return
  }
  saving.value = true
  try {
    await changePassword(oldPassword.value, newPassword.value)
    message.success('密码已修改，请重新登录')
    clearToken()
    clearUser()
    router.push('/login')
  } catch (e) {
    message.error(errMsg(e))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <n-space vertical :size="14" style="max-width: 820px">
    <n-card :bordered="false" size="small" style="border-radius: 8px">
      <template #header>
        <n-text strong style="font-size: 16px">账号信息</n-text>
      </template>
      <n-descriptions :column="2" label-placement="left" bordered size="small">
        <n-descriptions-item label="姓名">{{ userState.user?.name }}</n-descriptions-item>
        <n-descriptions-item label="工号">{{ userState.user?.employee_no }}</n-descriptions-item>
        <n-descriptions-item label="角色">
          <n-tag size="small" :type="userState.user?.role === 'admin' ? 'warning' : 'default'" :bordered="false">
            {{ userState.user?.role === 'admin' ? '管理员' : '普通用户' }}
          </n-tag>
        </n-descriptions-item>
        <n-descriptions-item label="账号状态">
          <n-tag size="small" :type="userState.user?.enabled ? 'success' : 'error'" :bordered="false">
            {{ userState.user?.enabled ? '正常' : '已停用' }}
          </n-tag>
        </n-descriptions-item>
        <n-descriptions-item label="我的目录">
          <n-text code>{{ userState.user?.dir_rel }}</n-text>
        </n-descriptions-item>
        <n-descriptions-item label="我的用量">
          {{ userState.user?.file_count }} 个文件 · {{ formatBytes(userState.user?.used_bytes || 0) }}
        </n-descriptions-item>
        <n-descriptions-item label="最后登录">
          {{ formatTime(userState.user?.last_login_at, true) }}
        </n-descriptions-item>
        <n-descriptions-item label="创建时间">
          {{ formatTime(userState.user?.created_at) }}
        </n-descriptions-item>
      </n-descriptions>
      <n-alert type="info" style="margin-top: 12px" :show-icon="false">
        你的文件只保存在自己的目录中；其他同事可以查看和下载你的文件，但不能修改或删除。
      </n-alert>
    </n-card>

    <n-card :bordered="false" size="small" style="border-radius: 8px">
      <template #header>
        <n-text strong style="font-size: 16px">修改密码</n-text>
      </template>
      <n-form label-placement="left" label-width="90">
        <n-form-item label="原密码">
          <n-input v-model:value="oldPassword" type="password" show-password-on="click" placeholder="请输入当前密码" />
        </n-form-item>
        <n-form-item label="新密码">
          <n-input v-model:value="newPassword" type="password" show-password-on="click" placeholder="至少 6 位" />
        </n-form-item>
        <n-form-item label="确认新密码">
          <n-input
            v-model:value="confirmPassword"
            type="password"
            show-password-on="click"
            placeholder="再次输入新密码"
            @keyup.enter="onSubmit"
          />
        </n-form-item>
        <n-form-item label=" ">
          <n-space>
            <n-button type="primary" :loading="saving" @click="onSubmit">保存并重新登录</n-button>
            <n-text depth="3" style="font-size: 12px; line-height: 34px">
              修改成功后所有已登录设备的会话都会失效
            </n-text>
          </n-space>
        </n-form-item>
      </n-form>
    </n-card>
  </n-space>
</template>
