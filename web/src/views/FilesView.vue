<script setup lang="ts">
// 全部文件 / 我的文件。
//
// 导航方式的变化：人员筛选不再由本页提供 —— 左侧菜单的「全部文件」展开后
// 就是按人的子 tab（还能置顶）。因此这里只按 URL 的 ?owner= 取数，
// 同一份导航在桌面与移动端都成立，不必维护两套筛选 UI。
//
// 「我的文件」上方嵌入上传面板：上传的目标就是自己的目录，
// 拖进来即可，不再需要单独的「上传文件」页面。
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { NCard, useMessage } from 'naive-ui'
import { errMsg, listFiles } from '@/api'
import type { FileItem } from '@/api/types'
import FileTable from '@/components/FileTable.vue'
import UploadPanel from '@/components/UploadPanel.vue'
import { loadOwners, ownersState } from '@/stores/owners'

const route = useRoute()
const message = useMessage()

const scope = computed<'all' | 'mine'>(() => (route.meta.scope === 'mine' ? 'mine' : 'all'))

/** 当前查看的人员目录；来自菜单子 tab 的 ?owner=<id>。 */
const ownerId = computed(() => {
  const v = Number(route.query.owner)
  return Number.isFinite(v) && v > 0 ? v : null
})

/** 列表标题：选中某人时显示其姓名，否则显示全部。 */
const ownerLabel = computed(() => {
  if (scope.value === 'mine') return '我的文件'
  if (ownerId.value === null) return '全部人员'
  const hit = ownersState.items.find((o) => o.user_id === ownerId.value)
  return hit ? hit.name : '全部人员'
})

const items = ref<FileItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const keyword = ref('')
const sort = ref('created_at')
const order = ref<'asc' | 'desc'>('desc')
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const res = await listFiles({
      scope: scope.value,
      owner_id: scope.value === 'mine' ? undefined : ownerId.value || undefined,
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
  // 文件数会影响菜单里的角标，一并刷新（失败静默，不影响列表）
  if (scope.value === 'all') loadOwners(true).catch(() => {})
}

function onSortChange(p: { sort: string; order: 'asc' | 'desc' }) {
  sort.value = p.sort
  order.value = p.order
  load()
}

// 切换菜单子 tab（?owner= 变化）或切到「我的文件」时，重置分页与关键字。
watch(
  () => route.fullPath,
  () => {
    keyword.value = ''
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
    <n-card class="card-surface files-card" :bordered="false">
      <!-- 上传面板只出现在「我的文件」：它上传到的就是自己的目录 -->
      <UploadPanel v-if="scope === 'mine'" class="files-card__upload" @uploaded="refresh" />

      <FileTable
        v-model:keyword="keyword"
        :title="scope === 'all' ? ownerLabel : ''"
        :items="items"
        :loading="loading"
        :total="total"
        :show-owner="scope === 'all'"
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
  display: grid;
  /* minmax(0,1fr)：grid 子项默认 min-width:auto，长文件名会把列撑宽 */
  grid-template-columns: minmax(0, 1fr);
  min-width: 0;
}

.files-card {
  min-width: 0;
}

.files-card__upload {
  margin-bottom: var(--space-lg);
}
</style>
