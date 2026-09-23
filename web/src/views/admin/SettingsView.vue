<script setup lang="ts">
// 系统配置：体积上限、允许类型、保留天数、分片大小、上传开关。
//
// 文案取舍：不再在每个输入框后面跟一行小字解释「这是什么、范围多少」——
// 范围与单位写进控件本身，开关的语义由标签与当前值体现。
// 只有「两个天数的先后关系」这种不看图会误解的地方，才用一行到期示意说明。
import { computed, onMounted, ref } from 'vue'
import {
  NButton,
  NCard,
  NDivider,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NSwitch,
  NTag,
  useMessage
} from 'naive-ui'
import { adminSettings, adminUpdateSettings, errMsg, uploadConfig } from '@/api'
import { applyUploadConfig } from '@/stores/user'
import type { Settings } from '@/api/types'
import { useIsMobile } from '@/utils/themeState'

const message = useMessage()
const isMobile = useIsMobile()

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
    message.warning('文件保留天数需在 1 - 3650 之间')
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

const presets = [
  { label: '文档与图片', value: 'pdf,doc,docx,xls,xlsx,ppt,pptx,txt,csv,png,jpg,jpeg,gif,webp,bmp' },
  { label: '仅图片', value: 'png,jpg,jpeg,gif,webp,bmp' },
  { label: '压缩包与文档', value: 'zip,rar,7z,tar,gz,pdf,doc,docx,xls,xlsx,ppt,pptx' }
]

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

/** 到期流程示意：两个天数是先后关系，纯靠标签看不出顺序。 */
const retentionHint = computed(
  () => `上传 ${form.value.retention_days} 天 → 回收站 ${form.value.trash_days} 天 → 彻底删除`
)

onMounted(load)
</script>

<template>
  <n-card class="card-surface settings-card" :bordered="false" :loading="loading">
    <div class="section-head">
      <n-tag size="small" type="info" :bordered="false">保存后立即生效</n-tag>
    </div>

    <n-form
      class="settings-form"
      :label-placement="isMobile ? 'top' : 'left'"
      :label-width="isMobile ? undefined : 132"
    >
      <n-form-item label="单文件上限">
        <n-input-number v-model:value="form.max_file_size_mb" class="num-input" :min="1" :max="102400">
          <template #suffix>MB</template>
        </n-input-number>
      </n-form-item>

      <n-form-item label="允许的文件类型">
        <div class="field-block">
          <div class="switch-row">
            <n-switch
              v-model:value="allowAll"
              @update:value="allowAll && (form.allowed_extensions = '')"
            />
            <span>{{ allowAll ? '不限类型' : '指定类型' }}</span>
          </div>

          <template v-if="!allowAll">
            <n-input
              v-model:value="form.allowed_extensions"
              type="textarea"
              :rows="2"
              placeholder="pdf, docx, xlsx, png…"
            />
            <div class="preset-row">
              <n-button v-for="p in presets" :key="p.label" size="tiny" quaternary @click="applyPreset(p.value)">
                {{ p.label }}
              </n-button>
            </div>
            <div v-if="extPreview.length" class="ext-tags">
              <n-tag v-for="e in extPreview.slice(0, 24)" :key="e" size="small" :bordered="false">
                {{ e }}
              </n-tag>
              <n-tag v-if="extPreview.length > 24" size="small" :bordered="false">
                +{{ extPreview.length - 24 }}
              </n-tag>
            </div>
          </template>
        </div>
      </n-form-item>

      <n-divider class="settings-divider" />

      <n-form-item label="文件保留">
        <n-input-number v-model:value="form.retention_days" class="num-input" :min="1" :max="3650">
          <template #suffix>天</template>
        </n-input-number>
      </n-form-item>

      <n-form-item label="回收站保留">
        <n-input-number v-model:value="form.trash_days" class="num-input" :min="0" :max="365">
          <template #suffix>天</template>
        </n-input-number>
      </n-form-item>

      <n-form-item label="到期流程">
        <n-tag size="small" :bordered="false">{{ retentionHint }}</n-tag>
      </n-form-item>

      <n-form-item label="分片大小">
        <n-input-number v-model:value="form.chunk_size_mb" class="num-input" :min="1" :max="64">
          <template #suffix>MB</template>
        </n-input-number>
      </n-form-item>

      <n-form-item label="允许上传">
        <div class="switch-row">
          <n-switch v-model:value="form.upload_enabled" />
          <span>{{ form.upload_enabled ? '开启' : '暂停' }}</span>
        </div>
      </n-form-item>
    </n-form>

    <div class="form-actions">
      <n-button @click="load">放弃修改</n-button>
      <n-button type="primary" :loading="saving" @click="save">保存</n-button>
    </div>
  </n-card>
</template>

<style scoped>
.settings-card {
  max-width: 720px;
}

.settings-form {
  margin-top: 18px;
}

.settings-divider {
  margin: 4px 0 18px;
}

.num-input {
  width: 150px;
}

.field-block {
  display: grid;
  gap: 10px;
  width: 100%;
}

.switch-row {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--color-text);
  font-size: 14px;
}

.preset-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.ext-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 6px;
}

@media (max-width: 768px) {
  .num-input {
    width: 100%;
  }
}
</style>
