// 上传队列的全局单例（reactive 单例，与 stores/user.ts 一致，不引入 Pinia）。
//
// 为什么必须是单例：队列原先由「我的文件」页面内的 useUploadQueue 持有，
// 组件一卸载、队列就从界面上消失 —— 用户切到别的页面后看不到还有多少没传完，
// 也没有统一的取消入口。右下角浮窗是常驻的，只能读同一份队列状态。
import { computed, reactive } from 'vue'
import { UploadManager, sweepPersisted, type UploadTask } from '@/utils/upload'

export interface UploadQueueState {
  tasks: UploadTask[]
  /** 上传策略是否已拉到；未拉到前不做「已暂停」判定，避免误报。 */
  configLoaded: boolean
}

export const uploadQueueState = reactive<UploadQueueState>({
  tasks: [],
  configLoaded: false
})

let manager: UploadManager | null = null
/** 当前队列绑定的工号；用于识别"换账号了"，同一账号重复进入页面不清队列。 */
let boundEmployeeNo: string | null = null

/** 单个任务完成/失败的通知订阅者：提示与列表刷新都靠它，与页面是否打开无关。 */
const doneListeners = new Set<(task: UploadTask) => void>()
const errorListeners = new Set<(task: UploadTask) => void>()

/** 任务对象是就地修改的，这里复制一份以触发视图更新。 */
function refresh(): void {
  uploadQueueState.tasks = [...(manager?.tasks ?? [])]
}

/** 拿到（必要时创建）全局唯一的 UploadManager。 */
export function getUploadManager(): UploadManager {
  if (!manager) {
    manager = new UploadManager('anon', {
      onUpdate: refresh,
      onDone: (t) => {
        refresh()
        doneListeners.forEach((fn) => fn(t))
      },
      onError: (t) => {
        refresh()
        errorListeners.forEach((fn) => fn(t))
      }
    })
  }
  return manager
}

export function onUploadDone(fn: (task: UploadTask) => void): () => void {
  doneListeners.add(fn)
  return () => doneListeners.delete(fn)
}

export function onUploadError(fn: (task: UploadTask) => void): () => void {
  errorListeners.add(fn)
  return () => errorListeners.delete(fn)
}

/**
 * 登录用户确定后调用：把队列绑定到该工号。返回是否发生了「换账号」。
 *
 * 换账号必须清队列：续传记录按工号区分，旧账号没传完的文件留在这里，
 * 既会显示错归属，重试也会拿错身份去续传。同一账号重复进入页面则不能清
 * （在「我的文件」里切子目录会重复调用本函数）。
 */
export function bindUploadUser(employeeNo: string): boolean {
  const next = employeeNo || 'anon'
  const m = getUploadManager()
  if (boundEmployeeNo === next) return false
  const switched = boundEmployeeNo !== null
  m.tasks.splice(0, m.tasks.length)
  m.setEmployeeNo(next)
  boundEmployeeNo = next
  uploadQueueState.configLoaded = false
  refresh()
  return switched
}

/** 进入「我的文件」时调用：清掉超过 3 天的续传记录（幂等，可重复调用）。 */
export function sweepUploadSessions(): void {
  sweepPersisted()
}

/** 是否还有任务在跑（浮窗据此决定标题文案与折叠时机）。 */
export const uploadsActive = computed(() =>
  uploadQueueState.tasks.some(
    (t) => t.state === 'uploading' || t.state === 'hashing' || t.state === 'merging'
  )
)

export function cancelUploadTask(t: UploadTask): Promise<void> {
  return getUploadManager().cancel(t)
}

export function removeUploadTask(t: UploadTask): void {
  getUploadManager().remove(t)
  refresh()
}

export function retryUploadTask(t: UploadTask): void {
  if (t.state !== 'error') return
  const m = getUploadManager()
  m.remove(t)
  m.add(t.file, t.folderId, t.dirs)
  refresh()
}

export function clearFinishedUploads(): void {
  const m = getUploadManager()
  m.tasks.filter((t) => t.state === 'done' || t.state === 'canceled').forEach((t) => m.remove(t))
  refresh()
}
