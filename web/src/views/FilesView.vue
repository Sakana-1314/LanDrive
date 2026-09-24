<script setup lang="ts">
// 全部文件 / 我的文件。
//
// 导航分两层：
//   - 人员：左侧菜单「全部文件」展开后按人切换（?owner=<id>），本页不重复提供；
//   - 目录：本页内的文件夹导航（?folder=<id>），带面包屑，可新建/重命名/删除。
//
// 「我的文件」上方嵌入上传面板：在哪个目录就传到哪个目录（真实磁盘层级）。
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NButton,
  NCard,
  NEmpty,
  NIcon,
  NInput,
  NModal,
  useDialog,
  useMessage
} from 'naive-ui'
import {
  ChevronForwardOutline,
  CreateOutline,
  FolderOpenOutline,
  HomeOutline,
  LinkOutline,
  TrashOutline
} from '@vicons/ionicons5'
import {
  createFolder,
  deleteFolder,
  errMsg,
  listFiles,
  listFolders,
  renameFolder
} from '@/api'
import type { FileItem, Folder } from '@/api/types'
import FileTable from '@/components/FileTable.vue'
import UploadPanel from '@/components/UploadPanel.vue'
import { useCreateShare } from '@/composables/useCreateShare'
import { formatBytes } from '@/utils/format'
import { loadOwners, ownersState } from '@/stores/owners'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const dialog = useDialog()
const share = useCreateShare()

const scope = computed<'all' | 'mine'>(() => (route.meta.scope === 'mine' ? 'mine' : 'all'))

/** 当前查看的人员目录；来自菜单子 tab 的 ?owner=<id>。 */
const ownerId = computed(() => {
  const v = Number(route.query.owner)
  return Number.isFinite(v) && v > 0 ? v : null
})

/** 当前所在文件夹（0 = 根目录）；来自 ?folder=<id>。 */
const folderId = computed(() => {
  const v = Number(route.query.folder)
  return Number.isFinite(v) && v > 0 ? v : 0
})

/**
 * 目录导航只在**明确看某个人的目录**时提供。
 *
 * 「全部人员」是跨人的混合视图，把所有人的同名文件夹混在一起没有意义；
 * 管理员也一样先选人再看目录。
 */
const folderNavEnabled = computed(() => scope.value === 'mine' || ownerId.value !== null)

/** 目录归属谁：我的文件→自己；看某人→那个人。 */
const navOwnerId = computed(() => (scope.value === 'mine' ? 0 : ownerId.value || 0))

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

const folders = ref<Folder[]>([])
const breadcrumb = ref<Folder[]>([])
const foldersLoading = ref(false)

async function loadFolders() {
  if (!folderNavEnabled.value) {
    folders.value = []
    breadcrumb.value = []
    return
  }
  foldersLoading.value = true
  try {
    const res = await listFolders({ owner_id: navOwnerId.value, folder_id: folderId.value })
    folders.value = res.folders
    breadcrumb.value = res.breadcrumb
  } catch (e) {
    message.error(errMsg(e))
  } finally {
    foldersLoading.value = false
  }
}

