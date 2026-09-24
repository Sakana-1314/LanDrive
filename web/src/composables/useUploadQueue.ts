// 上传队列的组合式逻辑：持有 UploadManager，负责校验、入队与队列操作。
//
// 为什么从组件里抽出来：上传入口现在是「页面工具条按钮 + 整页拖放」两处，
// 若继续由某个上传组件持有 manager，两个入口就得各自持有一份队列。
// 逻辑放这里，FilesView 的两个入口调用同一份 addFiles 即可。
import { computed, ref } from 'vue'
import { useMessage } from 'naive-ui'
import { errMsg, uploadConfig } from '@/api'
import {
  applyUploadConfig,
  state as userState,
  uploadState,
  validateFile
} from '@/stores/user'
import { UploadManager, sweepPersisted, type UploadTask } from '@/utils/upload'

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

  /** 校验并入队（拖放与选择文件共用）。返回真正入队的数量。 */
  function addFiles(files: File[]): number {
    const m = ensureManager()
    let added = 0
    for (const f of files) {
      const err = validateFile(f)
      if (err) {
        message.error(`${f.name}：${err}`)
        continue
      }
      m.add(f)
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
    m.add(t.file)
    tasks.value = [...m.tasks]
  }

  function clearFinished(): void {
    const m = ensureManager()
    m.tasks.filter((t) => t.state === 'done' || t.state === 'canceled').forEach((t) => m.remove(t))
    tasks.value = [...m.tasks]
  }

  const uploadDisabled = computed(() => configLoaded.value && !uploadState.uploadEnabled)

  return { tasks, uploadDisabled, prepare, addFiles, cancel, retry, remove, clearFinished }
}
