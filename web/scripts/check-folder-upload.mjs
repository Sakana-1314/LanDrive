// 上传文件夹的端到端回归测试（真实浏览器）。
//
// 守的是一个真实缺陷：从系统拖入**文件夹**时，`dataTransfer.files` 里只有那个
// 文件夹本身（一个 0 字节的 File），旧代码直接读 files，于是「只上传了一个与
// 文件夹同名的空文件」，目录内容全丢。修复要经 `webkitGetAsEntry()` 递归展开目录，
// 并按相对层级在服务端建出同样的目录结构。
//
// 为什么必须用真实浏览器：入口写在 Drop/DragEvent 与 FileSystemEntry 上，
// 纯函数单测只能覆盖"展平"这一步，覆盖不到"事件真的被接住并走到网络"。
// 本脚本伪造 FileSystemEntry 树注入 DataTransfer，驱动真实组件，并断言
// 实际发出的「建目录」与「/uploads/init」请求 —— 这才是缺陷的真正现场。
//
// 用法：
//   node scripts/check-folder-upload.mjs                 # 自带 mock 后端 + dev server
//   BASE=http://127.0.0.1:5173 node scripts/check-...    # 用已有服务
//
// 说明：需要 playwright（与 check-responsive 同样处理，未装时默认跳过；
// REQUIRE_PLAYWRIGHT=1 时失败而不是跳过，避免"什么都没检查却报绿"）。
import { spawn } from 'node:child_process'

let chromium
try {
  ;({ chromium } = await import('playwright'))
} catch {
  try {
    const { execSync } = await import('node:child_process')
    const { createRequire } = await import('node:module')
    const globalRoot = execSync('npm root -g', { encoding: 'utf8' }).trim()
    const require = createRequire(import.meta.url)
    ;({ chromium } = require(`${globalRoot}/playwright`))
  } catch {
    if (process.env.REQUIRE_PLAYWRIGHT === '1') {
      console.error('❌ 未安装 playwright，但 REQUIRE_PLAYWRIGHT=1 要求必须执行检查')
      process.exit(1)
    }
    console.log('⚠️  未安装 playwright，跳过上传文件夹检查')
    process.exit(0)
  }
}

let failures = 0
const problems = []
function check(ok, msg) {
  if (ok) return
  failures++
  problems.push(msg)
}

const procs = []
function start(cmd, args, env = {}) {
  // detached 让子进程自成进程组，退出时按组杀，避免留下占端口的孤儿进程
  const p = spawn(cmd, args, { stdio: 'ignore', detached: true, env: { ...process.env, ...env } })
  procs.push(p)
  return p
}
function stopAll() {
  for (const p of procs) {
    if (p.exitCode !== null || p.signalCode !== null) continue
    try {
      process.kill(-p.pid, 'SIGTERM')
    } catch {
      try {
        p.kill('SIGTERM')
      } catch {
        /* 已退出 */
      }
    }
  }
}
process.on('SIGINT', () => {
  stopAll()
  process.exit(130)
})
process.on('uncaughtException', (e) => {
  console.error('未捕获异常:', e)
  stopAll()
  process.exit(1)
})

async function waitFor(url, timeoutMs = 40000) {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    try {
      const r = await fetch(url, { signal: AbortSignal.timeout(2000) })
      if (r.ok) return true
    } catch {
      /* 还没起来 */
    }
    await new Promise((r) => setTimeout(r, 500))
  }
  return false
}

let target = process.env.BASE
if (!target) {
  const MOCK_PORT = 18091
  const DEV_PORT = 4175
  target = `http://127.0.0.1:${DEV_PORT}`
  console.log('未指定 BASE，自动启动 mock 后端与 dev server…')
  start(process.execPath, ['scripts/mock-api.mjs', String(MOCK_PORT)])
  if (!(await waitFor(`http://127.0.0.1:${MOCK_PORT}/api/health`, 15000))) {
    console.error('❌ mock 后端启动失败')
    stopAll()
    process.exit(1)
  }
  start('npx', ['vite', '--port', String(DEV_PORT), '--strictPort', '--host', '127.0.0.1'], {
    VITE_DEV_API_TARGET: `http://127.0.0.1:${MOCK_PORT}`
  })
  if (!(await waitFor(target, 60000))) {
    console.error('❌ dev server 启动失败')
    stopAll()
    process.exit(1)
  }
  process.on('exit', stopAll)
}

