// 分片上传引擎：并发上传、进度统计、断点续传、失败重试。
//
// 断点续传依据保存在 localStorage（键含工号/文件名/大小/最后修改时间），
// 页面刷新后先向服务端确认会话是否仍有效（410 表示已被清理，需重新开始）。
import { cancelUpload, completeUpload, getUpload, initUpload, putChunk } from '@/api'
import type { FileItem, UploadSession } from '@/api/types'
import { uploadState } from '@/stores/user'

/** 并发上传的分片数。 */
const CONCURRENCY = 3
/** 单个分片的最大重试次数。 */
const MAX_RETRY = 3

export type UploadState = 'pending' | 'hashing' | 'uploading' | 'merging' | 'done' | 'error' | 'canceled'

export interface UploadTask {
  id: string
  file: File
  /** 展示进度 0-100 */
  percent: number
  /** 已上传字节 */
  loaded: number
  total: number
  state: UploadState
  /** 每秒字节数 */
  speed: number
  /** 预计剩余秒数 */
  eta: number
  error: string
  /** 服务端会话 ID（初始化后才有） */
  uploadId: string
  /** 已完成的分片数 */
  doneChunks: number
  totalChunks: number
  /** 合并后的文件（done 后才有） */
  result: FileItem | null
  /** 是否由断点续传恢复 */
  resumed: boolean
  startedAt: number
  canceled: boolean
}

const STORE_PREFIX = 'lanfs-upload:'

interface PersistedSession {
  upload_id: string
  chunk_size: number
  done: number[]
  /** 记录时间，用于过期清理 */
  at: number
}

function storeKey(employeeNo: string, file: File): string {
  return `${STORE_PREFIX}${employeeNo}:${file.name}:${file.size}:${file.lastModified}`
}

function readPersisted(key: string): PersistedSession | null {
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return null
    const v = JSON.parse(raw) as PersistedSession
    if (!v.upload_id || !v.chunk_size) return null
    return v
  } catch {
    return null
  }
}

function writePersisted(key: string, v: PersistedSession): void {
  try {
    localStorage.setItem(key, JSON.stringify(v))
  } catch {
    /* 隐私模式下 localStorage 可能不可用，忽略即可 */
  }
}

function clearPersisted(key: string): void {
  try {
    localStorage.removeItem(key)
  } catch {
    /* 同上 */
  }
}

/** 清理超过 3 天的本地续传记录，避免 localStorage 无限增长。 */
export function sweepPersisted(): void {
  try {
    const now = Date.now()
    const dead: string[] = []
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i)
      if (!k || !k.startsWith(STORE_PREFIX)) continue
      const v = readPersisted(k)
      if (!v || now - v.at > 3 * 24 * 3600 * 1000) dead.push(k)
    }
    dead.forEach((k) => localStorage.removeItem(k))
  } catch {
    /* 忽略 */
  }
}

/** 计算小文件的 sha256（大文件跳过，避免无谓耗时）。 */
async function sha256Of(file: File): Promise<string> {
  // 超过 64MB 的文件不预计算，避免浏览器卡顿；服务端合并后仍会算一次真实值。
  if (file.size > 64 * 1024 * 1024) return ''
  try {
    const buf = await file.arrayBuffer()
    const digest = await crypto.subtle.digest('SHA-256', buf)
    return Array.from(new Uint8Array(digest))
      .map((b) => b.toString(16).padStart(2, '0'))
      .join('')
  } catch {
    return ''
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms))
}

function isRetryable(e: unknown): boolean {
  const status = (e as { status?: number })?.status
  if (status === undefined) return true // 网络错误（超时/断网）
  return status >= 500 || status === 408 || status === 429
}

export interface UploadCallbacks {
  onUpdate?: (task: UploadTask) => void
  onDone?: (task: UploadTask) => void
  onError?: (task: UploadTask) => void
}

/** 上传管理器：维护任务列表并驱动实际的分片上传。 */
export class UploadManager {
  tasks: UploadTask[] = []
  private employeeNo = 'anon'
  private callbacks: UploadCallbacks

  constructor(employeeNo: string, callbacks: UploadCallbacks = {}) {
    this.employeeNo = employeeNo || 'anon'
    this.callbacks = callbacks
  }

  private notify(task: UploadTask) {
    this.callbacks.onUpdate?.(task)
  }

  /** 添加上传任务并立即开始。返回任务对象。 */
  add(file: File): UploadTask {
    const task: UploadTask = {
      id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      file,
      percent: 0,
      loaded: 0,
      total: file.size,
      state: 'pending',
      speed: 0,
      eta: 0,
      error: '',
      uploadId: '',
      doneChunks: 0,
      totalChunks: 0,
      result: null,
      resumed: false,
      startedAt: Date.now(),
      canceled: false
    }
    this.tasks.unshift(task)
    void this.run(task)
    return task
  }

  /** 取消任务：停止后续分片并通知服务端清理。 */
  async cancel(task: UploadTask): Promise<void> {
    task.canceled = true
    task.state = 'canceled'
    this.notify(task)
    if (task.uploadId) {
      try {
        await cancelUpload(task.uploadId)
      } catch {
        /* 会话可能已被清理 */
      }
      clearPersisted(storeKey(this.employeeNo, task.file))
    }
  }

  /** 从任务列表移除（不删除服务端会话）。 */
  remove(task: UploadTask): void {
    this.tasks = this.tasks.filter((t) => t.id !== task.id)
  }

