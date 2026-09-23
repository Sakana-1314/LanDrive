<script setup lang="ts">
// 主框架：顶部栏（品牌 + 当前用户） + 左侧菜单（按角色渲染） + 内容区。
import { computed, h, onMounted, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import {
  NAvatar,
  NButton,
  NDropdown,
  NIcon,
  NLayout,
  NLayoutContent,
  NLayoutHeader,
  NLayoutSider,
  NMenu,
  NSpace,
  NText,
  useMessage,
  type MenuOption
} from 'naive-ui'
import {
  CloudUploadOutline,
  DocumentTextOutline,
  FolderOpenOutline,
  PersonCircleOutline,
  ServerOutline,
  SettingsOutline,
  StatsChartOutline,
  TimeOutline
} from '@vicons/ionicons5'
import { clearToken } from '@/api'
import { clearUser, isAdmin, state } from '@/stores/user'
import { formatBytes } from '@/utils/format'

const router = useRouter()
const route = useRoute()
const message = useMessage()
const collapsed = ref(false)

function renderIcon(icon: typeof FolderOpenOutline) {
  return () => h(NIcon, null, { default: () => h(icon) })
}

const menuOptions = computed<MenuOption[]>(() => {
  const opts: MenuOption[] = [
    {
      label: () => h(RouterLink, { to: '/files' }, { default: () => '全部文件' }),
      key: '/files',
      icon: renderIcon(FolderOpenOutline)
    },
    {
      label: () => h(RouterLink, { to: '/files/mine' }, { default: () => '我的文件' }),
      key: '/files/mine',
      icon: renderIcon(DocumentTextOutline)
    },
    {
      label: () => h(RouterLink, { to: '/upload' }, { default: () => '上传文件' }),
      key: '/upload',
      icon: renderIcon(CloudUploadOutline)
    }
  ]
  if (isAdmin()) {
    opts.push(
      {
        type: 'divider',
        key: 'd1'
      },
      {
        label: () => h(RouterLink, { to: '/admin/dashboard' }, { default: () => '统计看板' }),
        key: '/admin/dashboard',
        icon: renderIcon(StatsChartOutline)
      },
      {
        label: () => h(RouterLink, { to: '/admin/users' }, { default: () => '用户管理' }),
        key: '/admin/users',
        icon: renderIcon(PersonCircleOutline)
      },
      {
        label: () => h(RouterLink, { to: '/admin/files' }, { default: () => '文件与回收站' }),
        key: '/admin/files',
        icon: renderIcon(ServerOutline)
      },
      {
        label: () => h(RouterLink, { to: '/admin/logs' }, { default: () => '审计日志' }),
        key: '/admin/logs',
        icon: renderIcon(TimeOutline)
      },
      {
        label: () => h(RouterLink, { to: '/admin/settings' }, { default: () => '系统配置' }),
        key: '/admin/settings',
        icon: renderIcon(SettingsOutline)
      }
    )
  }
  return opts
})

const activeKey = computed(() => {
  if (route.path.startsWith('/admin/')) return route.path
  if (route.path === '/files/mine') return '/files/mine'
  return route.path
})

const userOptions = [
  { label: '个人设置', key: 'profile' },
  { type: 'divider', key: 'd' },
  { label: '退出登录', key: 'logout' }
]

function onUserSelect(key: string) {
  if (key === 'profile') {
    router.push('/profile')
    return
  }
  if (key === 'logout') {
    clearToken()
    clearUser()
    message.success('已退出登录')
    router.push('/login')
  }
}

const myUsage = computed(() => {
  const u = state.user
  if (!u) return ''
  return `${u.file_count} 个文件 · ${formatBytes(u.used_bytes)}`
})

onMounted(() => {
  // 窄屏自动折叠侧边栏。
  if (window.innerWidth < 900) collapsed.value = true
})
</script>

<template>
  <n-layout position="absolute">
    <n-layout-header bordered class="header">
      <n-space align="center" :size="12">
        <n-button quaternary circle @click="collapsed = !collapsed">
          <template #icon>
            <n-icon><folder-open-outline /></n-icon>
          </template>
        </n-button>
        <n-text strong style="font-size: 17px">局域网文件助手</n-text>
        <n-text depth="3" style="font-size: 12px">公司内部文件共享</n-text>
      </n-space>

      <n-space align="center" :size="10">
        <n-text depth="3" style="font-size: 12px">{{ myUsage }}</n-text>
        <n-dropdown :options="userOptions" trigger="click" @select="onUserSelect">
          <n-space align="center" :size="6" style="cursor: pointer">
            <n-avatar round :size="30" color="#1f6feb">
              {{ state.user?.name?.slice(0, 1) || '?' }}
            </n-avatar>
            <div style="line-height: 1.2">
              <div style="font-size: 13px">{{ state.user?.name }}</div>
              <div style="font-size: 11px; opacity: 0.65">
                {{ state.user?.employee_no }}
                <span v-if="isAdmin()">· 管理员</span>
              </div>
            </div>
          </n-space>
        </n-dropdown>
      </n-space>
    </n-layout-header>

    <n-layout has-sider position="absolute" style="top: 56px">
      <n-layout-sider
        bordered
        collapse-mode="width"
        :collapsed-width="64"
        :width="200"
        :collapsed="collapsed"
        show-trigger
        @collapse="collapsed = true"
        @expand="collapsed = false"
      >
        <n-menu
          :value="activeKey"
          :collapsed="collapsed"
          :collapsed-width="64"
          :collapsed-icon-size="20"
          :options="menuOptions"
        />
      </n-layout-sider>

      <n-layout-content content-style="padding: 16px;" :native-scrollbar="false">
        <router-view />
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<style scoped>
.header {
  height: 56px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
}
</style>
