<script setup lang="ts">
// 系统配置：单文件体积上限、允许的文件类型、保留天数、分片大小、上传开关。
import { computed, onMounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NDivider,
  NForm,
  NFormItem,
  NInputNumber,
  NSpace,
  NSwitch,
  NTag,
  NText,
  useMessage
} from 'naive-ui'
import { adminSettings, adminUpdateSettings, errMsg, uploadConfig } from '@/api'
import { applyUploadConfig, state as userState } from '@/stores/user'
import type { Settings } from '@/api/types'

const message = useMessage()

const form = ref<Settings>({
  max_file_size_mb: 500,
  allowed_extensions: '',
  retention_days: 15,
  trash_days: 7,
  chunk_size_mb: 4,
  upload_enabled: true
})
const loading = ref(false)
const saving = ref(false)

/** 允许类型为空 = 允许全部。 */
const allowAll = ref(true)

function syncAllowAll() {
  allowAll.value = form.value.allowed_extensions.trim() === ''
}

async function load() {
  loading.value = true
  try {
    form.value = await adminSettings()
    syncAllowAll()
  } catch (e) {
    message.error(errMsg(e))
  } finally {
    loading.value = false
  }
}

async function save() {
  // 前端先做一遍范围校验，减少无效请求。
  if (form.value.max_file_size_mb < 1 || form.value.max_file_size_mb > 102400) {
    message.warning('单文件体积上限需在 1 - 102400 MB 之间')
    return
  }
  if (form.value.retention_days < 1 || form.value.retention_days > 3650) {
    message.warning('保留天数需在 1 - 3650 之间')
    return
  }
  if (form.value.trash_days < 0 || form.value.trash_days > 365) {
    message.warning('回收站保留天数需在 0 - 365 之间')
    return
  }
  if (form.value.chunk_size_mb < 1 || form.value.chunk_size_mb > 64) {
    message.warning('分片大小需在 1 - 64 MB 之间')
    return
  }
  saving.value = true
  try {
    const next = await adminUpdateSettings({
      max_file_size_mb: form.value.max_file_size_mb,
      allowed_extensions: allowAll.value ? '' : form.value.allowed_extensions,
      retention_days: form.value.retention_days,
      trash_days: form.value.trash_days,
      chunk_size_mb: form.value.chunk_size_mb,
      upload_enabled: form.value.upload_enabled
    })
    form.value = next
    syncAllowAll()
    // 立即刷新本地缓存，让本页面的上传校验用上新策略。
    applyUploadConfig(await uploadConfig())
    message.success('配置已保存并立即生效')
  } catch (e) {
    message.error(errMsg(e))
  } finally {
    saving.value = false
  }
}

const presets = computed(() => [
  { label: '仅文档与图片', value: 'pdf,doc,docx,xls,xlsx,ppt,pptx,txt,csv,png,jpg,jpeg,gif,webp,bmp' },
  { label: '仅图片', value: 'png,jpg,jpeg,gif,webp,bmp' },
  { label: '压缩包与文档', value: 'zip,rar,7z,tar,gz,pdf,doc,docx,xls,xlsx,ppt,pptx' }
])

function applyPreset(v: string) {
  form.value.allowed_extensions = v
  allowAll.value = false
}

const extPreview = computed(() => {
  const raw = form.value.allowed_extensions.trim()
  if (!raw) return []
  return raw
    .split(/[,;，、\s]+/)
    .filter(Boolean)
    .map((s) => (s.startsWith('.') ? s.toLowerCase() : '.' + s.toLowerCase()))
})

onMounted(load)
</script>

