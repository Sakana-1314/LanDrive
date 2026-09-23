<script setup lang="ts">
// 主框架：桌面端固定侧栏 + 顶栏；移动端改为抽屉导航，顶栏只留汉堡键与页面标题。
//
// 响应式策略（照搬同组织 Electrical-Manager 的做法）：
//   桌面（>768px）：侧栏常驻，可折叠到 64px 图标栏；
//   移动（≤768px）：侧栏收进抽屉，顶栏高度与内边距同步收窄。
// 这样窄屏不会出现「侧栏吃掉一半宽度、内容挤成一条」的情况。
import {
  computed,
  defineComponent,
  h,
  onMounted,
  ref,
  watch,
  type Component as VueComponent,
  type PropType
} from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import {
  NAvatar,
  NDrawer,
  NDrawerContent,
  NDropdown,
  NIcon,
  NLayout,
  NLayoutContent,
  NLayoutHeader,
  NLayoutSider,
  NMenu,
  useDialog,
  type MenuOption
} from 'naive-ui'
import {
  CheckmarkOutline,
  DocumentTextOutline,
  FolderOpenOutline,
  HardwareChipOutline,
  LogOutOutline,
  MenuOutline,
  MoonOutline,
  PersonCircleOutline,
  PersonOutline,
  Pin,
  PinOutline,
  ServerOutline,
  SpeedometerOutline,
  SunnyOutline
} from '@vicons/ionicons5'
import { clearToken } from '@/api'
import type { OwnerAggregate } from '@/api/types'
import { clearUser, isAdmin, state } from '@/stores/user'
import { loadOwners, ownersState, togglePin } from '@/stores/owners'
import { formatBytes } from '@/utils/format'
import { setThemeMode, themeState, useIsNarrow, useIsMobile } from '@/utils/themeState'

const router = useRouter()
const route = useRoute()
const dialog = useDialog()

const isMobile = useIsMobile()
const isNarrow = useIsNarrow()
const collapsed = ref(false)
const drawerOpen = ref(false)

// 窄屏笔记本自动折叠侧栏；一旦用户手动展开过就不再自动干预。
const userToggled = ref(false)
watch(
  isNarrow,
  (narrow) => {
    if (!userToggled.value) collapsed.value = narrow
  },
  { immediate: true }
)

function toggleCollapse() {
  userToggled.value = true
  collapsed.value = !collapsed.value
}

function renderIcon(icon: VueComponent) {
  return () => h(NIcon, null, { default: () => h(icon) })
}

/** 菜单项点击后关闭抽屉，否则移动端点完导航抽屉还盖在页面上。 */
function closeDrawerOnMobile() {
  if (isMobile.value) drawerOpen.value = false
}

/**
 * 图钉按钮：点一下切换置顶。
 * stopPropagation 必须加 —— 否则点击会同时被 n-menu 当成"选中该项"而触发导航。
 */
const PinToggle = defineComponent({
  props: { owner: { type: Object as PropType<OwnerAggregate>, required: true } },
  setup(props) {
    return () =>
      h(
        NIcon,
        {
          size: 15,
          // 行内样式而非 scoped class：该节点由渲染函数产出，
          // n-menu 的 extra 区域不受 scoped 属性覆盖（同一文件既有的做法）。
          style: {
            color: props.owner.pinned ? 'var(--color-primary)' : 'var(--color-text-muted)',
            cursor: 'pointer',
            display: 'inline-flex',
            // 触控目标偏小，补一点内边距，移动端也好点
            padding: '2px',
            borderRadius: '4px'
          },
          title: props.owner.pinned ? '取消置顶' : '置顶',
          onClick: (e: MouseEvent) => {
            e.stopPropagation()
            e.preventDefault()
            togglePin(props.owner.user_id)
          }
        },
        { default: () => h(props.owner.pinned ? Pin : PinOutline) }
      )
  }
})

