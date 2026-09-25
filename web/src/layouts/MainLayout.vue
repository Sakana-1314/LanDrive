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
  EllipsisHorizontalOutline,
  FolderOpenOutline,
  HardwareChipOutline,
  LinkOutline,
  LogOutOutline,
  MenuOutline,
  MoonOutline,
  PersonCircleOutline,
  PersonOutline,
  ServerOutline,
  SpeedometerOutline,
  SunnyOutline
} from '@vicons/ionicons5'
import { clearToken } from '@/api'
import PreviewModal from '@/components/PreviewModal.vue'
import UploadPanel from '@/components/UploadPanel.vue'
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
 * 用户子 tab 右侧的「更多」菜单。
 *
 * 为什么从图钉图标改成下拉菜单：图钉是个只能表达一件事的按钮，
 * 而且点下去到底是"置顶"还是"取消置顶"只能靠悬停提示区分，
 * 触屏上根本没有提示。收进菜单后「置顶」成了一个**勾选状态项**，
 * 当前是否已置顶一眼可见。
 */
const OwnerMenu = defineComponent({
  props: { owner: { type: Object as PropType<OwnerAggregate>, required: true } },
  setup(props) {
    const options = computed<MenuOption[]>(() => [
      {
        key: 'pin',
        // 勾选态必须画在 label 里：n-dropdown 不渲染 `extra`
        // （那是 n-menu 的字段），写在 extra 上会静默丢失。
        label: () =>
          h(
            'div',
            {
              style: {
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: '16px',
                minWidth: '76px'
              }
            },
            [
              h('span', '置顶'),
              props.owner.pinned
                ? h(NIcon, { color: 'var(--color-primary)' }, { default: () => h(CheckmarkOutline) })
                : null
            ]
          )
      }
    ])

    function onSelect(key: string) {
      if (key === 'pin') void togglePin(props.owner.user_id)
    }

    return () =>
      h(
        NDropdown,
        {
          options: options.value,
          trigger: 'click',
          placement: 'bottom-end',
          onSelect
        },
        {
          default: () =>
            h(
              NIcon,
              {
                size: 15,
                // 行内样式而非 scoped class：该节点由渲染函数产出，
                // n-menu 的 extra 区域不受 scoped 属性覆盖（同一文件既有的做法）。
                style: {
                  // 已置顶时点亮触发键：否则置顶状态只能在菜单展开后才看得到
                  color: props.owner.pinned ? 'var(--color-primary)' : 'var(--color-text-muted)',
                  cursor: 'pointer',
                  display: 'inline-flex',
                  // 触控目标偏小，补一点内边距，移动端也好点
                  padding: '2px',
                  borderRadius: '4px'
                },
                title: '更多',
                // 拦掉冒泡，否则点它会被 n-menu 当成"选中该项"而触发导航
                // （下拉自己的 click 监听挂在触发键上，不受冒泡被拦的影响）。
                onClick: (e: MouseEvent) => {
                  e.stopPropagation()
                  e.preventDefault()
                }
              },
              { default: () => h(EllipsisHorizontalOutline) }
            )
        }
      )
  }
})

// 「全部文件」是父级 tab：它自身就是"看所有人的文件"，展开后按人列出子 tab ——
// 想找谁的文件直接点，不必先进入列表再筛选。
//
// 两个取舍：
//   - 不再单独放一个「全部人员」子项：父级已经是这个入口，重复一项只是多一次点击；
//   - 名下没有文件的账号不列出（列出也只会点进空列表），
//     因此一个人都没有文件时父级退化成普通链接，不显示可展开的空子菜单。
const menuOptions = computed<MenuOption[]>(() => {
  const withFiles = ownersState.items.filter((o) => o.file_count > 0)
  const children: MenuOption[] = withFiles.map((o) => ({
    // 只显示姓名：侧栏是导航，不是数据看板 —— 带上文件数会让每行变长、
    // 姓名被挤窄，而"谁的文件多"对"点进去找文件"这件事没有帮助。
    label: () =>
      h(RouterLink, { to: `/files?owner=${o.user_id}` }, {
        default: () => o.name
      }),
    key: `/files?owner=${o.user_id}`,
    // 「更多」下拉里勾选置顶
    extra: () => h(OwnerMenu, { owner: o })
  }))

  const allFilesLink = () =>
    h(
      RouterLink,
      {
        to: '/files',
        class: ['all-files-link', { 'is-current': route.path === '/files' && !route.query.owner }],
        // 父级同时要"可点进去看全部"和"可展开"：n-menu 默认把父级的点击
        // 当成展开/收起，于是点它永远回不到"全部文件"。
        // 这里让 label 只负责导航（拦住冒泡，避免又被当成展开）；
        // 展开/收起交给右侧的箭头（n-menu 自己渲染）。
        onClick: (e: MouseEvent) => e.stopPropagation()
      },
      { default: () => '全部文件' }
    )

  const opts: MenuOption[] = [
    {
      label: () => h(RouterLink, { to: '/files/mine' }, { default: () => '我的文件' }),
      key: '/files/mine',
      icon: renderIcon(DocumentTextOutline)
    },
    {
      // 分享管理对所有登录用户开放：需求要求人人能看到所有分享
      label: () => h(RouterLink, { to: '/shares' }, { default: () => '分享管理' }),
      key: '/shares',
      icon: renderIcon(LinkOutline)
    },
    children.length
      ? {
          // 有子项时才可展开；父级本身也能点，用来查看所有人的文件
          label: allFilesLink,
          key: 'files-group',
          icon: renderIcon(FolderOpenOutline),
          children
        }
      : {
          // 没有任何人上传过文件：退化成普通链接，避免展开出空菜单
          label: allFilesLink,
          key: '/files',
          icon: renderIcon(FolderOpenOutline)
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
    if (owner) return `/files?owner=${owner}`
    // 有子项时父级的 key 是 files-group（父级本身就是"看所有人的文件"），
    // 没有子项时父级退化成普通项、key 为 /files。
    return ownersState.items.some((o) => o.file_count > 0) ? 'files-group' : '/files'
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
        <div class="brand-mark">
          <img src="/logo.png" alt="局域网文件助手" width="32" height="32" />
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

  <!--
    右下角常驻的上传进度浮窗。挂在这里而不是页面里：上传会跨页面继续跑，
    队列状态是全局单例（stores/upload.ts），浮窗因此能在任何页面显示进度。
    队列为空时组件自身不渲染，平时不占版面。
  -->
  <PreviewModal />
  <UploadPanel />

  <!-- 移动端抽屉导航 -->
  <n-drawer v-model:show="drawerOpen" placement="left" :width="248">
    <n-drawer-content body-content-style="padding: 0">
      <div class="brand drawer-brand">
        <div class="brand-mark">
          <img src="/logo.png" alt="局域网文件助手" width="32" height="32" />
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

/* 圆角已在 logo.png 的像素里切好，这里不要再叠 border-radius */
.brand-mark {
  display: grid;
  flex: none;
  width: 32px;
  height: 32px;
  place-items: center;
}

.brand-mark img {
  display: block;
  width: 100%;
  height: 100%;
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
