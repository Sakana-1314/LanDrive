// 上传条目展平的单元测试（Node 环境直接运行，零依赖）。
//
// 运行：node --experimental-strip-types src/utils/uploadEntries.spec.ts
//
// 这个测试守的是一个真实缺陷：从系统拖入**文件夹**时，
// `dataTransfer.files` 里只有那个文件夹本身（一个 0 字节的 File），
// 只读 files 就会「只上传一个与文件夹同名的空文件」。
// 因此目录必须经 webkitGetAsEntry 递归展开，且展平后的相对层级要完整保留。
import assert from 'node:assert/strict'
import {
  dirsFromRelativePath,
  entriesFromDataTransfer,
  entriesFromFileList,
  hasDirectoryItem,
  isSafeDirName,
  MAX_ENTRIES
} from './uploadEntries.ts'

function file(name: string, rel = '', size = 10): File {
  const f = new File([new Uint8Array(size)], name)
  if (rel) Object.defineProperty(f, 'webkitRelativePath', { value: rel })
  return f
}

// 1) 拖入文件夹：只有文件夹本身时能识别出"这是目录"，而不是当成空文件
{
  const dirFile = file('报表', '', 0)
  // 真实浏览器里目录项没有 size，且 webkitGetAsEntry().isDirectory === true
  const items = [
    { kind: 'file', webkitGetAsEntry: () => ({ isFile: false, isDirectory: true, name: '报表' }) }
  ] as unknown as DataTransferItem[]
  assert.equal(hasDirectoryItem(items), true, '含目录项时必须识别为目录拖放')
  assert.equal(
    hasDirectoryItem([{ kind: 'file', webkitGetAsEntry: () => ({ isFile: true, isDirectory: false, name: 'a.txt' }) }] as unknown as DataTransferItem[]),
    false,
    '纯文件拖放不应误判为目录'
  )
  assert.equal(hasDirectoryItem(null), false, '无 items 时应安全返回 false')
  assert.equal(dirFile.name, '报表')
}

// 2) webkitRelativePath 拆目录：`报表/2026/1.xlsx` → ['报表','2026']
{
  assert.deepEqual(dirsFromRelativePath('报表/2026/1.xlsx'), ['报表', '2026'], '应剥掉文件名只留目录')
  assert.deepEqual(dirsFromRelativePath('1.xlsx'), [], '根层文件没有目录段')
  assert.deepEqual(dirsFromRelativePath(''), [], '空路径是根层')
  assert.deepEqual(dirsFromRelativePath('报表//1.xlsx'), ['报表'], '空段应被忽略')
  assert.deepEqual(dirsFromRelativePath('/报表/1.xlsx'), ['报表'], '前导斜杠应被忽略')
}

// 3) 由 FileList（选文件夹）展平：每个文件都带完整相对目录
{
  const files = [
    file('1.xlsx', '报表/2026/1.xlsx'),
    file('2.xlsx', '报表/2026/2.xlsx'),
    file('说明.txt', '报表/说明.txt'),
    file('root.txt', '')
  ]
  const entries = entriesFromFileList(files)
  assert.equal(entries.length, 4, '应展平出 4 个条目')
  assert.deepEqual(entries[0].dirs, ['报表', '2026'], '深层文件应保留完整层级')
  assert.deepEqual(entries[1].dirs, ['报表', '2026'], '同层文件层级一致')
  assert.deepEqual(entries[2].dirs, ['报表'], '浅层文件只保留一层')
  assert.deepEqual(entries[3].dirs, [], '跟目录文件无层级')
  assert.equal(entries[0].file.name, '1.xlsx', '文件名不应被改动')
}

// 4) 目录名校验：与服务端 SanitizeFolderName 同口径
{
  for (const ok of ['报表', '2026', 'a-b_c.d', '带空格的 名字']) {
    assert.equal(isSafeDirName(ok), true, `${ok} 应被接受`)
  }
  for (const bad of ['', '  ', '.', '..', 'a/b', 'a\\b', 'a:b', 'a*b', 'a?b', 'a"b', 'a<b', 'a>b', 'a|b', 'a\u0000b']) {
    assert.equal(isSafeDirName(bad), false, `${JSON.stringify(bad)} 应被拒绝`)
  }
}

// 5) 上限常量存在且合理（防止误拖超大目录把浏览器拖死）
assert.ok(MAX_ENTRIES > 0 && MAX_ENTRIES <= 10000, '条目上限应是合理的正数')

