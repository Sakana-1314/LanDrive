import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { loadMe } from './stores/user'
import { setNetworkUnavailableHandler } from './api'

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