<template>
  <n-space vertical :size="14" style="max-width: 860px">
    <n-card :bordered="false" size="small" style="border-radius: 8px" :loading="loading">
      <template #header>
        <n-space align="center" :size="8">
          <n-text strong style="font-size: 16px">系统配置</n-text>
          <n-tag size="small" type="info" :bordered="false">保存后立即生效</n-tag>
        </n-space>
      </template>

      <n-form label-placement="left" label-width="150">
        <n-form-item label="单文件最大体积">
          <n-space align="center" :size="8">
            <n-input-number v-model:value="form.max_file_size_mb" :min="1" :max="102400" style="width: 160px">
              <template #suffix>MB</template>
            </n-input-number>
            <n-text depth="3" style="font-size: 12px">
              上传前即校验，超出会直接拒绝（1 - 102400 MB）
            </n-text>
          </n-space>
        </n-form-item>

        <n-form-item label="允许上传的文件类型">
          <n-space vertical :size="8" style="width: 100%">
            <n-space align="center" :size="8">
              <n-switch v-model:value="allowAll" @update:value="allowAll && (form.allowed_extensions = '')" />
              <n-text>{{ allowAll ? '允许所有文件类型（默认）' : '只允许指定类型' }}</n-text>
            </n-space>

            <template v-if="!allowAll">
              <n-input
                v-model:value="form.allowed_extensions"
                type="textarea"
                :rows="2"
                placeholder="逗号分隔，例如：pdf,docx,xlsx,png,jpg；留空表示允许全部"
              />
              <n-space :size="6" align="center">
                <n-text depth="3" style="font-size: 12px">常用预设：</n-text>
                <n-button v-for="p in presets" :key="p.label" size="tiny" quaternary @click="applyPreset(p.value)">
                  {{ p.label }}
                </n-button>
              </n-space>
              <n-space v-if="extPreview.length" :size="4">
                <n-tag v-for="e in extPreview.slice(0, 40)" :key="e" size="tiny" :bordered="false">{{ e }}</n-tag>
                <n-text v-if="extPreview.length > 40" depth="3" style="font-size: 12px">
                  等 {{ extPreview.length }} 种
                </n-text>
              </n-space>
            </template>
          </n-space>
        </n-form-item>

        <n-divider style="margin: 4px 0 16px" />

        <n-form-item label="文件保留天数">
          <n-space align="center" :size="8">
            <n-input-number v-model:value="form.retention_days" :min="1" :max="3650" style="width: 160px">
              <template #suffix>天</template>
            </n-input-number>
            <n-text depth="3" style="font-size: 12px">
              上传后经过该天数，文件自动删除（对普通用户不可见），默认 15 天
            </n-text>
          </n-space>
        </n-form-item>

        <n-form-item label="回收站保留天数">
          <n-space align="center" :size="8">
            <n-input-number v-model:value="form.trash_days" :min="0" :max="365" style="width: 160px">
              <template #suffix>天</template>
            </n-input-number>
            <n-text depth="3" style="font-size: 12px">
              自动删除后继续保留的天数，期间管理员可恢复；0 表示到期立即彻底删除。默认 7 天
            </n-text>
          </n-space>
        </n-form-item>

        <n-form-item label="分片大小">
          <n-space align="center" :size="8">
            <n-input-number v-model:value="form.chunk_size_mb" :min="1" :max="64" style="width: 160px">
              <template #suffix>MB</template>
            </n-input-number>
            <n-text depth="3" style="font-size: 12px">
              大文件分片上传的分片尺寸（1 - 64 MB）。修改后新的上传任务生效，进行中的任务不受影响
            </n-text>
          </n-space>
        </n-form-item>

        <n-form-item label="允许上传">
          <n-space align="center" :size="8">
            <n-switch v-model:value="form.upload_enabled" />
            <n-text depth="3" style="font-size: 12px">
              关闭后所有用户都无法上传新文件（已上传的文件不受影响），可用于维护期
            </n-text>
          </n-space>
        </n-form-item>
      </n-form>

      <n-alert type="info" :show-icon="false" style="margin-top: 6px">
        到期策略当前为：上传后 {{ form.retention_days }} 天自动删除（进入回收站），回收站保留
        {{ form.trash_days }} 天后彻底删除磁盘文件与数据库记录。清理任务每 5 - 10 分钟执行一次。
      </n-alert>

      <n-space justify="end" style="margin-top: 14px">
        <n-button @click="load">放弃修改</n-button>
        <n-button type="primary" :loading="saving" @click="save">保存配置</n-button>
      </n-space>
    </n-card>

    <n-card :bordered="false" size="small" style="border-radius: 8px">
      <template #header>
        <n-text strong>当前生效配置</n-text>
      </template>
      <n-space vertical :size="6">
        <n-text depth="3" style="font-size: 12px">
          单文件上限：{{ userState.settings?.max_file_size_mb ?? form.max_file_size_mb }} MB ·
          允许类型：{{ (userState.settings?.allowed_extensions || '') === '' ? '全部' : userState.settings?.allowed_extensions }} ·
          保留：{{ userState.settings?.retention_days ?? form.retention_days }} 天 ·
          回收站：{{ userState.settings?.trash_days ?? form.trash_days }} 天 ·
          分片：{{ userState.settings?.chunk_size_mb ?? form.chunk_size_mb }} MB ·
          上传开关：{{ (userState.settings?.upload_enabled ?? form.upload_enabled) ? '开启' : '关闭' }}
        </n-text>
      </n-space>
    </n-card>
  </n-space>
</template>
