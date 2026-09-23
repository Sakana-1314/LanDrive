<script setup lang="ts">
// 全部文件 / 我的文件：左边人员筛选，右边文件列表。
//
// 「全部文件」可以浏览并下载所有人的文件；「我的文件」只列自己的，
// 且只有自己能改名或删除（权限由后端的 can_edit 决定）。
//
// 移动端不显示左侧人员栏（窄屏放不下），改为下拉筛选。
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { NCard, NIcon, NSelect, NTag, useMessage } from 'naive-ui'
import { FolderOutline } from '@vicons/ionicons5'
import { errMsg, listFiles, listOwners } from '@/api'
import type { FileItem, OwnerAggregate } from '@/api/types'
import FileTable from '@/components/FileTable.vue'
import { formatBytes } from '@/utils/format'
import { useIsMobile } from '@/utils/themeState'

const route = useRoute()
const message = useMessage()
const isMobile = useIsMobile()

const scope = computed<'all' | 'mine'>(() => (route.meta.scope === 'mine' ? 'mine' : 'all'))

const items = ref<FileItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const keyword = ref('')
const sort = ref('created_at')
const order = ref<'asc' | 'desc'>('desc')
const loading = ref(false)
const owners = ref<OwnerAggregate[]>([])

/** 当前选中的人员：null = 全部。 */
const selectedOwner = ref<number | null>(null)

const totalFiles = computed(() => owners.value.reduce((s, o) => s + o.file_count, 0))

/** 人员下拉（移动端）与侧栏（桌面端）共用同一份选项。 */
const ownerOptions = computed(() => [
  { label: `全部人员（${totalFiles.value}）`, value: 0 },
  ...owners.value.map((o) => ({
    label: `${o.name} · ${o.file_count} 个 · ${formatBytes(o.used_bytes)}`,
    value: o.user_id
  }))
])

const selectedOwnerKey = computed({
  get: () => selectedOwner.value ?? 0,
  set: (v: number) => {
    selectedOwner.value = v === 0 ? null : v
    onOwnerChange()
  }
})

function onOwnerChange() {
  page.value = 1
  load()
}

async function loadOwners() {
  try {
    const res = await listOwners()
    owners.value = res.items
  } catch (e) {
    message.error(errMsg(e))
  }
}

async function load() {
  loading.value = true
  try {
    const res = await listFiles({
      scope: scope.value,
      owner_id: scope.value === 'mine' ? undefined : selectedOwner.value || undefined,
      q: keyword.value.trim() || undefined,
      page: page.value,
      page_size: pageSize.value,
      sort: sort.value,
      order: order.value
    })
    items.value = res.items
    total.value = res.total
  } catch (e) {
    message.error(errMsg(e))
  } finally {
    loading.value = false
  }
}

function refresh() {
  load()
  if (scope.value === 'all') loadOwners()
}

function onSortChange(p: { sort: string; order: 'asc' | 'desc' }) {
  sort.value = p.sort
  order.value = p.order
  load()
}

watch(
  () => route.fullPath,
  () => {
    keyword.value = ''
    selectedOwner.value = null
    page.value = 1
    refresh()
  }
)

watch([page, pageSize], () => load())

onMounted(() => {
  refresh()
})
</script>

<template>
  <div class="files-page">
    <!-- 桌面端：左侧人员列表。移动端改由下方下拉选择（窄屏放不下侧栏）。 -->
    <aside v-if="scope === 'all' && !isMobile" class="owner-panel">
      <div class="owner-panel__head">
        <n-icon color="var(--color-primary)" :size="18"><folder-outline /></n-icon>
        <span>人员目录</span>
      </div>
      <ul class="owner-list">
        <li>
          <button
            type="button"
            class="owner-item"
            :class="{ active: selectedOwner === null }"
            @click="
              () => {
                selectedOwner = null
                onOwnerChange()
              }
            "
          >
            <span class="owner-item__name">全部人员</span>
            <span class="owner-item__meta">{{ totalFiles }} 个文件</span>
          </button>
        </li>
        <li v-for="o in owners" :key="o.user_id">
          <button
            type="button"
            class="owner-item"
            :class="{ active: selectedOwner === o.user_id }"
            @click="
              () => {
                selectedOwner = o.user_id
                onOwnerChange()
              }
            "
          >
            <span class="owner-item__name">{{ o.name }}</span>
            <span class="owner-item__meta">{{ o.file_count }} 个 · {{ formatBytes(o.used_bytes) }}</span>
          </button>
        </li>
        <li v-if="!owners.length" class="owner-empty">还没有人上传文件</li>
      </ul>
    </aside>

    <n-card class="files-card" :bordered="false">
      <div class="section-head">
        <div class="section-head__title">
          <n-tag size="small" :bordered="false" :type="scope === 'mine' ? 'info' : 'default'">
            {{ scope === 'mine' ? '仅我可修改' : '均可下载' }}
          </n-tag>
        </div>
      </div>

      <!-- 移动端：人员筛选收成一行下拉 -->
      <n-select
        v-if="scope === 'all' && isMobile && owners.length"
        v-model:value="selectedOwnerKey"
        class="owner-select"
        :options="ownerOptions"
        size="medium"
      />

      <FileTable
        v-model:keyword="keyword"
        :items="items"
        :loading="loading"
        :total="total"
        :page="page"
        :page-size="pageSize"
        @update:page="(v: number) => (page = v)"
        @update:page-size="(v: number) => (pageSize = v)"
        @refresh="load"
        @sort="onSortChange"
      />
    </n-card>
  </div>
</template>

<style scoped>
.files-page {
  display: flex;
  align-items: flex-start;
  gap: 14px;
}

.owner-panel {
  flex: none;
  width: 240px;
  max-height: calc(100vh - var(--header-height) - 60px);
  overflow-y: auto;
  border: 1px solid var(--color-border-subtle);
  border-radius: var(--radius-card);
  background: var(--color-surface);
  box-shadow: var(--shadow-card);
}

.owner-panel__head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 14px 14px 10px;
  color: var(--color-text-strong);
  font-size: 14px;
  font-weight: 650;
}

.owner-list {
  margin: 0;
  padding: 0 8px 10px;
  list-style: none;
}

.owner-item {
  display: grid;
  gap: 2px;
  width: 100%;
  padding: 8px 10px;
  border: none;
  border-radius: var(--radius-control);
  color: inherit;
  text-align: left;
  background: transparent;
  cursor: pointer;
}

.owner-item:hover {
  background: var(--color-primary-soft);
}

.owner-item.active {
  background: var(--color-primary-soft);
  box-shadow: inset 2px 0 0 var(--color-primary);
}

.owner-item__name {
  overflow: hidden;
  color: var(--color-text-strong);
  font-size: 14px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.owner-item__meta {
  color: var(--color-text-muted);
  font-size: 12px;
}

.owner-empty {
  padding: 10px;
  color: var(--color-text-muted);
  font-size: 13px;
}

.files-card {
  flex: 1;
  min-width: 0;
}

.owner-select {
  margin-top: 12px;
}

/* 移动端：内容区占满宽度 */
@media (max-width: 768px) {
  .files-page {
    display: block;
  }
}
</style>
