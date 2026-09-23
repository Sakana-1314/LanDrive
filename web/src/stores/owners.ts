// 用户目录（owners）的全局状态：左侧菜单与文件列表共用同一份数据。
//
// 为什么要放到 store 而不是各页面自己拉：
//   - 菜单里的"用户子 tab"与页面里的筛选必须一致，各拉一份会出现
//     「菜单显示已置顶、列表顺序却没变」这类不一致；
//   - 置顶操作在菜单里触发，列表也需要立刻看到新顺序。
// 沿用 reactive 单例（不引入 Pinia，见 AGENTS.md §5）。
import { reactive } from 'vue'
import { listOwners, pinOwner, unpinOwner } from '@/api'
import type { OwnerAggregate } from '@/api/types'

interface State {
  items: OwnerAggregate[]
  loading: boolean
  loaded: boolean
  /** 正在切换置顶的用户 id，用于按钮 loading 与防重复点击。 */
  pending: number | null
}

export const ownersState = reactive<State>({
  items: [],
  loading: false,
  loaded: false,
  pending: null
})

/** 拉取用户目录列表。force=false 时命中缓存直接返回，避免每次进页面都请求。 */
export async function loadOwners(force = false): Promise<void> {
  if (ownersState.loading) return
  if (ownersState.loaded && !force) return
  ownersState.loading = true
  try {
    const res = await listOwners()
    ownersState.items = res.items
    ownersState.loaded = true
  } finally {
    ownersState.loading = false
  }
}

/**
 * 切换置顶状态。服务端负责排序，因此成功后重新拉取列表，
 * 保证顺序与 pinned 标记来自同一份数据源。
 */
export async function togglePin(userId: number): Promise<void> {
  const target = ownersState.items.find((o) => o.user_id === userId)
  if (!target || ownersState.pending !== null) return
  ownersState.pending = userId
  try {
    if (target.pinned) {
      await unpinOwner(userId)
    } else {
      await pinOwner(userId)
    }
    await loadOwners(true)
  } finally {
    ownersState.pending = null
  }
}

/** 退出登录时清空：否则换账号会看到上一个账号的置顶顺序。 */
export function clearOwners(): void {
  ownersState.items = []
  ownersState.loaded = false
  ownersState.pending = null
}
