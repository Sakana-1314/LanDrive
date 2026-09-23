import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { loadMe } from './stores/user'
import { setNetworkUnavailableHandler } from './api'
// 设计令牌与全局基础样式：必须在挂载前引入，否则首屏拿不到令牌会闪一下无样式。
import './styles.css'

// 网络不通时由拦截器触发：把路由切到「网络不可用」页，
// 文案为需求指定的「无法在此网络下使用，请更换网络再试！」。
setNetworkUnavailableHandler(() => {
  if (router.currentRoute.value.name !== 'network-blocked') {
    router.replace({ name: 'network-blocked' })
  }
})

// 页面加载时若已有令牌，先恢复用户状态，避免刷新后闪回登录页。
loadMe().finally(() => {
  createApp(App).use(router).mount('#app')
})