// 「全部文件」是父级 tab，展开后按用户列出子 tab —— 想找谁的文件直接点，
// 不必先进入列表再用筛选器找。置顶的用户排在前面（顺序由服务端给）。
const menuOptions = computed<MenuOption[]>(() => {
  const children: MenuOption[] = [
    {
      label: () => h(RouterLink, { to: '/files' }, { default: () => '全部人员' }),
      key: '/files',
      icon: renderIcon(PersonCircleOutline)
    },
    ...ownersState.items.map((o) => ({
      // label 里直接带文件数与占用：省掉一行说明，也让"谁的文件多"一眼可见
      label: () =>
        h(RouterLink, { to: `/files?owner=${o.user_id}` }, {
          default: () => `${o.name} · ${o.file_count}`
        }),
      key: `/files?owner=${o.user_id}`,
      // 图钉即置顶开关：功能显而易见，不需要额外文案
      extra: () => h(PinToggle, { owner: o })
    }))
  ]

  const opts: MenuOption[] = [
    {
      label: () => h(RouterLink, { to: '/files/mine' }, { default: () => '我的文件' }),
      key: '/files/mine',
      icon: renderIcon(DocumentTextOutline)
    },
    {
      label: '全部文件',
      key: 'files-group',
      icon: renderIcon(FolderOpenOutline),
      children
    }
  ]
  if (isAdmin()) {
    opts.push(
      { type: 'divider', key: 'd1' },
      {
        label: () => h(RouterLink, { to: '/admin/workbench' }, { default: () => '工作台' }),
        key: '/admin/workbench',
        icon: renderIcon(SpeedometerOutline)
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
        label: () => h(RouterLink, { to: '/admin/settings' }, { default: () => '系统管理' }),
        key: '/admin/settings',
        icon: renderIcon(HardwareChipOutline)
      }
    )
  }
  return opts
})

// 子项 key 形如 /files?owner=<id>，因此选中态要带查询串；
// 管理员页面则直接用路径。
const activeKey = computed(() => {
  if (route.path.startsWith('/admin/')) return route.path
  if (route.path === '/files') {
    const owner = route.query.owner
    return owner ? `/files?owner=${owner}` : '/files'
  }
  return route.path
})

// 「全部文件」默认展开：用户子 tab 是主要导航方式，收起等于藏起来。
// 用受控变量而非 default-expanded-keys，否则导航后会被 n-menu 收回。
const expandedKeys = ref<string[]>(['files-group'])
watch(
  () => route.path,
  (p) => {
    if (p.startsWith('/files') && !expandedKeys.value.includes('files-group')) {
      expandedKeys.value = [...expandedKeys.value, 'files-group']
    }
  }
)

/** 当前页面标题（移动端顶栏显示，替代面包屑）。 */
const pageTitle = computed(() => (route.meta.title as string) || '局域网文件助手')

/** 用户菜单：外观三档 + 个人设置 + 退出。外观档位显示对勾，避免再放一个独立切换键。 */
const themeOptions = computed<MenuOption[]>(() => {
  const modes = [
    { key: 'theme:auto', label: '跟随系统', icon: SunnyOutline },
    { key: 'theme:light', label: '浅色', icon: SunnyOutline },
    { key: 'theme:dark', label: '深色', icon: MoonOutline }
  ]
  return modes.map((m) => ({
    key: m.key,
    label: m.label,
    icon: renderIcon(m.icon),
    // 当前档位显示对勾：不需要额外文案说明哪个是选中的
    extra: () =>
      themeState.mode === m.key.replace('theme:', '')
        ? h(NIcon, { color: 'var(--color-primary)' }, { default: () => h(CheckmarkOutline) })
        : null
  }))
})

const myUsage = computed(() => {
  const u = state.user
  if (!u) return ''
  return `${u.file_count} 个文件 · ${formatBytes(u.used_bytes)}`
})

