<script setup lang="ts">
// 文件与回收站：管理员查看全部文件（含已标记删除），可恢复或彻底删除。
import { computed, onMounted, ref } from 'vue'
import { NCard, NIcon, NRadioButton, NRadioGroup, NSpace, NTag, NText, useMessage } from 'naive-ui'
import { TrashOutline } from '@vicons/ionicons5'
import { adminListFiles, errMsg, listOwners } from '@/api'
import type { FileItem, OwnerAggregate } from '@/api/types'
import FileTable from '@/components/FileTable.vue'

const message = useMessage()

const items = ref<FileItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const keyword = ref('')
const status = ref<'active' | 'trashed' | 'all'>('active')
const loading = ref(false)
const owners = ref<OwnerAggregate[]>([])
const ownerId = ref<number | null>(null)

const ownerOptions = computed(() => [
  { label: '全部用户', value: 0 },
  ...owners.value.map((o) => ({ label: `${o.name}（${o.employee_no}）`, value: o.user_id }))
])

async function load() {
  loading.value = true
  try {
    const res = await adminListFiles({
      status: status.value,
      owner_id: ownerId.value || undefined,
      q: keyword.value.trim() || undefined,
      page: page.value,
      page_size: pageSize.value
    })
    items.value = res.items
    total.value = res.total
  } catch (e) {
    message.error(errMsg(e))
  } finally {
    loading.value = false
  }
}

async function loadOwners() {
  try {
    owners.value = (await listOwners()).items
  } catch {
    /* 目录树拉取失败不影响主列表 */
  }
}

function refresh() {
  load()
  loadOwners()
}

onMounted(refresh)
</script>

<template>
  <n-space vertical :size="14">
    <n-card :bordered="false" size="small" style="border-radius: 8px">
      <template #header>
        <n-space justify="space-between" align="center" style="width: 100%" :wrap="false">
          <n-space align="center" :size="8">
            <n-icon color="#d03050"><trash-outline /></n-icon>
            <n-text strong style="font-size: 16px">文件与回收站</n-text>
            <n-tag v-if="status === 'trashed'" size="small" type="error" :bordered="false">
              {{ total }} 个待清理
            </n-tag>
          </n-space>
          <n-space :size="10" align="center">
            <n-radio-group
              v-model:value="status"
              size="small"
              @update:value="
                () => {
                  page = 1
                  load()
                }
              "
            >
              <n-radio-button value="active">有效文件</n-radio-button>
              <n-radio-button value="trashed">回收站</n-radio-button>
              <n-radio-button value="all">全部</n-radio-button>
            </n-radio-group>
            <select
              v-model.number="ownerId"
              class="owner-select"
              @change="
                () => {
                  page = 1
                  load()
                }
              "
            >
              <option v-for="o in ownerOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </n-space>
        </n-space>
      </template>

      <n-text depth="3" style="font-size: 12px; display: block; margin-bottom: 8px">
        有效文件到期后会自动进入回收站（对普通用户不可见），回收站保留期结束后由清理任务彻底删除磁盘文件与数据库记录。
      </n-text>

      <FileTable
        v-model:keyword="keyword"
        :items="items"
        :loading="loading"
        :total="total"
        :page="page"
        :page-size="pageSize"
        :show-expiry="false"
        admin-mode
        @update:page="
          (v: number) => {
            page = v
            load()
          }
        "
        @update:page-size="
          (v: number) => {
            pageSize = v
            page = 1
            load()
          }
        "
        @refresh="load"
      />
    </n-card>
  </n-space>
</template>

<style scoped>
.owner-select {
  height: 28px;
  border: 1px solid #e0e0e6;
  border-radius: 4px;
  padding: 0 6px;
  font-size: 13px;
  color: #333;
  background: #fff;
  max-width: 200px;
}
</style>
