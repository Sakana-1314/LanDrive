// 文件浏览器的行模型：把「当前目录的子文件夹」和「当前页的文件」合成一个列表。
//
// 为什么需要：此前子文件夹是表格**上方单独的一坨卡片**、文件是另一个列表，
// 同一个目录里的两类东西被拆在两处 —— 用户得上下扫两遍才知道这一层有什么，
// 而且"上面那排卡片"和"下面的表格"视觉上像是两个无关的东西。
// Windows 资源管理器是一个列表、文件夹排在前面，这里对齐这个行为。
import type { FileItem, Folder } from '@/api/types'

/** 列表里的一行：要么是文件夹，要么是文件。 */
export type BrowserRow =
  | { kind: 'folder'; key: string; folder: Folder }
  | { kind: 'file'; key: string; file: FileItem }

/** 文件夹的「项数」= 直接包含的文件 + 子文件夹（不含更深的层级）。 */
export function folderItemCount(f: Folder): number {
  return (f.file_count || 0) + (f.sub_folder_count || 0)
}

/**
 * 文件夹排序用的比较器。
 *
 * 为什么不直接用 `localeCompare('zh-Hans-CN')`：那个排序会把**中文排在英文和数字后面**
 * （实测 `AB` 排在 `文档` 之后，因为中文按拼音参与整串比较），与资源管理器
 * 「数字/字母在前、中文在后」的观感不符。这里显式分桶：ASCII 开头的一档在前，
 * 各档内用 `numeric` 保证 `报表2` 排在 `报表10` 前面。
 */
const folderCollator = new Intl.Collator('zh-Hans-CN', { numeric: true, sensitivity: 'base' })

function folderBucket(name: string): number {
  return /^[\x00-\x7F]/.test(name) ? 0 : 1
}

export function compareFolderNames(a: string, b: string): number {
  return folderBucket(a) - folderBucket(b) || folderCollator.compare(a, b)
}

/**
 * 按关键词筛选文件夹。
 *
 * 为什么需要：搜索是**服务端对文件**做的（`q` 只匹配文件名/上传者），文件夹不参与。
 * 若搜索时把当前目录的文件夹原样列出来，结果里会混进一批与关键词无关的目录；
 * 但若全都藏起来，用户搜「报表」时又进不去那个明明叫「报表」的目录。
 * 折中：按文件夹名本地匹配，命中的留下 —— 与资源管理器"搜索结果里也能看到
 * 命中的文件夹"一致。
 */
export function filterFolders(folders: Folder[], keyword: string): Folder[] {
  const q = keyword.trim().toLowerCase()
  if (!q) return folders
  return folders.filter((f) => f.name.toLowerCase().includes(q))
}

/**
 * 合成浏览列表：文件夹在前、文件在后。
 *
 * 两个刻意的取舍：
 *  - **文件夹只在第 1 页列出**。文件是服务端分页的，文件夹不是
 *    （listFolders 一次返回该层全部）。若每页都重复渲染同一批文件夹，
 *    翻到第 2 页会看到一批"每页都出现"的行，像是内容重复了。
 *  - 文件夹**按名称本地排序**，不跟着列排序器走。排序器是服务端对文件做的
 *    （`sort`/`order` 只是文件查询参数），文件夹若跟着变会与服务端结果不一致。
 *    资源管理器默认也是文件夹在上、不参与文件排序。
 */
export function buildRows(opts: { folders: Folder[]; files: FileItem[]; page: number }): BrowserRow[] {
  const rows: BrowserRow[] = []
  if (opts.page <= 1) {
    const sorted = [...opts.folders].sort((a, b) => compareFolderNames(a.name, b.name))
    for (const f of sorted) rows.push({ kind: 'folder', key: `folder-${f.id}`, folder: f })
  }
  for (const f of opts.files) rows.push({ kind: 'file', key: `file-${f.id}`, file: f })
  return rows
}

/**
 * 分页条左侧的汇总文案。
 *
 * 必须把「文件」与「文件夹」分开说：分页只对文件生效（itemCount 是文件数），
 * 直接写「共 12 个」会让用户以为列表里一共就 12 行 —— 实际还多了几个文件夹。
 */
export function listSummary(folderCount: number, fileTotal: number): string {
  const parts = [`共 ${fileTotal} 个文件`]
  if (folderCount > 0) parts.push(`${folderCount} 个文件夹`)
  return parts.join(' · ')
}
