<script setup lang="ts">
// 用户管理：新建、编辑、停用、重置密码、删除账号。
import { h, onMounted, reactive, ref } from 'vue'
import {
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NModal,
  NPagination,
  NPopconfirm,
  NRadio,
  NRadioGroup,
  NSpace,
  NSwitch,
  NTag,
  NText,
  useMessage,
  type DataTableColumns,
  type FormInst
} from 'naive-ui'
import { AddOutline, KeyOutline } from '@vicons/ionicons5'
import {
  adminCreateUser,
  adminDeleteUser,
  adminListUsers,
  adminResetPassword,
  adminUpdateUser,
  errMsg
} from '@/api'
import type { User } from '@/api/types'
import { formatBytes, formatTime } from '@/utils/format'
import { useIsMobile } from '@/utils/themeState'

const message = useMessage()
const isMobile = useIsMobile()

const items = ref<User[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const keyword = ref('')
const loading = ref(false)

const showCreate = ref(false)
const createForm = reactive({ employee_no: '', name: '', role: 'user', password: '' })
const createRef = ref<FormInst | null>(null)

const showReset = ref(false)
const resetTarget = ref<User | null>(null)
const resetPassword = ref('')

const showEdit = ref(false)
const editTarget = ref<User | null>(null)
const editForm = reactive({ name: '', role: 'user', enabled: true })

async function load() {
  loading.value = true
  try {
    const res = await adminListUsers({
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

async function onCreate() {
  if (!createForm.employee_no.trim() || !createForm.name.trim()) {
    message.warning('请填写工号与姓名')
    return
  }
  if (createForm.password.length < 6) {
    message.warning('初始密码长度至少 6 位')
    return
  }
  try {
    await adminCreateUser({
      employee_no: createForm.employee_no.trim(),
      name: createForm.name.trim(),
      role: createForm.role,
      password: createForm.password
    })
    message.success('账号已创建，并已自动创建其独立目录')
    showCreate.value = false
    Object.assign(createForm, { employee_no: '', name: '', role: 'user', password: '' })
    page.value = 1
    load()
  } catch (e) {
    message.error(errMsg(e))
  }
}

function openEdit(row: User) {
  editTarget.value = row
  editForm.name = row.name
  editForm.role = row.role
  editForm.enabled = row.enabled
  showEdit.value = true
}

async function onEdit() {
  if (!editTarget.value) return
  try {
    await adminUpdateUser(editTarget.value.id, {
      name: editForm.name.trim(),
      role: editForm.role,
      enabled: editForm.enabled
    })
    message.success('已保存')
    showEdit.value = false
    load()
  } catch (e) {
    message.error(errMsg(e))
  }
}

function openReset(row: User) {
  resetTarget.value = row
  resetPassword.value = ''
  showReset.value = true
}

async function onReset() {
  if (!resetTarget.value) return
  if (resetPassword.value.length < 6) {
    message.warning('新密码长度至少 6 位')
    return
  }
  try {
    await adminResetPassword(resetTarget.value.id, resetPassword.value)
    message.success('密码已重置，该账号的登录状态已失效')
    showReset.value = false
  } catch (e) {
    message.error(errMsg(e))
  }
}

const showDelete = ref(false)
const deleteTarget = ref<User | null>(null)

function openDelete(row: User) {
  deleteTarget.value = row
  showDelete.value = true
}

async function onDelete() {
  if (!deleteTarget.value) return
  try {
    await adminDeleteUser(deleteTarget.value.id)
    message.success('账号已删除')
    showDelete.value = false
    load()
  } catch (e) {
    message.error(errMsg(e))
  }
}

const columns: DataTableColumns<User> = [
  { title: '工号', key: 'employee_no', width: 120 },
  { title: '姓名', key: 'name', width: 130 },
  {
    title: '角色',
    key: 'role',
    width: 100,
    render: (r) =>
      h(NTag, { size: 'small', type: r.role === 'admin' ? 'warning' : 'default', bordered: false }, {
        default: () => (r.role === 'admin' ? '管理员' : '用户')
      })
  },
  {
    title: '状态',
    key: 'enabled',
    width: 90,
    render: (r) =>
      h(NTag, { size: 'small', type: r.enabled ? 'success' : 'error', bordered: false }, {
        default: () => (r.enabled ? '正常' : '停用')
      })
  },
  { title: '文件数', key: 'file_count', width: 90 },
  { title: '占用', key: 'used_bytes', width: 100, render: (r) => formatBytes(r.used_bytes) },
  { title: '目录', key: 'dir_rel', width: 110 },
  { title: '最后登录', key: 'last_login_at', width: 150, render: (r) => formatTime(r.last_login_at) },
  {
    title: '操作',
    key: 'actions',
    width: 230,
    fixed: 'right',
    render: (row) =>
      h(NSpace, { size: 2 }, {
        default: () => [
          h(NButton, { size: 'small', quaternary: true, onClick: () => openEdit(row) }, { default: () => '编辑' }),
          h(
            NButton,
            { size: 'small', quaternary: true, onClick: () => openReset(row) },
            { default: () => '重置密码' }
          ),
          h(
            NButton,
            { size: 'small', quaternary: true, type: 'error', onClick: () => openDelete(row) },
            { default: () => '删除' }
          )
        ]
      })
  }
]

onMounted(load)
</script>

<template>
  <n-space vertical :size="14">
    <n-card class="card-surface" :bordered="false">
      <div class="section-head" :class="{ 'section-head--stack': isMobile }">
        <!-- 左：计数 + 搜索；右：主操作按钮 -->
        <div class="section-head__main">
          <n-tag :bordered="false">{{ total }} 个账号</n-tag>
          <n-input
            v-model:value="keyword"
            placeholder="搜索工号或姓名"
            clearable
            @keyup.enter="((page = 1), load())"
          />
        </div>
        <div class="section-head__actions">
          <n-button type="primary" @click="showCreate = true">
            <template #icon>
              <n-icon><add-outline /></n-icon>
            </template>
            新建
          </n-button>
        </div>
      </div>

      <div class="desktop-only">
      <n-data-table
        :columns="columns"
        :data="items"
        :loading="loading"
        :row-key="(r: User) => r.id"
        remote
        size="small"
        :scroll-x="1200"
        :pagination="{
          page,
          pageSize,
          itemCount: total,
          showSizePicker: true,
          pageSizes: [10, 20, 50, 100]
        }"
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
      />
      </div>

      <!-- 移动端：账号卡片 -->
      <div class="mobile-only">
        <n-empty v-if="!items.length" description="暂无账号" style="padding: 28px 0" />
        <ul v-else class="user-cards">
          <li v-for="u in items" :key="u.id" class="user-card">
            <div class="user-card__top">
              <span class="user-card__name">{{ u.name }}</span>
              <span class="user-card__no">{{ u.employee_no }}</span>
              <n-tag size="small" :type="u.role === 'admin' ? 'warning' : 'default'" :bordered="false">
                {{ u.role === 'admin' ? '管理员' : '用户' }}
              </n-tag>
              <n-tag v-if="!u.enabled" size="small" type="error" :bordered="false">停用</n-tag>
            </div>
            <div class="user-card__meta">
              <span>{{ u.file_count }} 个文件</span>
              <span>{{ formatBytes(u.used_bytes) }}</span>
              <span>{{ formatTime(u.last_login_at) }}</span>
            </div>
            <div class="user-card__actions">
              <n-button size="small" quaternary @click="openEdit(u)">编辑</n-button>
              <n-button size="small" quaternary @click="openReset(u)">重置密码</n-button>
              <n-button size="small" quaternary type="error" @click="openDelete(u)">删除</n-button>
            </div>
          </li>
        </ul>
        <n-pagination
          v-if="total > pageSize"
          class="mobile-pager"
          :page="page"
          :page-size="pageSize"
          :item-count="total"
          :page-slot="5"
          @update:page="
            (v: number) => {
              page = v
              load()
            }
          "
        />
      </div>
    </n-card>

    <!-- 新建账号 -->
    <n-modal v-model:show="showCreate" preset="card" title="新建账号" :style="{ width: isMobile ? '92vw' : '460px' }">
      <n-form ref="createRef" label-placement="left" label-width="80">
        <n-form-item label="工号">
          <n-input v-model:value="createForm.employee_no" placeholder="登录用；也是文件目录名，创建后不可修改" />
        </n-form-item>
        <n-form-item label="姓名">
          <n-input v-model:value="createForm.name" placeholder="用于展示，允许重名" />
        </n-form-item>
        <n-form-item label="角色">
          <n-radio-group v-model:value="createForm.role">
            <n-space>
              <n-radio value="user">普通用户</n-radio>
              <n-radio value="admin">管理员</n-radio>
            </n-space>
          </n-radio-group>
        </n-form-item>
        <n-form-item label="初始密码">
          <n-input
            v-model:value="createForm.password"
            type="password"
            show-password-on="click"
            placeholder="至少 6 位，建议首次登录后修改"
          />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showCreate = false">取消</n-button>
          <n-button type="primary" @click="onCreate">创建</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 编辑账号 -->
    <n-modal v-model:show="showEdit" preset="card" title="编辑账号" :style="{ width: isMobile ? '92vw' : '440px' }">
      <n-form label-placement="left" label-width="80">
        <n-form-item label="工号">
          <n-input :value="editTarget?.employee_no" disabled />
        </n-form-item>
        <n-form-item label="姓名">
          <n-input v-model:value="editForm.name" />
        </n-form-item>
        <n-form-item label="角色">
          <n-radio-group v-model:value="editForm.role">
            <n-space>
              <n-radio value="user">普通用户</n-radio>
              <n-radio value="admin">管理员</n-radio>
            </n-space>
          </n-radio-group>
        </n-form-item>
        <n-form-item label="启用">
          <n-switch v-model:value="editForm.enabled" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showEdit = false">取消</n-button>
          <n-button type="primary" @click="onEdit">保存</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 重置密码 -->
    <n-modal v-model:show="showReset" preset="card" title="重置密码" :style="{ width: isMobile ? '92vw' : '420px' }">
      <n-space vertical :size="10">
        <n-text>
          为 <b>{{ resetTarget?.name }}（{{ resetTarget?.employee_no }}）</b> 设置新密码
        </n-text>
        <n-input
          v-model:value="resetPassword"
          type="password"
          show-password-on="click"
          placeholder="新密码，至少 6 位"
        />
        <n-text depth="3">重置后该用户的登录状态立即失效。</n-text>
      </n-space>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showReset = false">取消</n-button>
          <n-button type="primary" @click="onReset">
            <template #icon>
              <n-icon><key-outline /></n-icon>
            </template>
            确认重置
          </n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 删除账号 -->
    <n-modal v-model:show="showDelete" preset="card" title="删除账号" :style="{ width: isMobile ? '92vw' : '480px' }">
      <n-space vertical :size="12">
        <n-text>
          即将删除账号 <b>{{ deleteTarget?.name }}（{{ deleteTarget?.employee_no }}）</b>。
        </n-text>
        <n-text v-if="deleteTarget && deleteTarget.file_count > 0" type="warning">
          该账号名下的 {{ deleteTarget.file_count }} 个文件（{{ formatBytes(deleteTarget.used_bytes) }}）
          会与其目录一并清除，不可恢复。
        </n-text>
      </n-space>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showDelete = false">取消</n-button>
          <n-popconfirm @positive-click="onDelete">
            <template #trigger>
              <n-button type="error">确认删除</n-button>
            </template>
            将删除该账号及其全部文件，确定继续？
          </n-popconfirm>
        </n-space>
      </template>
    </n-modal>
  </n-space>
</template>

<style scoped>
.section-head__main :deep(.n-input) {
  width: 240px;
}

.user-cards {
  display: grid;
  /* minmax(0,…) 防止 nowrap 内容把网格列撑宽（grid 子项默认 min-width:auto） */
  grid-template-columns: minmax(0, 1fr);
  min-width: 0;
  gap: 10px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.user-card {
  padding: 12px 13px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--color-surface-soft);
}

.user-card__top {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.user-card__name {
  color: var(--color-text-strong);
  font-size: 15px;
  font-weight: 650;
}

.user-card__no {
  color: var(--color-text-muted);
  font-size: 13px;
}

.user-card__meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  margin-top: 6px;
  color: var(--color-text-muted);
  font-size: 12px;
}

.user-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 10px;
}

.mobile-pager {
  display: flex;
  justify-content: center;
  margin-top: 16px;
}

.mobile-only {
  display: none;
}

@media (max-width: 768px) {
  .desktop-only {
    display: none;
  }

  .mobile-only {
    display: block;
  }

  .section-head--stack {
    align-items: stretch;
    flex-direction: column;
  }

  .section-head--stack .section-head__main,
  .section-head--stack .section-head__actions {
    width: 100%;
    margin-left: 0;
  }

  .section-head--stack .section-head__main :deep(.n-input) {
    flex: 1;
    width: auto;
  }
}
</style>
