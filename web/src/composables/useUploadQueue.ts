// 上传队列的组合式逻辑：持有 UploadManager，负责校验、入队、目录准备与队列操作。
//
// 为什么从组件里抽出来：上传入口现在是「页面工具条按钮 + 选择文件夹 + 整页拖放」三处，
// 若继续由某个上传组件持有 manager，几个入口就得各自持有一份队列。
// 逻辑放这里，FilesView 的入口统一调用同一份 addFiles 即可。
import { computed, ref } from 'vue'
import { useMessage } from 'naive-ui'
import { createFolder, errMsg, listFolders, uploadConfig } from '@/api'
import {
  applyUploadConfig,
  state as userState,
  uploadState,
  validateFile
} from '@/stores/user'
import { UploadManager, sweepPersisted, type UploadTask } from '@/utils/upload'
import type { UploadEntry } from '@/utils/uploadEntries'
import { isSafeDirName } from '@/utils/uploadEntries'

export function useUploadQueue(opts: {
  /** 取当前目标目录（0 = 用户根目录）：进到哪层目录就传到哪层。 */
  getFolderId: () => number
  /** 有文件上传完成时回调（用于刷新列表）。 */
  onUploaded: () => void
}) {
  const message = useMessage()

  const tasks = ref<UploadTask[]>([])
  let manager: UploadManager | null = null
  /** 上传策略是否已拉到。未拉到前不做「已暂停」判定，避免误报。 */
  const configLoaded = ref(false)
  let preparePromise: Promise<void> | null = null

  /**
   * 目录路径 → folder id 的缓存，避免同一批文件反复为同一路径建目录/发请求。
   *
   * 键是「父目录 id + 目录名」：同名目录挂在不同的父目录下是两个不同的目录。
   */
  const dirIdCache = new Map<string, number>()
  /** 记录目录是否已确认存在（含服务端返回 409 同名的情况）。 */
  const dirReady = new Map<string, Promise<number>>()

  function ensureManager(): UploadManager {
    if (!manager) {
      manager = new UploadManager(userState.user?.employee_no || 'anon', {
        onUpdate: () => {
          tasks.value = [...tasks.value]
        },
        onDone: (t) => {
          message.success(`「${t.file.name}」已上传`)
          tasks.value = [...tasks.value]
          opts.onUploaded()
        },
        onError: (t) => {
          message.error(`「${t.file.name}」上传失败：${t.error}`)
          tasks.value = [...tasks.value]
        }
      })
    }
    // 每次入队前同步一次目标目录：用户可能已切到别的文件夹。
    manager.setFolderId(opts.getFolderId() || 0)
    return manager
  }

  /**
   * 进入「我的文件」时调用：清掉过期续传记录并拉取上传策略。
   * 幂等且并发安全 —— /files 与 /files/mine 复用同一个组件实例，
   * 每次导航进来都会调一次，不能因此重复发请求或重复弹错。
   */
  function prepare(): Promise<void> {
    if (!preparePromise) {
      preparePromise = (async () => {
        sweepPersisted()
        try {
          applyUploadConfig(await uploadConfig())
        } catch (e) {
          message.error(errMsg(e))
        } finally {
          configLoaded.value = true
        }
      })()
    }
    return preparePromise
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
    const m = ensureManager()
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
        m.add(f, folderId, g.dirs)
        added++
      }
    }
    tasks.value = [...m.tasks]
    return added
  }

  /** 校验并入队纯文件列表（无层级）。返回真正入队的数量。 */
  function addFiles(files: File[]): number {
    const m = ensureManager()
    const base = opts.getFolderId() || 0
    let added = 0
    for (const f of files) {
      const err = validateFile(f)
      if (err) {
        message.error(`${f.name}：${err}`)
        continue
      }
      m.add(f, base, [])
      added++
    }
    tasks.value = [...m.tasks]
    return added
  }

  async function cancel(t: UploadTask): Promise<void> {
    await ensureManager().cancel(t)
    tasks.value = [...tasks.value]
  }

  function remove(t: UploadTask): void {
    ensureManager().remove(t)
    tasks.value = [...tasks.value]
  }

  function retry(t: UploadTask): void {
    if (t.state !== 'error') return
    const m = ensureManager()
    m.remove(t)
    m.add(t.file, t.folderId, t.dirs)
    tasks.value = [...m.tasks]
  }

  function clearFinished(): void {
    const m = ensureManager()
    m.tasks.filter((t) => t.state === 'done' || t.state === 'canceled').forEach((t) => m.remove(t))
    tasks.value = [...m.tasks]
  }

  const uploadDisabled = computed(() => configLoaded.value && !uploadState.uploadEnabled)

  return {
    tasks,
    uploadDisabled,
    prepare,
    addFiles,
    addEntries,
    cancel,
    retry,
    remove,
    clearFinished
  }
}