const userOptions = computed<MenuOption[]>(() => [
  {
    // 使用量放进悬浮菜单：它属于"我的账号信息"，常驻顶栏只是占地方。
    key: 'usage',
    type: 'render',
    render: () =>
      h('div', { class: 'menu-usage' }, [
        h('div', { class: 'menu-usage__name' }, state.user?.name || ''),
        h('div', { class: 'menu-usage__sub' }, myUsage.value)
      ])
  },
  { type: 'divider', key: 'd-usage' },
  // 外观做成二级悬浮菜单（普通项带 children，Naive UI 自动渲染箭头与级联面板），
  // 比原来平铺三项更省空间，也符合"设置类选项收进子菜单"的惯例。
  { label: '外观', key: 'theme', icon: renderIcon(SunnyOutline), children: themeOptions.value },
  { label: '个人设置', key: 'profile', icon: renderIcon(PersonOutline) },
  { type: 'divider', key: 'd2' },
  { label: '退出登录', key: 'logout', icon: renderIcon(LogOutOutline) }
])

// 进入主框架即拉取用户目录（顶栏与菜单都要用），失败不弹错：
// 菜单缺子项不影响其它功能，页面内的请求会各自给出提示。
onMounted(() => {
  loadOwners().catch(() => {})
})

function onUserSelect(key: string) {
  if (key.startsWith('theme:')) {
    setThemeMode(key.replace('theme:', '') as 'auto' | 'light' | 'dark')
    return
  }
  if (key === 'profile') {
    router.push('/profile')
    return
  }
  if (key === 'logout') {
    confirmLogout()
  }
}

function confirmLogout() {
  dialog.warning({
    title: '退出登录',
    content: '确定要退出当前账号吗？',
    positiveText: '退出',
    negativeText: '取消',
    onPositiveClick: () => {
      clearToken()
      clearUser()
      router.push('/login')
    }
  })
}
</script>

<template>
  <n-layout class="app-shell" :has-sider="!isMobile">
    <!-- 桌面端侧栏；移动端不渲染（改用下面的抽屉） -->
    <n-layout-sider
      v-if="!isMobile"
      bordered
      collapse-mode="width"
      :collapsed-width="64"
      :width="208"
      :collapsed="collapsed"
      show-trigger
      @collapse="collapsed = true"
      @expand="collapsed = false"
    >
      <div class="brand" :class="{ compact: collapsed }">
        <div class="brand-mark" aria-hidden="true">
          <n-icon :size="18"><folder-open-outline /></n-icon>
        </div>
        <span v-if="!collapsed" class="brand-text">局域网文件助手</span>
      </div>
      <n-menu
        v-model:expanded-keys="expandedKeys"
        :value="activeKey"
        :collapsed="collapsed"
        :collapsed-width="64"
        :collapsed-icon-size="20"
        :options="menuOptions"
        :indent="18"
      />
    </n-layout-sider>

    <n-layout :native-scrollbar="false">
      <n-layout-header bordered class="topbar">
        <div class="topbar-left">
          <!-- 移动端：汉堡键打开抽屉导航 -->
          <button
            v-if="isMobile"
            type="button"
            class="icon-button"
            aria-label="打开导航菜单"
            @click="drawerOpen = true"
          >
            <n-icon :size="22"><menu-outline /></n-icon>
          </button>
          <!-- 桌面端：折叠侧栏 -->
          <button
            v-else
            type="button"
            class="icon-button"
            :aria-label="collapsed ? '展开侧栏' : '折叠侧栏'"
            @click="toggleCollapse"
          >
            <n-icon :size="20"><menu-outline /></n-icon>
          </button>
          <!-- 顶栏显示当前页标题：品牌已在侧栏/抽屉中，重复显示只是占地方 -->
          <h1 class="topbar-title">{{ pageTitle }}</h1>
        </div>

        <div class="topbar-actions">
          <n-dropdown :options="userOptions" trigger="click" placement="bottom-end" @select="onUserSelect">
            <button type="button" class="user-trigger" aria-label="打开用户菜单">
              <n-avatar round :size="32" class="avatar">
                {{ state.user?.name?.slice(0, 1) || '?' }}
              </n-avatar>
              <span v-if="!isMobile" class="user-meta">
                <span class="user-name">{{ state.user?.name }}</span>
                <span class="user-sub">{{ state.user?.employee_no }}</span>
              </span>
            </button>
          </n-dropdown>
        </div>
      </n-layout-header>

      <n-layout-content class="app-content">
        <router-view />
      </n-layout-content>
    </n-layout>
  </n-layout>

  <!-- 移动端抽屉导航 -->
  <n-drawer v-model:show="drawerOpen" placement="left" :width="248">
    <n-drawer-content body-content-style="padding: 0">
      <div class="brand drawer-brand">
        <div class="brand-mark" aria-hidden="true">
          <n-icon :size="18"><folder-open-outline /></n-icon>
        </div>
        <span class="brand-text">局域网文件助手</span>
      </div>
      <n-menu
        v-model:expanded-keys="expandedKeys"
        :value="activeKey"
        :options="menuOptions"
        :indent="18"
        @update:value="closeDrawerOnMobile"
      />
    </n-drawer-content>
  </n-drawer>
