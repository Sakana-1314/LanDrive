<script setup lang="ts">
// 文件与回收站：管理员查看全部文件（含已标记删除），可恢复或彻底删除。
//
// 文案取舍：删掉「有效文件到期后会自动进入回收站…」这段说明 ——
// 「回收站」标签页与「待清理」角标已经把生命周期表达清楚。
import { computed, onMounted, ref } from 'vue'
import { NCard, NRadioButton, NRadioGroup, NSelect, NTag, useMessage } from 'naive-ui'
import { adminListFiles, errMsg, listOwners } from '@/api'
import type { FileItem, OwnerAggregate } from '@/api/types'
import FileTable from '@/components/FileTable.vue'
import { useIsMobile } from '@/utils/themeState'

const message = useMessage()
const isMobile = useIsMobile()

const items = ref<FileItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const keyword = ref('')
const status = ref<'active' | 'trashed' | 'all'>('active')
const loading = ref(false)
const owners = ref<OwnerAggregate[]>([])
const ownerId = ref<number>(0)

const ownerOptions = computed(() => [
  { label: '全部人员', value: 0 },
  ...owners.value.map((o) => ({ label: `${o.name} · ${o.employee_no}`, value: o.user_id }))
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
    /* 人员列表拉取失败不影响主列表 */
  }
}

function refresh() {
  load()
  loadOwners()
}

function onStatusChange() {
  page.value = 1
  load()
}

function onOwnerChange() {
  page.value = 1
  load()
}

onMounted(refresh)
</script>

<template>
  <n-card class="card-surface" :bordered="false">
    <div class="section-head">
      <!-- 左：筛选（状态切换 + 人员）；右侧留白 -->
      <div class="section-head__main">
        <n-tag v-if="status === 'trashed'" size="small" type="error" :bordered="false">
          {{ total }} 待清理
        </n-tag>
        <div class="filters" :class="{ 'filters--mobile': isMobile }">
          <n-radio-group v-model:value="status" @update:value="onStatusChange">
            <n-radio-button value="active">有效</n-radio-button>
            <n-radio-button value="trashed">回收站</n-radio-button>
            <n-radio-button value="all">全部</n-radio-button>
          </n-radio-group>
          <n-select
            v-model:value="ownerId"
            class="owner-filter"
            :options="ownerOptions"
            @update:value="onOwnerChange"
          />
        </div>
      </div>
    </div>

    <div class="table-wrap">
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
    </div>
  </n-card>
</template>

<style scoped>
.filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.owner-filter {
  width: 200px;
}

.table-wrap {
  margin-top: var(--space-md);
}

@media (max-width: 768px) {
  .filters--mobile {
    align-items: stretch;
    flex-direction: column;
    width: 100%;
  }

  .owner-filter {
    width: 100%;
  }
}
</style>
