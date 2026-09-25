// 拖放/选择到的条目 → 展平成「文件 + 相对目录」列表。
//
// 为什么需要单独一层：从系统拖入一个**文件夹**时，
// `dataTransfer.files` 里只有那个文件夹本身（一个 0 字节的 File），
// 直接入队就会"只上传一个与文件夹同名的空文件"。
// 完整内容只能通过 `DataTransferItem.webkitGetAsEntry()` 递归读取。
//
// 同理，`<input webkitdirectory>` 选目录时每个 File 带 `webkitRelativePath`
// （如 `报表/2026/1.xlsx`），也要按它还原层级。
//
// 本模块只做"读出来 + 规范化"，不碰网络：便于在 Node 下单测纯函数部分。

/** 一个待上传条目：文件 + 相对目标目录的层级路径（不含文件名）。 */
export interface UploadEntry {
  file: File
  /** 相对目标目录的子路径，如 ['报表','2026']；直接落在目标目录时为空数组。 */
  dirs: string[]
}

/** 单次上传允许的最大文件数，防止误拖超大目录把浏览器拖死。 */
export const MAX_ENTRIES = 2000

/** 目录递归的最大深度，与服务端 maxDepth(16) 对齐。 */
export const MAX_DEPTH = 16

/**
 * 把 `webkitRelativePath` 拆成目录段。
 *
 * 例：`报表/2026/1.xlsx` → `['报表','2026']`；
 * 只在根层选文件时 `webkitRelativePath` 为空串 → `[]`。
 */
export function dirsFromRelativePath(relativePath: string): string[] {
  const parts = String(relativePath || '')
    .split('/')
    .map((s) => s.trim())
    .filter(Boolean)
  // 最后一段是文件名，目录是它前面的部分。
  return parts.slice(0, -1)
}

/** 校验单个目录名：与服务端 `SanitizeFolderName` 同口径，避免建目录时才失败。 */
export function isSafeDirName(name: string): boolean {
  const n = String(name || '').trim()
  if (!n || n === '.' || n === '..') return false
  // 控制字符与 Windows 保留字符：服务端会剥掉，这里直接判为不安全更直观。
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f]/.test(n)) return false
  if (/[/\\:*?"<>|]/.test(n)) return false
  return true
}

/** 把文件列表（含 webkitRelativePath）展平为上传条目。 */
export function entriesFromFileList(files: ArrayLike<File>): UploadEntry[] {
  const out: UploadEntry[] = []
  for (let i = 0; i < files.length; i++) {
    const f = files[i]
    const rel = (f as File & { webkitRelativePath?: string }).webkitRelativePath || ''
    out.push({ file: f, dirs: dirsFromRelativePath(rel) })
  }
  return out
}

/** 判断拖放里是否含目录项（含目录时不能只读 files，必须走 entry 递归）。 */
export function hasDirectoryItem(items: ArrayLike<DataTransferItem> | undefined | null): boolean {
  if (!items) return false
  for (let i = 0; i < items.length; i++) {
    const it = items[i]
    if (it.kind !== 'file') continue
    // Safari/旧浏览器没有 webkitGetAsEntry，此时只能退化成 files。
    const entry = it.webkitGetAsEntry?.()
    if (entry?.isDirectory) return true
  }
  return false
}

/** FileSystemEntry 上我们用到的最小字段（DOM 类型在各浏览器不完全一致）。 */
interface FsEntry {
  isFile: boolean
  isDirectory: boolean
  name: string
  file?: (cb: (f: File) => void, err?: (e: unknown) => void) => void
  createReader?: () => {
    readEntries: (cb: (entries: FsEntry[]) => void, err?: (e: unknown) => void) => void
  }
}

function readFile(entry: FsEntry): Promise<File | null> {
  return new Promise((resolve) => {
    try {
      entry.file?.(
        (f) => resolve(f),
        () => resolve(null)
      )
    } catch {
      resolve(null)
    }
  })
}

/** 读空一个目录的 reader（readEntries 每次最多返回 100 条，必须循环读到空）。 */
function readAllEntries(reader: { readEntries: (cb: (e: FsEntry[]) => void, err?: (e: unknown) => void) => void }): Promise<FsEntry[]> {
  return new Promise((resolve) => {
    const acc: FsEntry[] = []
    const step = () => {
      try {
        reader.readEntries(
          (batch) => {
            if (!batch.length) return resolve(acc)
            acc.push(...batch)
            step()
          },
          () => resolve(acc)
        )
      } catch {
        resolve(acc)
      }
    }
    step()
  })
}

/**
 * 递归读取拖入的文件/文件夹。
 *
 * `baseDirs` 是当前已经走过的目录段（递归时累加）。
 * 返回的每个 entry 都带完整相对目录，供后续在服务端建出同样的层级。
 */
export async function entriesFromDataTransfer(
  dt: DataTransfer,
  opts: { max?: number } = {}
): Promise<UploadEntry[]> {
  const max = opts.max ?? MAX_ENTRIES
  const out: UploadEntry[] = []
  const items = dt.items
  // 没有 items（或浏览器不支持 entry 接口）时退回到 files，至少能传文件。
  if (!items || !items.length || typeof items[0]?.webkitGetAsEntry !== 'function') {
    return entriesFromFileList(dt.files)
  }

  const roots: FsEntry[] = []
  for (let i = 0; i < items.length; i++) {
    if (items[i].kind !== 'file') continue
    const e = items[i].webkitGetAsEntry?.() as FsEntry | null
    if (e) roots.push(e)
  }
  if (!roots.length) return entriesFromFileList(dt.files)

  async function walk(entry: FsEntry, dirs: string[]): Promise<void> {
    if (out.length >= max) return
    if (entry.isFile) {
      const f = await readFile(entry)
      if (f) out.push({ file: f, dirs })
      return
    }
    if (!entry.isDirectory) return
    if (dirs.length >= MAX_DEPTH) return
    const reader = entry.createReader?.()
    if (!reader) return
    const children = await readAllEntries(reader)
    for (const c of children) {
      if (out.length >= max) return
      // 只有目录才把自己的名字并入路径；文件的名字不是目录段。
      const nextDirs = c.isDirectory && isSafeDirName(c.name) ? [...dirs, c.name] : dirs
      await walk(c, nextDirs)
    }
  }

  for (const r of roots) {
    if (out.length >= max) break
    if (r.isDirectory) {
      // 拖进来的顶层目录本身要成为目标目录下的一个子目录。
      const reader = r.createReader?.()
      if (!reader) continue
      const children = await readAllEntries(reader)
      const topDirs = isSafeDirName(r.name) ? [r.name] : []
      for (const c of children) {
        if (out.length >= max) break
        const nextDirs = c.isDirectory && isSafeDirName(c.name) ? [...topDirs, c.name] : topDirs
        await walk(c, nextDirs)
      }
    } else {
      await walk(r, [])
    }
  }
  return out
}