</template>

<style scoped>
.app-shell {
  height: 100vh;
}

/* ---------- 品牌区 ---------- */
.brand {
  display: flex;
  height: var(--header-height);
  align-items: center;
  gap: 10px;
  padding: 0 18px;
  border-bottom: 1px solid var(--color-border-subtle);
  white-space: nowrap;
}

.brand.compact {
  justify-content: center;
  padding: 0;
}

.brand-mark {
  display: grid;
  flex: none;
  width: 32px;
  height: 32px;
  place-items: center;
  border-radius: 10px;
  color: #fff;
  background: var(--gradient-brand);
}

.brand-text {
  min-width: 0;
  overflow: hidden;
  color: var(--color-text-strong);
  font-size: 15px;
  font-weight: 650;
  text-overflow: ellipsis;
}

/* ---------- 顶栏 ---------- */
.topbar {
  display: flex;
  height: var(--header-height);
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 0 20px;
  background: var(--color-surface-translucent);
  backdrop-filter: blur(12px);
}

.topbar-left {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}

.topbar-title {
  min-width: 0;
  margin: 0;
  overflow: hidden;
  color: var(--color-text-strong);
  font-size: 17px;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.topbar-actions {
  display: flex;
  flex: none;
  align-items: center;
  gap: 10px;
}

/* 顶栏弱图标按钮：触控目标 ≥40px，移动端也好点 */
.icon-button {
  display: grid;
  flex: none;
  width: 40px;
  height: 40px;
  place-items: center;
  border: 1px solid transparent;
  border-radius: 10px;
  color: var(--color-text);
  background: transparent;
  cursor: pointer;
  transition:
    background-color 0.2s ease,
    border-color 0.2s ease;
}

.icon-button:hover,
.icon-button:focus-visible {
  border-color: var(--color-primary-border);
  background: var(--color-primary-soft);
  outline: none;
}

.user-trigger {
  display: flex;
  min-height: 44px;
  align-items: center;
  gap: 10px;
  padding: 4px 10px;
  border: 1px solid transparent;
  border-radius: 12px;
  color: inherit;
  background: transparent;
  cursor: pointer;
  transition:
    background-color 0.2s ease,
    border-color 0.2s ease;
}

.user-trigger:hover,
.user-trigger:focus-visible {
  border-color: var(--color-primary-border);
  background: var(--color-primary-soft);
  outline: none;
}

.avatar {
  flex: none;
  color: #fff;
  background: var(--gradient-brand);
}

.user-meta {
  display: grid;
  gap: 1px;
  text-align: left;
}

.user-name {
  color: var(--color-text-strong);
  font-size: 14px;
  font-weight: 600;
  line-height: 1.25;
}

.user-sub {
  color: var(--color-text-muted);
  font-size: 12px;
  line-height: 1.25;
}

/* ---------- 内容区 ---------- */
.app-content {
  padding: var(--content-padding);
  background: var(--page-glow), var(--color-bg);
}

/* ---------- 移动端适配（≤768px）----------
 * 顶栏收窄、内容内边距收窄、品牌区高度与抽屉一致。 */
@media (max-width: 768px) {
  .topbar {
    height: 56px;
    padding: 0 12px;
  }

  .topbar-title {
    font-size: 16px;
  }

  .app-content {
    padding: 14px 12px 24px;
  }

  .drawer-brand {
    height: 56px;
  }
}
</style>