// 6) 真递归：伪造 FileSystemEntry 树，验证目录被递归展开且层级正确。
//    这是本缺陷的核心路径 —— 旧代码只读 dataTransfer.files，永远走不到这里。
{
  interface FakeFs { isFile: boolean; isDirectory: boolean; name: string
    _file?: File; _children?: FakeFs[] }
  const mkFile = (name: string, content = 'x'): FakeFs =>
    ({ isFile: true, isDirectory: false, name, _file: new File([content], name) })
  const mkDir = (name: string, children: FakeFs[]): FakeFs =>
    ({ isFile: false, isDirectory: true, name, _children: children })

  // 报表/{2026/{1.xlsx,2.xlsx}, 说明.txt}
  const tree = mkDir('报表', [
    mkDir('2026', [mkFile('1.xlsx'), mkFile('2.xlsx')]),
    mkFile('说明.txt')
  ])

  function entryOf(e: FakeFs): unknown {
    if (e.isFile) {
      return { isFile: true, isDirectory: false, name: e.name,
        file: (cb: (f: File) => void) => cb(e._file as File) }
    }
    let served = false
    return {
      isFile: false, isDirectory: true, name: e.name,
      createReader: () => ({
        readEntries: (cb: (batch: unknown[]) => void) => {
          // 真实 readEntries 每次最多 100 条并需反复调用；这里一次给完再给空
          if (served) return cb([])
          served = true
          cb((e._children || []).map(entryOf))
        }
      })
    }
  }

  const dt = {
    files: [] as unknown as FileList,
    items: [{ kind: 'file', webkitGetAsEntry: () => entryOf(tree) }]
  } as unknown as DataTransfer

  const got = await entriesFromDataTransfer(dt)
  const asPath = got.map((e) => [...e.dirs, e.file.name].join('/')).sort()
  assert.deepEqual(
    asPath,
    ['报表/2026/1.xlsx', '报表/2026/2.xlsx', '报表/说明.txt'].sort(),
    '应递归展开目录并保留完整相对层级'
  )
  assert.equal(got.length, 3, '目录本身不应作为条目出现（旧代码正是把它当成了文件）')
  assert.ok(
    !got.some((e) => e.file.size === 0 && e.dirs.length === 0 && e.file.name === '报表'),
    '不得再把文件夹本身当成一个 0 字节文件上传'
  )
}

// 7) 没有 webkitGetAsEntry（不支持 entry 的浏览器）时退回 files，不能丢文件
{
  const files = [file('a.txt', ''), file('b.txt', '')]
  const dt = {
    files,
    items: [{ kind: 'file' }] // 无 webkitGetAsEntry
  } as unknown as DataTransfer
  const got = await entriesFromDataTransfer(dt)
  assert.equal(got.length, 2, '不支持 entry 接口时应退回 files')
  assert.deepEqual(got.map((e) => e.file.name), ['a.txt', 'b.txt'])
}

// 8) 条目上限必须真的生效（防止误拖超大目录把浏览器拖死）
{
  const many = mkFilesInDir(5)
  const dt = {
    files: [] as unknown as FileList,
    items: [{ kind: 'file', webkitGetAsEntry: () => many }]
  } as unknown as DataTransfer
  const got = await entriesFromDataTransfer(dt, { max: 3 })
  assert.equal(got.length, 3, '达到上限后应停止递归')
}

// 9) 空目录：不应产生任何条目（尤其不能退化成"一个同名空文件"）
{
  const empty = mkFilesInDir(0)
  const dt = {
    files: [] as unknown as FileList,
    items: [{ kind: 'file', webkitGetAsEntry: () => empty }]
  } as unknown as DataTransfer
  const got = await entriesFromDataTransfer(dt)
  assert.deepEqual(got, [], '空目录不应入队任何东西')
}

// 10) 目录名非法（含分隔符）时：文件不丢，只是少一层目录，避免整棵树建失败
{
  let served = false
  const badDir = {
    isFile: false, isDirectory: true, name: 'a/b',
    createReader: () => ({
      readEntries: (cb: (b: unknown[]) => void) => {
        if (served) return cb([])
        served = true
        cb([{ isFile: true, isDirectory: false, name: 'x.txt',
          file: (c: (f: File) => void) => c(new File(['x'], 'x.txt')) }])
      }
    })
  }
  const dt = {
    files: [] as unknown as FileList,
    items: [{ kind: 'file', webkitGetAsEntry: () => badDir }]
  } as unknown as DataTransfer
  const got = await entriesFromDataTransfer(dt)
  assert.equal(got.length, 1, '非法目录名不应导致文件被丢弃')
  assert.deepEqual(got[0].dirs, [], '非法目录名应被跳过而不是拼进路径')
}

function mkFilesInDir(n: number): unknown {
  let served = false
  const children = Array.from({ length: n }, (_, i) => ({
    isFile: true, isDirectory: false, name: `f${i}.txt`,
    file: (cb: (f: File) => void) => cb(new File(['x'], `f${i}.txt`))
  }))
  return {
    isFile: false, isDirectory: true, name: 'big',
    createReader: () => ({
      readEntries: (cb: (batch: unknown[]) => void) => {
        if (served) return cb([])
        served = true
        cb(children)
      }
    })
  }
}

console.log('✅ 上传条目展平测试全部通过（文件夹递归、相对层级、目录名校验、上限）')
