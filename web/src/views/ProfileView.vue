<script setup lang="ts">
// 个人设置：账号信息 + 修改密码。
//
// 文案取舍：去掉「你的文件只在你的目录、他人可下载但不可修改」这段说明 ——
// 权限差异在文件列表里由「仅我可修改 / 均可下载」标签直接体现，
// 这里重复一遍只是噪音。
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NTag,
  useMessage
} from 'naive-ui'
import { KeyOutline, PersonOutline } from '@vicons/ionicons5'
import { changePassword, clearToken, errMsg } from '@/api'
import { clearUser, isAdmin, state as userState } from '@/stores/user'
import { formatBytes, formatTime } from '@/utils/format'

const router = useRouter()
const message = useMessage()

const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const saving = ref(false)

const isAdminUser = computed(() => isAdmin())

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
  <div class="profile-page">
    <n-card class="card-surface" :bordered="false">
      <div class="section-head">
        <div class="section-head__title">
          <n-icon color="var(--color-primary)" :size="18"><person-outline /></n-icon>
          账号信息
        </div>
        <n-tag size="small" :type="isAdminUser ? 'warning' : 'default'" :bordered="false">
          {{ isAdminUser ? '管理员' : '普通用户' }}
        </n-tag>
      </div>

      <n-descriptions class="info-grid" :column="2" label-placement="top" size="small">
        <n-descriptions-item label="姓名">{{ userState.user?.name }}</n-descriptions-item>
        <n-descriptions-item label="工号">{{ userState.user?.employee_no }}</n-descriptions-item>
        <n-descriptions-item label="我的用量">
          {{ userState.user?.file_count }} 个文件 · {{ formatBytes(userState.user?.used_bytes || 0) }}
        </n-descriptions-item>
        <n-descriptions-item label="账号状态">
          <n-tag size="small" :type="userState.user?.enabled ? 'success' : 'error'" :bordered="false">
            {{ userState.user?.enabled ? '正常' : '已停用' }}
          </n-tag>
        </n-descriptions-item>
        <n-descriptions-item label="最后登录">
          {{ formatTime(userState.user?.last_login_at, true) }}
        </n-descriptions-item>
        <n-descriptions-item label="创建时间">
          {{ formatTime(userState.user?.created_at) }}
        </n-descriptions-item>
      </n-descriptions>
    </n-card>

    <n-card class="card-surface" :bordered="false">
      <div class="section-head">
        <div class="section-head__title">
          <n-icon color="var(--color-primary)" :size="18"><key-outline /></n-icon>
          修改密码
        </div>
      </div>

      <n-form class="pwd-form" label-placement="top" @submit.prevent="onSubmit">
        <n-form-item label="原密码">
          <n-input
            v-model:value="oldPassword"
            type="password"
            show-password-on="click"
            placeholder="当前密码"
            :input-props="{ autocomplete: 'current-password' }"
          />
        </n-form-item>
        <n-form-item label="新密码">
          <n-input
            v-model:value="newPassword"
            type="password"
            show-password-on="click"
            placeholder="至少 6 位"
            :input-props="{ autocomplete: 'new-password' }"
          />
        </n-form-item>
        <n-form-item label="确认新密码">
          <n-input
            v-model:value="confirmPassword"
            type="password"
            show-password-on="click"
            placeholder="再次输入新密码"
            :input-props="{ autocomplete: 'new-password' }"
            @keyup.enter="onSubmit"
          />
        </n-form-item>
        <n-button type="primary" :loading="saving" attr-type="submit">
          保存并重新登录
        </n-button>
      </n-form>
    </n-card>
  </div>
</template>

<style scoped>
.profile-page {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 14px;
  max-width: 760px;
}

.info-grid {
  margin-top: 16px;
}

/* 修改密码：桌面三列、移动单列 */
.pwd-form {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 0 16px;
  margin-top: 12px;
}

.pwd-form :deep(.n-form-item) {
  margin-bottom: 12px;
}

.pwd-form > .n-button {
  grid-column: 1 / -1;
  justify-self: start;
}

@media (max-width: 768px) {
  .pwd-form {
    grid-template-columns: 1fr;
  }
}
</style>