const BASE = target

// 预检 1：页面/接口必须真的可用，否则后面全是假通过
try {
  const html = await (await fetch(BASE, { signal: AbortSignal.timeout(5000) })).text()
  // 断言测的是本项目：开发机上同时跑着别的项目，端口被占会测成别人（踩过）
  if (!html.includes('局域网文件助手')) throw new Error('页面不是本项目')
} catch (e) {
  console.error(`❌ ${BASE} 不可用或不是本项目（${e.message}）`)
  stopAll()
  process.exit(1)
}
try {
  const res = await fetch(new URL('/api/health', BASE), { signal: AbortSignal.timeout(5000) })
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
} catch (e) {
  console.error(`❌ ${BASE}/api/health 不可达（${e.message}）`)
  stopAll()
  process.exit(1)
}

const browser = await chromium.launch()
try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
  const inits = []
  const creates = []
  page.on('request', (r) => {
    if (r.url().includes('/uploads/init')) inits.push(safeParse(r.postData()))
  })
  // 目录 id 只在**响应**里（请求体只有 name/parent_id），
  // 从响应拿 id 才能断言文件落到了哪个目录。
  page.on('response', async (r) => {
    if (!r.url().includes('/api/folders') || r.request().method() !== 'POST') return
    try {
      creates.push(await r.json())
    } catch {
      /* 非 JSON 响应忽略，下面的断言会报出来 */
    }
  })
  await page.addInitScript(() => localStorage.setItem('lanfs-token', 'folder-check'))
  await page.goto(`${BASE}/files/mine`, { waitUntil: 'networkidle', timeout: 30000 })
  await page.waitForTimeout(1200)

  // --- 1) 工具条必须有「上传文件夹」入口，且输入带 webkitdirectory ---
  const ui = await page.evaluate(() => {
    const buttons = [...document.querySelectorAll('button')].map((b) => b.textContent.trim())
    const inputs = [...document.querySelectorAll('input[type=file]')]
    return {
      hasFolderButton: buttons.some((t) => t.includes('上传文件夹')),
      hasDirInput: inputs.some((i) => i.hasAttribute('webkitdirectory') && i.multiple)
    }
  })
  check(ui.hasFolderButton, '工具条缺少「上传文件夹」按钮')
  check(ui.hasDirInput, '缺少带 webkitdirectory 的目录选择输入')

  // --- 2) 拖入文件夹：必须递归展开，按层级建目录并把文件放进各自目录 ---
  // 用 mock 里不存在的名字，强制走「创建目录」分支
  await page.evaluate(() => {
    const mkFile = (name, content) => ({
      isFile: true, isDirectory: false, name,
      file: (cb) => cb(new File([content], name, { type: 'text/plain' }))
    })
    const mkDir = (name, children) => ({
      isFile: false, isDirectory: true, name,
      createReader: () => {
        let served = false
        return { readEntries: (cb) => { if (served) return cb([]); served = true; cb(children) } }
      }
    })
    const tree = mkDir('回归目录', [
      mkDir('子层', [mkFile('1.txt', 'a'), mkFile('2.txt', 'b')]),
      mkFile('说明.txt', 'c')
    ])
    const dt = new DataTransfer()
    Object.defineProperty(dt, 'items', { value: [{ kind: 'file', webkitGetAsEntry: () => tree }] })
    // 真实浏览器里拖入文件夹时 dataTransfer.files 就是**空的**（不只是一个
    // 0 字节条目），如果这里塞一个文件夹 File 进去，就变成了一个真实浏览器
    // 不会出现的输入，测试也随之失真。
    Object.defineProperty(dt, 'files', { value: [] })
    Object.defineProperty(dt, 'types', { value: ['Files'] })
    const mk = (type) => {
      const e = new DragEvent(type, { bubbles: true, cancelable: true })
      Object.defineProperty(e, 'dataTransfer', { value: dt })
      return e
    }
    document.dispatchEvent(mk('dragenter'))
    document.dispatchEvent(mk('dragover'))
    document.dispatchEvent(mk('drop'))
  })
  await page.waitForTimeout(3000)

  // 队列现在在右下角的浮窗里（components/UploadPanel.vue），且全部传完会自动
  // 收起成一行 —— 小文件几乎瞬间传完，所以这里必须先展开再读条目，
  // 否则读到的是空列表（不是"目录没展开"，是"内容被收起来了"）。
  if (await page.locator('.upload-panel.is-collapsed .upload-panel__head').count()) {
    await page.locator('.upload-panel .upload-panel__head').click()
    await page.waitForTimeout(200)
  }

  const queue = await page.evaluate(() => ({
    names: [...document.querySelectorAll('.task__name')].map((e) => e.textContent.trim()),
    // 缺陷的核心表征：队列里出现一个与文件夹同名的条目
    folderAsFile: [...document.querySelectorAll('.task__name')].some(
      (e) => e.textContent.trim() === '回归目录'
    )
  }))

  check(!queue.folderAsFile, '仍把文件夹本身当成一个条目上传（缺陷未修复）')
  check(queue.names.length === 3, `应入队 3 个真实文件，实际 ${queue.names.length} 个`)
  for (const want of ['1.txt', '2.txt', '说明.txt']) {
    check(queue.names.includes(want), `队列缺少文件「${want}」（目录内容没有递归展开）`)
  }
  check(
    inits.every((p) => p && p.file_size > 0),
    '仍有 0 字节条目被上传（说明把目录当成了空文件）'
  )

  // 目录必须按层级建出来：先 回归目录，再以它为 parent 建 子层
  const rootCreate = creates.find((c) => c && c.name === '回归目录')
  check(!!rootCreate, '没有为目标文件夹创建目录')
  const subCreate = creates.find((c) => c && c.name === '子层')
  check(!!subCreate, '没有创建子目录「子层」')
  // 子目录必须挂在刚建的父目录下（parent_id 指向父目录的 id）
  check(
    !!subCreate && !!rootCreate && subCreate.parent_id === rootCreate.id,
    '子目录的 parent_id 没有指向父目录（层级没有还原）'
  )

  // 文件要分别落进对应目录：子层两个文件同一 folder_id，且不同于说明.txt 的
  const byName = Object.fromEntries(inits.filter(Boolean).map((p) => [p.file_name, p.folder_id]))
  check(!!byName['1.txt'] && !!byName['1.txt'], '1.txt 未带目标目录')
  check(byName['1.txt'] === byName['2.txt'], '同层文件应落到同一个目录')
  check(
    byName['说明.txt'] !== byName['1.txt'],
    '不同层级的文件落到了同一个目录（层级未生效）'
  )
  check(
    !!rootCreate && byName['说明.txt'] === rootCreate.id,
    '浅层文件没有落到目标目录本身'
  )
  check(!!subCreate && byName['1.txt'] === subCreate.id, '深层文件没有落到子目录')

  await page.close()
} finally {
  await browser.close()
  stopAll()
}

if (failures) {
  console.error(`\n❌ 上传文件夹检查未通过（${failures} 项）：`)
  for (const p of problems) console.error(`   - ${p}`)
  process.exit(1)
}
console.log('✅ 上传文件夹检查通过：目录被递归展开、层级正确、文件各归其目录')

function safeParse(s) {
  try {
    return JSON.parse(s)
  } catch {
    return null
  }
}
