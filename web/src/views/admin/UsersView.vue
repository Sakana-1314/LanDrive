<script setup lang="ts">
// 用户管理：新建、编辑、停用、重置密码、删除账号。
import { h, onMounted, reactive, ref } from 'vue'
import {
  NButton,
  NCard,
  NDataTable,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NModal,
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
import { AddOutline, KeyOutline, RefreshOutline } from '@vicons/ionicons5'
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

const message = useMessage()

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
const deletePurge = ref(false)

function openDelete(row: User) {
  deleteTarget.value = row
  deletePurge.value = false
  showDelete.value = true
}

async function onDelete() {
  if (!deleteTarget.value) return
  try {
    await adminDeleteUser(deleteTarget.value.id, deletePurge.value)
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
    <n-card :bordered="false" size="small" style="border-radius: 8px">
      <template #header>
        <n-space justify="space-between" align="center" style="width: 100%">
          <n-space align="center" :size="8">
            <n-text strong style="font-size: 16px">用户管理</n-text>
            <n-tag size="small" :bordered="false">共 {{ total }} 个账号</n-tag>
          </n-space>
          <n-space :size="8">
            <n-input
              v-model:value="keyword"
              placeholder="搜索工号或姓名"
              clearable
              style="width: 220px"
              @keyup.enter="((page = 1), load())"
            />
            <n-button @click="((page = 1), load())">
              <template #icon>
                <n-icon><refresh-outline /></n-icon>
              </template>
              查询
            </n-button>
            <n-button type="primary" @click="showCreate = true">
              <template #icon>
                <n-icon><add-outline /></n-icon>
              </template>
              新建账号
            </n-button>
          </n-space>
        </n-space>
      </template>

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
    </n-card>

    <!-- 新建账号 -->
    <n-modal v-model:show="showCreate" preset="card" title="新建账号" style="width: 460px">
      <n-form ref="createRef" label-placement="left" label-width="80">
        <n-form-item label="工号">
          <n-input v-model:value="createForm.employee_no" placeholder="登录用，创建后不可修改" />
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
        <n-text depth="3" style="font-size: 12px; display: block; margin-bottom: 10px">
          创建后会自动为该用户建立独立目录 users/&lt;ID&gt;。
        </n-text>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showCreate = false">取消</n-button>
          <n-button type="primary" @click="onCreate">创建</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 编辑账号 -->
    <n-modal v-model:show="showEdit" preset="card" title="编辑账号" style="width: 440px">
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
          <n-text depth="3" style="font-size: 12px; margin-left: 8px">停用后该账号立即无法登录</n-text>
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
    <n-modal v-model:show="showReset" preset="card" title="重置密码" style="width: 420px">
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
        <n-text depth="3" style="font-size: 12px">重置后该用户已登录的会话会立即失效，需要使用新密码重新登录。</n-text>
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
    <n-modal v-model:show="showDelete" preset="card" title="删除账号" style="width: 480px">
      <n-space vertical :size="12">
        <n-text>
          即将删除账号 <b>{{ deleteTarget?.name }}（{{ deleteTarget?.employee_no }}）</b>。
        </n-text>
        <n-text v-if="deleteTarget && deleteTarget.file_count > 0" type="warning">
          该账号名下还有 {{ deleteTarget.file_count }} 个文件（{{ formatBytes(deleteTarget.used_bytes) }}）。
        </n-text>
        <n-space v-if="deleteTarget && deleteTarget.file_count > 0" vertical :size="6">
          <n-switch v-model:value="deletePurge" />
          <n-text depth="3" style="font-size: 12px">
            开启「同时彻底删除文件」会永久删除该账号全部文件（磁盘文件与记录一并删除，不可恢复）。
            不开启则删除会失败，请先转移或删除这些文件。
          </n-text>
        </n-space>
        <n-text depth="3" style="font-size: 12px">删除后该账号的目录也会被移除。</n-text>
      </n-space>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showDelete = false">取消</n-button>
          <n-popconfirm @positive-click="onDelete">
            <template #trigger>
              <n-button type="error">确认删除</n-button>
            </template>
            {{ deletePurge ? '将永久删除该账号及其全部文件，确定继续？' : '确定删除该账号？' }}
          </n-popconfirm>
        </n-space>
      </template>
    </n-modal>
  </n-space>
</template>
