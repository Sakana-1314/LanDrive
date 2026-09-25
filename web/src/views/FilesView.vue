<script setup lang="ts">
// 全部文件 / 我的文件。
//
// 导航分两层：
//   - 人员：左侧菜单「全部文件」展开后按人切换（?owner=<id>），本页不重复提供；
//   - 目录：本页内的文件夹导航（?folder=<id>），带面包屑，可新建/重命名/删除。
//
// 上传入口有两个，都不占独立拖拽区的版面：
//   - 工具条的「上传文件」按钮（移动端没有拖拽操作，必须留按钮）；
//   - 整页拖放（PageDropZone）—— 拖到页面任意位置松手即传到当前目录。
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NAlert,
  NButton,
  NCard,
  NIcon,
  NInput,
  NModal,
  useDialog,
  useMessage
} from 'naive-ui'
import {
  ChevronForwardOutline,
  CloudUploadOutline,
  CreateOutline,
  FolderOpenOutline,
  HomeOutline
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
import PageDropZone from '@/components/PageDropZone.vue'
import { useCreateShare } from '@/composables/useCreateShare'
import { useUploadQueue } from '@/composables/useUploadQueue'
import { entriesFromFileList, type UploadEntry } from '@/utils/uploadEntries'
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

// --- 上传 ---
//
// 上传只在「我的文件」开放：目标目录是「我的文件」当前所在的那一层，
// 拖到任意位置、或点「上传文件」选文件，都落到同一处。
// 队列状态在 stores/upload.ts 的全局单例里，右下角的浮窗展示它 ——
// 所以传完之后切到别的页面，进度依然可见、可取消。
const {
  uploadDisabled,
  prepare: prepareUpload,
  addFiles: addUploadFiles,
  addEntries: addUploadEntries
} = useUploadQueue({
  getFolderId: () => folderId.value,
  onUploaded: () => reload()
})

/**
 * 整页拖放的落点逻辑。
 * 目标目录由 addEntries 在**入队那一刻**同步（松手即入队），
 * 因此用户随后切换目录也不会把这次拖入的文件传错地方。
 * 参数是展平后的条目（含相对目录），不是裸 File 列表 —— 拖入的文件夹要保留层级。
 */
function onDropFiles(entries: UploadEntry[]) {
  void addUploadEntries(entries)
}

// 「全部文件」等非本人目录不开放上传（后端只允许传到自己目录）
const dropEnabled = computed(() => scope.value === 'mine')

/** 隐藏的原生文件输入：点工具条按钮时由它弹出系统选择框。 */
const filePicker = ref<HTMLInputElement | null>(null)
/** 选择文件夹的输入（带 webkitdirectory，移动端选目录的入口）。 */
const dirPicker = ref<HTMLInputElement | null>(null)

/** 选完文件即入队；清空 value，否则连续选同一个文件不会触发 change。 */
function onPickFiles(e: Event) {
  const el = e.target as HTMLInputElement
  const files = Array.from(el.files || [])
  el.value = ''
  if (files.length) addUploadFiles(files)
}

/**
 * 选择文件夹：每个 File 带 webkitRelativePath（如 `报表/2026/1.xlsx`），
 * 据此还原目录层级后入队（与拖入文件夹走同一条路径）。
 */
function onPickDir(e: Event) {
  const el = e.target as HTMLInputElement
  const entries = entriesFromFileList(el.files || [])
  el.value = ''
  if (entries.length) void addUploadEntries(entries)
}

// 切换人员或目录时重置分页与关键字。
watch(
  () => route.fullPath,
  () => {
    keyword.value = ''
    page.value = 1
    reload()
    // /files 与 /files/mine 复用同一个组件实例：从「全部文件」切到
    // 「我的文件」不会重新挂载，因此这里也要准备上传（prepare 幂等）。
    if (scope.value === 'mine') void prepareUpload()
  }
)

watch([page, pageSize], () => load())

onMounted(() => {
  reload()
  if (scope.value === 'mine') void prepareUpload()
})
</script>

<template>
  <div class="files-page">
    <n-card class="card-surface files-card" :bordered="false">
      <!-- 目录工具条：左=面包屑（筛选/定位），右=按钮（上传、新建文件夹） -->
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
        <!-- 上传入口：拖拽之外留按钮（移动端没有拖拽操作，且能整个文件夹一起传） -->
        <div v-if="scope === 'mine'" class="folder-bar__tools">
          <n-button
            type="primary"
            :disabled="uploadDisabled"
            @click="filePicker?.click()"
          >
            <template #icon><n-icon><cloud-upload-outline /></n-icon></template>
            上传文件
          </n-button>
          <n-button
            secondary
            :disabled="uploadDisabled"
            @click="dirPicker?.click()"
          >
            <template #icon><n-icon><folder-open-outline /></n-icon></template>
            上传文件夹
          </n-button>
          <n-button secondary @click="openCreateFolder">
            <template #icon><n-icon><create-outline /></n-icon></template>
            新建文件夹
          </n-button>
        </div>
      </div>

      <!-- 上传已暂停：必须说明原因，否则用户不知道为什么拖不进去 -->
      <n-alert
        v-if="scope === 'mine' && uploadDisabled"
        class="files-card__notice"
        type="warning"
        title="上传已暂停"
      >
        管理员已暂停上传功能。
      </n-alert>

      <!--
        文件夹与文件在同一个列表里（FileTable 内部合成行，文件夹在前）：
        此前子文件夹是这里单独的一坨卡片，同一个目录的两类东西被拆在两处，
        用户得上下扫两遍才知道这一层有什么。现在对齐 Windows 资源管理器的形态。
      -->

      <!--
        空状态由 FileTable 统一渲染（桌面表格的 #empty 与移动端卡片各一处）。
        这里**不要**再加页面级 n-empty：PR #13 加过一条「这里还没有文件」，
        与表格内的「暂无文件」同时渲染，页面上就出现了两遍空提示。
      -->

      <FileTable
        v-model:keyword="keyword"
        :title="scope === 'all' ? ownerLabel : ''"
        :items="items"
        :folders="folders"
        :folder-editable="scope === 'mine'"
        :loading="loading"
        :total="total"
        :show-owner="scope === 'all'"
        :page="page"
        :page-size="pageSize"
        @update:page="(v: number) => (page = v)"
        @update:page-size="(v: number) => (pageSize = v)"
        @refresh="reload"
        @sort="onSortChange"
        @open-folder="enterFolder"
        @share-folder="onShareFolder"
        @rename-folder="openRenameFolder"
        @delete-folder="confirmDeleteFolder"
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

    <!--
      点击选择的原生文件输入：隐藏但由「上传文件」按钮触发。
      为什么不用 n-upload：它自带虚线拖拽框，而需求是整个页面都能拖入，
      再摆一个拖拽区只会让人以为只有框里能拖。
    -->
    <input
      v-if="scope === 'mine'"
      ref="filePicker"
      class="file-picker"
      type="file"
      multiple
      tabindex="-1"
      aria-hidden="true"
      @change="onPickFiles"
    />

    <!--
      选择整个文件夹：webkitdirectory 让系统选择框只列目录，选中的每个文件
      都带 webkitRelativePath（如 报表/2026/1.xlsx），据此还原层级后再上传。
      没有拖拽能力的移动端也能用它传整个目录。
    -->
    <input
      v-if="scope === 'mine'"
      ref="dirPicker"
      class="file-picker"
      type="file"
      webkitdirectory
      directory
      multiple
      tabindex="-1"
      aria-hidden="true"
      @change="onPickDir"
    />

    <!-- 整页拖放：拖到「我的文件」任意位置松手即上传 -->
    <PageDropZone
      v-if="dropEnabled"
      :disabled="uploadDisabled"
      @files="onDropFiles"
    />
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

.folder-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-sm);
  margin-bottom: var(--space-md);
  min-width: 0;
}

/* 工具条右侧：上传与新建文件夹并排，窄屏一起换行 */
.folder-bar__tools {
  display: flex;
  /* 不能 flex:none —— 三个按钮共 368px，窄屏会顶出卡片被裁掉（390px 下「新建文件夹」
     只剩一半）。允许收缩并换行，窄屏时按钮折到下一行。 */
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  min-width: 0;
}

/* 提示条与文件列表之间的间距，避免文字贴住目录卡片 */
.files-card__notice {
  margin-bottom: var(--space-md);
}

/* 隐藏的原生文件输入：只作为「上传文件」按钮的触发器，不占版面 */
.file-picker {
  display: none;
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

.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}


</style>
