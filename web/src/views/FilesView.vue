<script setup lang="ts">
// 全部文件 / 我的文件：左侧用户目录树 + 右侧文件表格。
//
// 「全部文件」按用户目录分组展示，所有人都能预览与下载；
// 表格操作列只在 can_edit（自己上传的文件，或管理员）时出现修改/删除按钮。
import { computed, h, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  NButton,
  NCard,
  NIcon,
  NLayout,
  NLayoutContent,
  NLayoutSider,
  NMenu,
  NSpace,
  NTag,
  NText,
  useMessage,
  type MenuOption
} from 'naive-ui'
import { FolderOutline, PeopleOutline, RefreshOutline } from '@vicons/ionicons5'
import { errMsg, listFiles, listOwners } from '@/api'
import type { FileItem, OwnerAggregate } from '@/api/types'
import FileTable from '@/components/FileTable.vue'
import { formatBytes } from '@/utils/format'

const route = useRoute()
const message = useMessage()

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

/** 当前选中的目录：null = 全部用户。 */
const selectedOwner = ref<number | null>(null)

const totalFiles = computed(() => owners.value.reduce((s, o) => s + o.file_count, 0))

const ownerMenuOptions = computed<MenuOption[]>(() => {
  const opts: MenuOption[] = [
    {
      label: () => `全部用户（${totalFiles.value} 个文件）`,
      key: 'all',
      icon: () => h(NIcon, null, { default: () => h(PeopleOutline) })
    },
    { type: 'divider', key: 'd' }
  ]
  for (const o of owners.value) {
    opts.push({
      label: () => `${o.name}（${o.employee_no}）· ${o.file_count} 个 · ${formatBytes(o.used_bytes)}`,
      key: String(o.user_id),
      icon: () => h(NIcon, null, { default: () => h(FolderOutline) })
    })
  }
  return opts
})

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

function onOwnerSelect(key: string) {
  selectedOwner.value = key === 'all' ? null : Number(key)
  page.value = 1
  load()
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
  <n-layout has-sider style="background: transparent">
    <n-layout-sider
      v-if="scope === 'all'"
      bordered
      :width="310"
      :native-scrollbar="false"
      style="border-radius: 8px; margin-right: 12px; max-height: calc(100vh - 90px)"
    >
      <div style="padding: 12px">
        <n-space align="center" :size="6" style="margin-bottom: 6px">
          <n-icon color="#1f6feb"><folder-outline /></n-icon>
          <n-text strong>用户目录</n-text>
        </n-space>
        <n-text depth="3" style="font-size: 12px; display: block; margin-bottom: 6px">
          每个用户都有独立目录，你可以浏览并下载所有人的文件。
        </n-text>
        <n-menu
          :value="selectedOwner === null ? 'all' : String(selectedOwner)"
          :options="ownerMenuOptions"
          :indent="14"
          @update:value="onOwnerSelect"
        />
      </div>
    </n-layout-sider>

    <n-layout-content :native-scrollbar="false">
      <n-card :bordered="false" size="small" style="border-radius: 8px">
        <template #header>
          <n-space align="center" :size="8">
            <n-text strong style="font-size: 16px">
              {{ scope === 'mine' ? '我的文件' : '全部文件' }}
            </n-text>
            <n-tag v-if="scope === 'mine'" size="small" type="info" :bordered="false">
              只有你可以修改或删除自己的文件
            </n-tag>
            <n-tag v-else size="small" :bordered="false">可预览与下载所有人的文件</n-tag>
          </n-space>
        </template>

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
        >
          <template #toolbar>
            <n-button :loading="loading" @click="refresh">
              <template #icon>
                <n-icon><refresh-outline /></n-icon>
              </template>
              刷新
            </n-button>
          </template>
        </FileTable>
      </n-card>
    </n-layout-content>
  </n-layout>
</template>
