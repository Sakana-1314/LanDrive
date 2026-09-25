// 侧栏用户子 tab 与置顶菜单的端到端回归（真实浏览器）。
//
// 守的是什么（用户需求）：
//   1. 用户子 tab **只显示姓名**，不显示「姓名 · 文件数」；
//   2. 置顶不再是行内图钉按钮，而是收进「⋯」下拉菜单里的**勾选项**：
//      未置顶时无对勾、已置顶时有对勾，点它能置顶/取消置顶并让顺序真的变。
//
// 为什么要真浏览器：
//   - 「文案里有没有那个计数」是渲染结果，单测与类型检查都看不见；
//   - 勾选态必须落在 n-dropdown 真正会渲染的字段上。这里踩过一个坑：
//     `extra` 是 **n-menu 专有**字段，n-dropdown 根本不读它 ——
//     写在 extra 上的对勾会**静默丢失**（不报错、类型也通过）。
//     只有浏览器能发现"点了置顶但没勾"。
//
// 用法：
//   node scripts/check-sidebar.mjs                    # 自带 mock 后端 + dev server
//   BASE=http://127.0.0.1:5173 node scripts/check-sidebar.mjs
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
    console.log('⚠️  未安装 playwright，跳过侧栏检查')
    process.exit(0)
  }
}

let failures = 0
const problems = []
function check(ok, msg) {
  if (!ok) {
    failures++
    problems.push(msg)
  }
}

const procs = []
function start(cmd, args, env = {}) {
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
  const MOCK_PORT = 18110
  const DEV_PORT = 4198
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

try {
  const html = await (await fetch(BASE, { signal: AbortSignal.timeout(5000) })).text()
  if (!html.includes('局域网文件助手')) throw new Error('页面不是本项目')
} catch (e) {
  console.error(`❌ ${BASE} 不可用或不是本项目（${e.message}）`)
  stopAll()
  process.exit(1)
}

/** 用户子 tab 的文案（只取姓名那一列，不含「更多」触发键）。 */
const TAB_LABELS = () =>
  [...document.querySelectorAll('.n-layout-sider .n-menu-item-content-header a')].map((a) =>
    a.innerText.trim()
  )

const browser = await chromium.launch()
try {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
  const page = await ctx.newPage()
  const errs = []
  page.on('pageerror', (e) => errs.push(e.message))

  await page.addInitScript(() => localStorage.setItem('lanfs-token', 'sidebar-check'))
  await page.goto(`${BASE}/files`, { waitUntil: 'networkidle', timeout: 30000 })
  await page.waitForTimeout(1000)

  // --- 1) 子 tab 只显示姓名，不带文件数 ---
  const labels = await page.evaluate(TAB_LABELS)
  check(labels.length > 0, '侧栏没有渲染出任何用户子 tab')
  const withCount = labels.filter((t) => /·/.test(t) || /\d/.test(t))
  check(
    withCount.length === 0,
    `用户子 tab 不应显示文件数，实际这些项还带着计数：${JSON.stringify(withCount)}`
  )
  // 姓名本身要在（别把文案整没了）
  check(
    labels.includes('李静') && labels.includes('张伟'),
    `子 tab 应显示姓名，实际 ${JSON.stringify(labels)}`
  )

  // --- 2) 置顶收进「⋯」下拉，而不是行内图钉按钮 ---
  const extras = await page.locator('.n-layout-sider .n-menu-item-content-header__extra').count()
  check(extras === 3, `每个用户子 tab 应有一个「更多」触发键，实际 ${extras} 个`)
  // 旧的图钉按钮 title 是「置顶 / 取消置顶」，新的触发键是「更多」：
  // 用 title 判定比比对 svg 路径稳（换图标不会误报）。
  const titles = await page.evaluate(() =>
    [...document.querySelectorAll('.n-layout-sider .n-menu-item-content-header__extra [title]')].map(
      (e) => e.getAttribute('title')
    )
  )
  check(
    titles.length > 0 && titles.every((t) => t === '更多'),
    `子 tab 右侧应是「更多」下拉触发键，实际 title = ${JSON.stringify(titles)}`
  )

  const order = () => page.evaluate(TAB_LABELS)
  // 王强（mock 里未置顶）→ 用它验置顶/取消
  const row = page.locator('.n-layout-sider .n-menu-item', { hasText: '王强' })
  const openMenu = async () => {
    await row.locator('.n-menu-item-content-header__extra .n-icon').click()
    await page.waitForTimeout(700)
  }

  await openMenu()
  check(
    (await page.locator('.n-dropdown-option').count()) > 0,
    '点「⋯」没有展开下拉菜单'
  )
  check(
    (await page.locator('.n-dropdown-option', { hasText: '置顶' }).count()) > 0,
    '下拉菜单里没有「置顶」项'
  )
  // 点「⋯」不应触发导航（会被 n-menu 当成选中该项）
  check(
    !/owner=/.test(page.url()),
    `点「⋯」触发键不应导航到某人的目录，实际 URL ${page.url()}`
  )
  // 未置顶：没有对勾
  check(
    (await page.locator('.n-dropdown-option .n-icon').count()) === 0,
    '未置顶时「置顶」项不应出现对勾'
  )

  // --- 3) 点「置顶」：勾上，且顺序真的变了 ---
  const before = await order()
  await page.locator('.n-dropdown-option', { hasText: '置顶' }).first().click()
  await page.waitForTimeout(1300)
  const after = await order()
  check(
    JSON.stringify(after) !== JSON.stringify(before),
    `置顶后侧栏顺序没有变化（仍为 ${JSON.stringify(after)}）`
  )
  check(
    after.indexOf('王强') < before.indexOf('王强'),
    `被置顶的用户应排到前面（置顶前第 ${before.indexOf('王强')} 位，置顶后第 ${after.indexOf('王强')} 位）`
  )

  // 已置顶：菜单里要出现对勾（勾选态真正渲染出来了）
  await openMenu()
  check(
    (await page.locator('.n-dropdown-option .n-icon').count()) === 1,
    '已置顶时「置顶」项应显示对勾（n-dropdown 不读 extra，勾选态必须画在 label 里）'
  )
  await page.keyboard.press('Escape')
  await page.waitForTimeout(500)

  // --- 4) 再点一次：取消置顶，顺序回到原样 ---
  await openMenu()
  await page.locator('.n-dropdown-option', { hasText: '置顶' }).first().click()
  await page.waitForTimeout(1300)
  const restored = await order()
  check(
    JSON.stringify(restored) === JSON.stringify(before),
    `取消置顶后顺序应回到原样，期望 ${JSON.stringify(before)}，实际 ${JSON.stringify(restored)}`
  )

  check(errs.length === 0, `页面有 JS 运行时错误：${errs.slice(0, 2).join(' | ')}`)
  await ctx.close()
} finally {
  await browser.close()
  stopAll()
}

if (failures) {
  console.error(`\n❌ 侧栏检查未通过（${failures} 项）：`)
  for (const p of problems) console.error(`   - ${p}`)
  process.exit(1)
}
console.log('✅ 侧栏检查通过：子 tab 只显示姓名，「⋯」下拉里勾选置顶且顺序真的变')
