// 浏览列表行模型与分页汇总文案的单元测试（Node 环境直接运行，零依赖）。
//
// 运行：node --experimental-strip-types src/utils/fileRows.spec.ts
//
// 守的是"文件夹与文件同列"这件事本身：文件夹必须排在文件前面、
// 只在第 1 页出现（否则每页都重复渲染一批行）、名称为本地排序（不跟服务端排序器走）。
// 这些行为错了不会报错，只会让列表看起来"怪怪的"。
import assert from 'node:assert/strict'
import {
  buildRows,
  compareFolderNames,
  filterFolders,
  folderItemCount,
  listSummary
} from './fileRows.ts'
import type { Folder } from '@/api/types'

function folder(id: number, name: string, fileCount = 0, subCount = 0): Folder {
  return {
    id,
    owner_id: 1,
    parent_id: null,
    name,
    path: name,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    owner_name: '管理员',
    owner_employee_no: 'admin',
    file_count: fileCount,
    used_bytes: 0,
    sub_folder_count: subCount
  }
}

// 文件只需要 id 与名字，其余字段这一层用不到
const fileItem = (id: number, name: string) => ({ id, original_name: name }) as unknown as import('@/api/types').FileItem

// 1) 文件夹必须排在文件前面（资源管理器的默认行为）
{
  const rows = buildRows({
    folders: [folder(1, '报表')],
    files: [fileItem(10, 'a.xlsx')],
    page: 1
  })
  assert.deepEqual(
    rows.map((r) => r.kind),
    ['folder', 'file'],
    '文件夹应排在文件前面'
  )
  assert.equal(rows[0].kind === 'folder' && rows[0].folder.name, '报表')
  assert.equal(rows[1].kind === 'file' && rows[1].file.original_name, 'a.xlsx')
}

// 2) 文件夹按名称排序（与服务端返回顺序无关）。
//    排序口径要点：**数字/字母在前、中文在后**（资源管理器的观感），
//    不能用 localeCompare('zh-Hans-CN') 一把梭 —— 那会把 AB 排到「文档」后面。
{
  const rows = buildRows({
    folders: [folder(1, '报表'), folder(2, '文档'), folder(3, 'AB'), folder(4, '2026')],
    files: [],
    page: 1
  })
  const names = rows.map((r) => (r.kind === 'folder' ? r.folder.name : ''))
  assert.deepEqual(
    names,
    ['2026', 'AB', '报表', '文档'],
    `文件夹应数字/字母在前、中文在后，实际 ${names.join(',')}`
  )
}

// 2b) 数字按数值比较：报表2 要排在 报表10 前面（否则是按字符逐个比）
assert.ok(
  compareFolderNames('报表2', '报表10') < 0,
  '「报表2」应排在「报表10」前面（数字要按数值比）'
)

// 3) 文件夹只在第 1 页列出：翻页时不能再重复出现一批行
//    （listFolders 返回该层全部文件夹，而文件是服务端分页的）
{
  const p1 = buildRows({ folders: [folder(1, '报表')], files: [fileItem(10, 'a')], page: 1 })
  const p2 = buildRows({ folders: [folder(1, '报表')], files: [fileItem(11, 'b')], page: 2 })
  assert.equal(p1.filter((r) => r.kind === 'folder').length, 1, '第 1 页应列出文件夹')
  assert.equal(p2.filter((r) => r.kind === 'folder').length, 0, '第 2 页不应重复列出文件夹')
  assert.equal(p2.length, 1, '第 2 页只应显示该页的文件')
}

// 4) 空目录 + 空文件列表 → 空行（由组件的空状态兜底）
assert.deepEqual(buildRows({ folders: [], files: [], page: 1 }), [])

// 5) 行 key 不能撞：文件夹 id 与文件 id 是两张表的自增，可能相同
{
  const rows = buildRows({ folders: [folder(1, '报表')], files: [fileItem(1, '同名.xlsx')], page: 1 })
  const keys = rows.map((r) => r.key)
  assert.equal(new Set(keys).size, keys.length, '行 key 必须唯一（文件夹/文件 id 可能相同）')
}

// 6) 项数 = 直接文件数 + 子文件夹数
assert.equal(folderItemCount(folder(1, 'a', 3, 2)), 5)
assert.equal(folderItemCount(folder(1, 'a', 3, 0)), 3)
assert.equal(folderItemCount(folder(1, 'a', 0, 0)), 0)

// 6b) 搜索时按文件夹名本地匹配：命中的目录要留下（否则搜「报表」进不去那个目录），
//     不命中的要滤掉（否则结果里混进一批与关键词无关的目录）。
{
  const list = [folder(1, '报表'), folder(2, '文档'), folder(3, '报表2026')]
  assert.deepEqual(
    filterFolders(list, '').map((f) => f.name),
    ['报表', '文档', '报表2026'],
    '空关键词不应过滤掉任何文件夹'
  )
  assert.deepEqual(
    filterFolders(list, '报表').map((f) => f.name),
    ['报表', '报表2026'],
    '关键词应同时命中包含它的目录名'
  )
  assert.deepEqual(filterFolders(list, '不存在的名字'), [], '无命中时应返回空')
  // 大小写不敏感（英文目录名）
  const en = [folder(1, 'Reports')]
  assert.equal(filterFolders(en, 'reports').length, 1, '英文目录名匹配应忽略大小写')
  assert.equal(filterFolders(list, '   ').length, 3, '纯空白关键词等同于不过滤')
}

// 7) 汇总文案必须区分"文件"与"文件夹"：分页只对文件生效
assert.equal(listSummary(0, 12), '共 12 个文件')
assert.equal(listSummary(3, 12), '共 12 个文件 · 3 个文件夹')
assert.equal(listSummary(1, 0), '共 0 个文件 · 1 个文件夹')

console.log('✅ 浏览列表行模型测试全部通过（文件夹在前、只第 1 页、名称排序、key 唯一）')
