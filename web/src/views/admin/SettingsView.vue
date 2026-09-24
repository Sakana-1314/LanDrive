<script setup lang="ts">
// 系统管理：上传策略（体积 / 类型 / 天数 / 分片 / 开关）+ 存储一致性检查。
//
// 为什么把一致性检查放在这里：那是低频运维动作，和"改配置"同属系统维护，
// 放在工作台会占掉指标的位置；两者都需要管理员权限，放一起也更顺手。
//
// 文案取舍：不在每个输入框后面跟小字解释范围 —— 范围与单位写进控件本身。
// 只有「两个天数的先后关系」靠标签看不出顺序，才用一行到期流程示意。
import { computed, onMounted, ref } from 'vue'
import {
  NButton,
  NCard,
  NDivider,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NInputNumber,
  NList,
  NListItem,
  NTag,
  NThing,
  NSwitch,
  useMessage
} from 'naive-ui'
import { CloudDoneOutline } from '@vicons/ionicons5'
import { adminScanStorage, adminSettings, adminUpdateSettings, errMsg, uploadConfig } from '@/api'
import { applyUploadConfig } from '@/stores/user'
import type { OrphanReport, Settings } from '@/api/types'
import { formatTime } from '@/utils/format'
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

// --- 存储一致性检查 ---

const scanning = ref(false)
const report = ref<OrphanReport | null>(null)

async function scan() {
  scanning.value = true
  try {
    report.value = await adminScanStorage()
    if (reportTotal.value === 0) message.success('数据库与磁盘完全一致')
    else message.warning(`发现 ${reportTotal.value} 处不一致`)
  } catch (e) {
    message.error(errMsg(e))
  } finally {
    scanning.value = false
  }
}

/** 扫描结果汇总：三项全 0 即健康。 */
const reportTotal = computed(() => {
  if (!report.value) return 0
  return report.value.orphans.length + report.value.missing.length + report.value.invalid.length
})

onMounted(load)
</script>

<template>
  <div class="settings-page">
    <n-card class="card-surface settings-card" :bordered="false" :loading="loading">
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

    <!-- 存储一致性：数据库记录与磁盘文件是否对应 -->
    <n-card class="card-surface settings-card" :bordered="false">
      <div class="section-head">
        <div class="section-head__main">
          <div class="section-head__title">
            <n-icon color="var(--color-primary)" :size="18"><cloud-done-outline /></n-icon>
            存储一致性
          </div>
        </div>
        <div class="section-head__actions">
          <n-button type="primary" :loading="scanning" @click="scan">扫描</n-button>
        </div>
      </div>

      <div v-if="report" class="scan-result">
        <n-tag :type="reportTotal === 0 ? 'success' : 'warning'" :bordered="false" size="small">
          {{ reportTotal === 0 ? '数据库与磁盘一致' : `发现 ${reportTotal} 处差异` }}
        </n-tag>
        <n-tag :type="report.orphans.length ? 'warning' : 'success'" :bordered="false" size="small">
          孤儿 {{ report.orphans.length }}
        </n-tag>
        <n-tag :type="report.missing.length ? 'error' : 'success'" :bordered="false" size="small">
          缺失 {{ report.missing.length }}
        </n-tag>
        <n-tag :type="report.invalid.length ? 'error' : 'success'" :bordered="false" size="small">
          非法路径 {{ report.invalid.length }}
        </n-tag>
        <span class="scan-time">{{ formatTime(report.scanned_at, true) }}</span>
      </div>

      <n-list v-if="report && reportTotal > 0" bordered class="scan-list">
        <n-list-item v-for="p in report.orphans.slice(0, 50)" :key="'o' + p">
          <n-thing :title="p">
            <template #description>
              <n-tag size="small" type="warning" :bordered="false">孤儿文件</n-tag>
            </template>
          </n-thing>
        </n-list-item>
        <n-list-item v-for="p in report.missing.slice(0, 50)" :key="'m' + p">
          <n-thing :title="p">
            <template #description>
              <n-tag size="small" type="error" :bordered="false">磁盘缺失</n-tag>
            </template>
          </n-thing>
        </n-list-item>
        <n-list-item v-for="p in report.invalid.slice(0, 50)" :key="'i' + p">
          <n-thing :title="p">
            <template #description>
              <n-tag size="small" type="error" :bordered="false">非法路径</n-tag>
            </template>
          </n-thing>
        </n-list-item>
      </n-list>
    </n-card>
  </div>
</template>

<style scoped>
.settings-page {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  min-width: 0;
  gap: var(--space-md);
}

.settings-card {
  max-width: 720px;
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
  margin-top: var(--space-md);
}

.scan-result {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.scan-time {
  color: var(--color-text-muted);
  font-size: 12px;
}

.scan-list {
  margin-top: var(--space-md);
}

@media (max-width: 768px) {
  .num-input {
    width: 100%;
  }
}
</style>
