// 上传队列的组合式逻辑：负责校验、入队、目录准备与队列操作。
//
// 「我的文件」的两个入口（工具条按钮 / 整页拖放）共用这里的 addFiles/addEntries；
// 队列状态本身放在 stores/upload.ts 的全局单例里，右下角浮窗读同一份数据，
// 因此离开该页面后进度依然可见、可取消。
import { computed, onBeforeUnmount, ref } from 'vue'
import { useMessage } from 'naive-ui'
import { createFolder, errMsg, listFolders, uploadConfig } from '@/api'
import { applyUploadConfig, state as userState, uploadState, validateFile } from '@/stores/user'
import {
  bindUploadUser,
  cancelUploadTask,
  clearFinishedUploads,
  getUploadManager,
  onUploadDone,
  removeUploadTask,
  retryUploadTask,
  sweepUploadSessions,
  uploadQueueState
} from '@/stores/upload'
import type { UploadTask } from '@/utils/upload'
import type { UploadEntry } from '@/utils/uploadEntries'
import { isSafeDirName } from '@/utils/uploadEntries'

export function useUploadQueue(opts: {
  /** 取当前目标目录（0 = 用户根目录）：进到哪层目录就传到哪层。 */
  getFolderId: () => number
  /** 有文件上传完成时回调（用于刷新列表）。 */
  onUploaded: () => void
}) {
  const message = useMessage()

  /** 上传策略是否已拉到（全局状态，浮窗的「已暂停」判定也读它）。 */
  const configLoaded = computed(() => uploadQueueState.configLoaded)

  // 完成/失败的提示由浮窗（UploadPanel）统一发出：上传是跨页面的操作，
  // 用户切走时这一页可能已经卸载，提示不能跟着消失；两处都发则会重复提示。
  // 本页只订阅「传完刷新列表」，页面卸载后自动退订。
  const offDone = onUploadDone(() => opts.onUploaded())
  onBeforeUnmount(offDone)

  /**
   * 上传策略的拉取：全局只做一次（浮窗的「已暂停」判定也读这份状态）。
   *
   * 为什么不允许重复拉：/files 与 /files/mine 复用同一个组件实例，
   * 每次导航进来都会调一次 prepare，靠这个模块级的 promise 保证
   * 只发一次请求，也不会因为并发而重复弹错。
   */
  const configPromise = ref<Promise<void> | null>(null)

  /**
   * 目录路径 → folder id 的缓存，避免同一批文件反复为同一路径建目录/发请求。
   *
   * 键是「父目录 id + 目录名」：同名目录挂在不同的父目录下是两个不同的目录。
   */
  const dirIdCache = new Map<string, number>()
  /** 记录目录是否已确认存在（含服务端返回 409 同名的情况）。 */
  const dirReady = new Map<string, Promise<number>>()

  /**
   * 进入「我的文件」时调用：切换/绑定队列所属账号并拉取上传策略。
   * 幂等且并发安全；重复进入同一账号不会清空正在跑的队列。
   */
  async function prepare(): Promise<void> {
    bindUploadUser(userState.user?.employee_no || 'anon')
    sweepUploadSessions()
    if (!configPromise.value) {
      configPromise.value = (async () => {
        try {
          applyUploadConfig(await uploadConfig())
        } catch (e) {
          // 策略拉取失败不影响上传（服务端仍会校验），但要告诉用户原因。
          message.error(errMsg(e))
        } finally {
          uploadQueueState.configLoaded = true
        }
      })()
    }
    await configPromise.value
  }

  /** 在服务端解析（必要时创建）子目录，返回其 id。 */
  async function resolveDir(parentId: number, name: string): Promise<number> {
    const cacheKey = `${parentId}/${name}`
    const cached = dirIdCache.get(cacheKey)
    if (cached !== undefined) return cached
    const inflight = dirReady.get(cacheKey)
    if (inflight) return inflight

    const p = (async () => {
      // 先查同层是否已有同名目录：拖入的目录树可能与已有结构重名，
      // 直接 POST 会拿到 409，这里复用它而不是让整批判失败。
      try {
        const listing = await listFolders({ owner_id: 0, folder_id: parentId })
        const found = listing.folders.find((f) => f.name === name)
        if (found) {
          dirIdCache.set(cacheKey, found.id)
          return found.id
        }
      } catch {
        /* 查询失败就退回直接创建；创建失败会走下面的分支 */
      }
      try {
        const folder = await createFolder({ name, parent_id: parentId || undefined })
        dirIdCache.set(cacheKey, folder.id)
        return folder.id
      } catch (e) {
        // 并发/已存在：再查一次，能查到就复用（避免整批因同名失败）。
        try {
          const listing = await listFolders({ owner_id: 0, folder_id: parentId })
          const found = listing.folders.find((f) => f.name === name)
          if (found) {
            dirIdCache.set(cacheKey, found.id)
            return found.id
          }
        } catch {
          /* 忽略，抛出原始错误 */
        }
        throw e
      }
    })()
    dirReady.set(cacheKey, p)
    try {
      return await p
    } catch (e) {
      dirReady.delete(cacheKey)
      throw e
    }
  }

  /** 逐级解析 `dirs` 并返回最深层目录 id。 */
  async function resolveDirPath(baseId: number, dirs: string[]): Promise<number> {
    let cur = baseId || 0
    for (const d of dirs) {
      if (!isSafeDirName(d)) continue
      cur = await resolveDir(cur, d)
    }
    return cur
  }

  /**
   * 校验并入队（拖放、选文件、选文件夹共用）。返回真正入队的数量。
   *
   * 带 `dirs` 的条目会先在服务端建出对应层级的目录，再把文件传进去 ——
   * 这是"拖入文件夹只得到一个同名空文件"的修复关键。
   */
  async function addEntries(entries: UploadEntry[]): Promise<number> {
    const m = getUploadManager()
    const base = opts.getFolderId() || 0
    let added = 0

    // 按目录层级分组：同一层只解析一次目录，减少请求与重复建目录。
    const groups = new Map<string, { dirs: string[]; files: File[] }>()
    for (const e of entries) {
      const target = e.dirs.filter(isSafeDirName)
      const key = target.join('/')
      const g = groups.get(key)
      if (g) g.files.push(e.file)
      else groups.set(key, { dirs: target, files: [e.file] })
    }

    for (const g of groups.values()) {
      let folderId = base
      if (g.dirs.length) {
        try {
          folderId = await resolveDirPath(base, g.dirs)
        } catch (err) {
          message.error(`无法创建文件夹「${g.dirs.join('/')}」：${errMsg(err)}`)
          continue
        }
      }
      for (const f of g.files) {
        const err = validateFile(f)
        if (err) {
          message.error(`${f.name}：${err}`)
          continue
        }
        // 每个任务自带目标目录：同一批文件分属不同层级，用共享字段会被后一个覆盖。
        m.setFolderId(folderId)
        m.add(f, folderId, g.dirs)
        added++
      }
    }
    return added
  }

  /** 校验并入队纯文件列表（无层级）。返回真正入队的数量。 */
  function addFiles(files: File[]): number {
    const m = getUploadManager()
    const base = opts.getFolderId() || 0
    let added = 0
    for (const f of files) {
      const err = validateFile(f)
      if (err) {
        message.error(`${f.name}：${err}`)
        continue
      }
      m.setFolderId(base)
      m.add(f, base, [])
      added++
    }
    return added
  }

  const uploadDisabled = computed(() => configLoaded.value && !uploadState.uploadEnabled)

  return {
    // tasks 不再返回：队列状态由全局单例持有，浮窗与页面都直接读
    // stores/upload.ts 的 uploadQueueState（返回副本反而会漏掉更新）。
    uploadDisabled,
    prepare,
    addFiles,
    addEntries,
    cancel: cancelUploadTask,
    remove: (t: UploadTask) => removeUploadTask(t),
    retry: (t: UploadTask) => retryUploadTask(t),
    clearFinished: clearFinishedUploads
  }
}