async function load() {
  loading.value = true
  try {
    const res = await listFiles({
      scope: scope.value,
      owner_id: scope.value === 'mine' ? undefined : ownerId.value || undefined,
      // 目录过滤只在提供目录导航时生效；「全部人员」视图保持原有的跨人混合
      folder_id: folderNavEnabled.value && folderId.value > 0 ? folderId.value : undefined,
      folder_root: folderNavEnabled.value && folderId.value === 0 ? true : undefined,
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

function reload() {
  load()
  loadFolders()
  if (scope.value === 'all') loadOwners(true).catch(() => {})
}

/** 进入某个子目录（用 query 而不是新路由，保持返回键可用）。 */
function enterFolder(f: Folder) {
  router.push({ query: { ...route.query, folder: String(f.id) } })
}

/** 跳到面包屑的某一层（null = 根目录）。 */
function goToBreadcrumb(id: number | null) {
  const q = { ...route.query }
  if (id === null) delete q.folder
  else q.folder = String(id)
  router.push({ query: q })
}

// --- 新建 / 重命名 / 删除文件夹 ---

const folderModal = ref(false)
const folderModalMode = ref<'create' | 'rename'>('create')
const folderNameInput = ref('')
const folderSubmitting = ref(false)
const editingFolder = ref<Folder | null>(null)

function openCreateFolder() {
  folderModalMode.value = 'create'
  editingFolder.value = null
  folderNameInput.value = ''
  folderModal.value = true
}

function openRenameFolder(f: Folder) {
  folderModalMode.value = 'rename'
  editingFolder.value = f
  folderNameInput.value = f.name
  folderModal.value = true
}

async function submitFolder() {
  const name = folderNameInput.value.trim()
  if (!name) {
    message.warning('请输入文件夹名称')
    return
  }
  folderSubmitting.value = true
  try {
    if (folderModalMode.value === 'create') {
      await createFolder({
        name,
        parent_id: folderId.value || 0,
        owner_id: navOwnerId.value || undefined
      })
      message.success(`已创建「${name}」`)
    } else if (editingFolder.value) {
      await renameFolder(editingFolder.value.id, {
        name,
        parent_id: editingFolder.value.parent_id ?? 0
      })
      message.success('已重命名')
    }
    folderModal.value = false
    reload()
  } catch (e) {
    message.error(errMsg(e))
  } finally {
    folderSubmitting.value = false
  }
}

function confirmDeleteFolder(f: Folder) {
  dialog.warning({
    title: '删除文件夹',
    content: `「${f.name}」及其中的文件都会被删除（文件可从回收站恢复）。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        const res = await deleteFolder(f.id)
        message.success(
          res.soft_deleted_files
            ? `已删除，${res.soft_deleted_files} 个文件移入回收站`
            : '已删除'
        )
        reload()
      } catch (e) {
        message.error(errMsg(e))
      }
    }
  })
}

function onShareFolder(f: Folder) {
  void share.create('folder', f.id, f.name)
}

function onSortChange(p: { sort: string; order: 'asc' | 'desc' }) {
  sort.value = p.sort
  order.value = p.order
  load()
}

// 切换人员或目录时重置分页与关键字。
watch(
  () => route.fullPath,
  () => {
    keyword.value = ''
    page.value = 1
    reload()
  }
)

watch([page, pageSize], () => load())

onMounted(() => {
  reload()
})
</script>

<template>
  <div class="files-page">
    <n-card class="card-surface files-card" :bordered="false">
      <!-- 目录工具条：面包屑 + 新建文件夹 -->
      <div v-if="folderNavEnabled" class="folder-bar">
        <div class="crumbs">
          <button class="crumb" type="button" @click="goToBreadcrumb(null)">
            <n-icon :size="14"><home-outline /></n-icon>
            <span>{{ scope === 'mine' ? '我的文件' : ownerLabel }}</span>
          </button>
          <template v-for="b in breadcrumb" :key="b.id">
            <n-icon class="crumb-sep" :size="13"><chevron-forward-outline /></n-icon>
            <button
              class="crumb"
              type="button"
              :class="{ 'is-current': b.id === folderId }"
              @click="goToBreadcrumb(b.id)"
            >
              {{ b.name }}
            </button>
          </template>
        </div>
        <n-button v-if="scope === 'mine'" size="small" secondary @click="openCreateFolder">
          <template #icon><n-icon><create-outline /></n-icon></template>
          新建文件夹
        </n-button>
      </div>

      <!-- 子目录卡片 -->
      <ul v-if="folders.length" class="folder-grid">
        <li v-for="f in folders" :key="f.id" class="folder-item">
          <button class="folder-main" type="button" @click="enterFolder(f)">
            <n-icon :size="20" color="var(--color-primary)"><folder-open-outline /></n-icon>
            <span class="folder-name" :title="f.name">{{ f.name }}</span>
            <span class="folder-meta">
              {{ f.file_count }} 个文件
              <template v-if="f.sub_folder_count"> · {{ f.sub_folder_count }} 个文件夹</template>
              <template v-if="f.used_bytes"> · {{ formatBytes(f.used_bytes) }}</template>
            </span>
          </button>
          <div v-if="scope === 'mine'" class="folder-tools">
            <n-button size="small" quaternary type="primary" @click="onShareFolder(f)">
              <template #icon><n-icon><link-outline /></n-icon></template>
            </n-button>
            <n-button size="small" quaternary @click="openRenameFolder(f)">
              <template #icon><n-icon><create-outline /></n-icon></template>
            </n-button>
            <n-button size="small" quaternary type="error" @click="confirmDeleteFolder(f)">
              <template #icon><n-icon><trash-outline /></n-icon></template>
            </n-button>
          </div>
        </li>
      </ul>

      <!-- 上传面板只出现在「我的文件」：它上传到的就是当前所在目录 -->
      <UploadPanel
        v-if="scope === 'mine'"
        class="files-card__upload"
        :folder-id="folderId"
        @uploaded="reload"
      />

      <n-empty
        v-if="folderNavEnabled && !folders.length && !items.length && !loading && folderId === 0"
        description="这里还没有文件"
        style="padding: 24px 0"
      />

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
        @refresh="reload"
        @sort="onSortChange"
      />
    </n-card>

    <!-- 新建 / 重命名文件夹 -->
    <n-modal
      v-model:show="folderModal"
      preset="card"
      :title="folderModalMode === 'create' ? '新建文件夹' : '重命名文件夹'"
      style="max-width: 400px"
      :bordered="false"
    >
      <n-input
        v-model:value="folderNameInput"
        placeholder="文件夹名称"
        :maxlength="120"
        autofocus
        @keyup.enter="submitFolder"
      />
      <template #footer>
        <div class="modal-actions">
          <n-button @click="folderModal = false">取消</n-button>
          <n-button type="primary" :loading="folderSubmitting" @click="submitFolder">
            {{ folderModalMode === 'create' ? '创建' : '保存' }}
          </n-button>
        </div>
      </template>
    </n-modal>
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

.folder-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-sm);
  margin-bottom: var(--space-md);
  min-width: 0;
}

.crumbs {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 2px;
  min-width: 0;
}

.crumb {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  max-width: 220px;
  padding: 3px 8px;
  overflow: hidden;
  border: 0;
  border-radius: var(--radius-control);
  color: var(--color-text-muted);
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
  background: transparent;
  cursor: pointer;
}

.crumb:hover {
  color: var(--color-primary);
  background: var(--color-surface-soft);
}

.crumb.is-current {
  color: var(--color-text-strong);
  font-weight: 600;
}

.crumb-sep {
  flex: none;
  color: var(--color-border);
}

.folder-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 10px;
  margin: 0 0 var(--space-lg);
  padding: 0;
  list-style: none;
}

.folder-item {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  padding: 9px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--color-surface);
}

.folder-main {
  display: grid;
  flex: 1;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 2px 9px;
  align-items: center;
  min-width: 0;
  padding: 0;
  border: 0;
  text-align: left;
  background: transparent;
  cursor: pointer;
}

.folder-name {
  overflow: hidden;
  color: var(--color-text-strong);
  font-size: 14px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.folder-meta {
  grid-column: 2;
  color: var(--color-text-muted);
  font-size: 12px;
}

.folder-tools {
  display: flex;
  flex: none;
  gap: 0;
}

.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

@media (max-width: 768px) {
  .folder-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