  private async run(task: UploadTask): Promise<void> {
    const key = storeKey(this.employeeNo, task.file)
    try {
      task.state = 'hashing'
      this.notify(task)
      const sha = await sha256Of(task.file)

      // 断点续传：优先复用本地记录的会话。
      let session: UploadSession | null = null
      const persisted = readPersisted(key)
      if (persisted) {
        try {
          const remote = await getUpload(persisted.upload_id)
          if (
            remote.status === 'uploading' &&
            remote.size_bytes === task.file.size &&
            remote.chunk_size === persisted.chunk_size
          ) {
            session = remote
            task.resumed = remote.uploaded.length > 0
          } else {
            clearPersisted(key)
          }
        } catch (e) {
          // 410 表示会话已被服务端清理；其它错误也不阻塞重新开始。
          const status = (e as { status?: number })?.status
          if (status === 410 || status === 404) clearPersisted(key)
          session = null
        }
      }

      if (!session) {
        session = await initUpload(task.file.name, task.file.size, sha)
      }

      task.uploadId = session.upload_id
      task.totalChunks = session.total_chunks
      const chunkSize = session.chunk_size
      task.total = task.file.size

      // 计算已完成分片的字节数，作为进度起点。
      const doneSet = new Set<number>(session.uploaded)
      task.doneChunks = doneSet.size
      let uploadedBytes = 0
      for (const idx of doneSet) {
        uploadedBytes += Math.min(chunkSize, Math.max(0, task.file.size - idx * chunkSize))
      }
      task.loaded = uploadedBytes
      task.percent = task.file.size === 0 ? (doneSet.has(0) ? 100 : 0) : Math.floor((uploadedBytes / task.file.size) * 100)
      task.state = 'uploading'
      this.notify(task)

      writePersisted(key, {
        upload_id: session.upload_id,
        chunk_size: chunkSize,
        done: Array.from(doneSet),
        at: Date.now()
      })

      // 待上传分片队列。
      const pending: number[] = []
      for (let i = 0; i < session.total_chunks; i++) {
        if (!doneSet.has(i)) pending.push(i)
      }

      // 进度统计用的滑动窗口。
      let windowBytes = uploadedBytes
      let windowAt = Date.now()
      const updateProgress = () => {
        const now = Date.now()
        const dt = (now - windowAt) / 1000
        if (dt >= 0.5) {
          const delta = task.loaded - windowBytes
          task.speed = delta / dt
          const remain = task.total - task.loaded
          task.eta = task.speed > 0 ? remain / task.speed : 0
          windowBytes = task.loaded
          windowAt = now
        }
        task.percent =
          task.total === 0
            ? task.doneChunks >= task.totalChunks
              ? 100
              : 0
            : Math.min(100, Math.floor((task.loaded / task.total) * 100))
        this.notify(task)
      }

      const uploadOne = async (idx: number): Promise<void> => {
        if (task.canceled) return
        const start = idx * chunkSize
        const end = Math.min(task.file.size, start + chunkSize)
        // 0 字节文件：生成一个空分片，保证服务端能收到 total_chunks=1 的记录。
        const blob = task.file.size === 0 ? new Blob([]) : task.file.slice(start, end)

        let attempt = 0
        for (;;) {
          try {
            await putChunk(session!.upload_id, idx, blob)
            break
          } catch (e) {
            attempt++
            if (task.canceled) return
            if (attempt >= MAX_RETRY || !isRetryable(e)) throw e
            await sleep(500 * attempt)
          }
        }

        doneSet.add(idx)
        task.doneChunks = doneSet.size
        // 只有真正新增的分片才累加字节。
        task.loaded = Math.min(task.total, task.loaded + (end - start))
        updateProgress()

        writePersisted(key, {
          upload_id: session!.upload_id,
          chunk_size: chunkSize,
          done: Array.from(doneSet),
          at: Date.now()
        })
      }

      // 固定并发的分片上传。
      let cursor = 0
      const workers: Promise<void>[] = []
      const workerCount = Math.min(CONCURRENCY, Math.max(1, pending.length))
      for (let w = 0; w < workerCount; w++) {
        workers.push(
          (async () => {
            for (;;) {
              if (task.canceled) return
              const i = cursor++
              if (i >= pending.length) return
              await uploadOne(pending[i])
            }
          })()
        )
      }
      await Promise.all(workers)
      if (task.canceled) return

      // 合并：服务端顺序拼接、校验、入库。
      task.state = 'merging'
      task.percent = 100
      this.notify(task)
      const res = await completeUpload(session.upload_id)
      task.result = res.file
      task.state = 'done'
      task.speed = 0
      task.eta = 0
      clearPersisted(key)
      this.notify(task)
      this.callbacks.onDone?.(task)
    } catch (e) {
      if (task.canceled) return
      const msg = (e as Error)?.message || '上传失败'
      task.state = 'error'
      task.error = msg
      this.notify(task)
      this.callbacks.onError?.(task)
    }
  }
}

/** 判断扩展名是否在当前策略允许范围内（前端即时校验，服务端仍会再校验一次）。 */
export function extAllowed(name: string): boolean {
  if (uploadState.allowAll || uploadState.allowedExtensions.length === 0) return true
  const dot = name.lastIndexOf('.')
  const ext = dot > 0 ? name.slice(dot).toLowerCase() : ''
  return !!ext && uploadState.allowedExtensions.includes(ext)
}
