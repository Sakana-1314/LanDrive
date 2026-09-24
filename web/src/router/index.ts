import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { clearToken, getToken, isNetworkError, setUnauthorizedHandler } from '@/api'
import { isAdmin, loadMe, state } from '@/stores/user'

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('@/views/LoginView.vue'),
    meta: { public: true, title: '登录' }
  },
  {
    path: '/',
    component: () => import('@/layouts/MainLayout.vue'),
    children: [
      { path: '', redirect: '/files' },
      {
        path: 'files',
        name: 'files',
        component: () => import('@/views/FilesView.vue'),
        meta: { title: '全部文件' }
      },
      {
        path: 'files/mine',
        name: 'files-mine',
        component: () => import('@/views/FilesView.vue'),
        meta: { title: '我的文件', scope: 'mine' }
      },
      // 上传不再单独成页：拖进「我的文件」即可。
      // 保留重定向，避免旧的收藏/书签 404。
      { path: 'upload', redirect: '/files/mine' },
      {
        path: 'shares',
        name: 'shares',
        component: () => import('@/views/SharesView.vue'),
        meta: { title: '分享管理' }
      },
      {
        path: 'profile',
        name: 'profile',
        component: () => import('@/views/ProfileView.vue'),
        meta: { title: '个人设置' }
      },
      {
        path: 'admin',
        redirect: '/admin/workbench'
      },
      {
        path: 'admin/workbench',
        name: 'admin-workbench',
        component: () => import('@/views/admin/WorkbenchView.vue'),
        meta: { title: '工作台', admin: true }
      },
      // 旧路径保留重定向，避免书签失效。
      { path: 'admin/dashboard', redirect: '/admin/workbench' },
      {
        path: 'admin/users',
        name: 'admin-users',
        component: () => import('@/views/admin/UsersView.vue'),
        meta: { title: '用户管理', admin: true }
      },
      {
        path: 'admin/files',
        name: 'admin-files',
        component: () => import('@/views/admin/AdminFilesView.vue'),
        meta: { title: '文件与回收站', admin: true }
      },
      {
        path: 'admin/settings',
        name: 'admin-settings',
        component: () => import('@/views/admin/SettingsView.vue'),
        meta: { title: '系统管理', admin: true }
      }
    ]
  },
  {
    path: '/preview/:id',
    name: 'preview',
    component: () => import('@/views/PreviewView.vue'),
    meta: { title: '文件预览' }
  },
  {
    // 公开分享页：**免登录**，因此放在主框架之外（无侧栏、无顶栏）。
    // 路由守卫只认 meta.public，不会拦它。
    path: '/s/:token',
    name: 'share',
    component: () => import('@/views/ShareView.vue'),
    meta: { public: true, title: '分享的文件' }
  },
  {
    path: '/network-blocked',
    name: 'network-blocked',
    component: () => import('@/views/NetworkBlockedView.vue'),
    meta: { public: true, title: '网络不可用' }
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'not-found',
    component: () => import('@/views/NotFoundView.vue'),
    meta: { public: true, title: '页面不存在' }
  }
]

export const router = createRouter({
  history: createWebHistory(),
  routes
})

setUnauthorizedHandler(() => {
  clearToken()
  if (router.currentRoute.value.name !== 'login') {
    router.push({ name: 'login', query: { redirect: router.currentRoute.value.fullPath } })
  }
})

router.beforeEach(async (to) => {
  document.title = to.meta.title ? `${to.meta.title as string} · 局域网文件助手` : '局域网文件助手'

  if (to.meta.public) return true

  if (!getToken()) {
    return { name: 'login', query: to.fullPath === '/' ? {} : { redirect: to.fullPath } }
  }

  // 刷新页面后内存状态为空，需要重新拉取用户信息。
  if (!state.user) {
    const okUser = await loadMe(true)
    if (!okUser) {
      // 「有令牌但拉取失败」需要区分两种情况：
      //   - 网络不通（内网接口访问不到）→ 提示更换网络；
      //   - 令牌失效 → 回登录页。
      if (getToken() && isNetworkError(state.lastError)) {
        return { name: 'network-blocked' }
      }
      return { name: 'login', query: { redirect: to.fullPath } }
    }
  }

  if (to.meta.admin && !isAdmin()) {
    return { name: 'files' }
  }
  return true
})
