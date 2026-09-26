// 侧栏用户子 tab 与置顶菜单的端到端回归（真实浏览器）。
//
// 守的是什么（用户需求）：
//   0. 母 tab「全部文件」**不可点击导航**：它只是展开/收起的分组标题，
//      内部没有链接、点它不改 URL（用户反馈"这个母 tab 怎么能点击呢"）；
//   1. 用户子 tab **只显示姓名**，不显示「姓名 · 文件数」；
//   2. 置顶不再是行内图钉按钮，而是收进「⋯」下拉菜单里的**勾选项**：
//      未置顶时无对勾、已置顶时有对勾，点它能置顶/取消置顶并让顺序真的变；
//   3. 「更多」触发键**贴行尾**、图标是**实心三点**（细线版看不清、认不出是"更多"）；
//      桌面侧栏与移动端抽屉两处都要成立。
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
  // 用 /files/mine 作为入口（任何页面都能看到侧栏）。
  // 不用 `/files`：不带 owner 时它现在会重定向（混合视图已下线），
  // 测试依赖重定向反而掩盖了"侧栏本身是否正常"。
  await page.goto(`${BASE}/files/mine`, { waitUntil: 'networkidle', timeout: 30000 })
  await page.waitForTimeout(1000)

  // --- 0) 母 tab 必须是**纯展开/收起**，不能导航、不能是链接 ---
  //
  // 用户反馈的原话是"全部文件这个母 tab 怎么能点击呢"。守两层：
  //   1. 它内部不能有 <a>（有 <a> 就会跳转，也说明又被当成了导航项）；
  //   2. 点它不能改 URL —— 只该收起/展开子 tab。
  const parentSel = '.n-layout-sider .n-menu-item'
  const parentRow = page.locator(parentSel, { hasText: '全部文件' }).first()
  check(
    (await parentRow.locator('a').count()) === 0,
    '母 tab「全部文件」内部不该有链接（它应只是展开/收起的分组标题）'
  )
  const beforeClick = await page.evaluate(() => ({
    url: location.pathname + location.search,
    kids: document.querySelectorAll('.n-layout-sider .n-submenu-children .n-menu-item').length
  }))
  await parentRow.locator('.n-menu-item-content-header').first().click()
  await page.waitForTimeout(800)
  const afterClick = await page.evaluate(() => ({
    url: location.pathname + location.search,
    kids: document.querySelectorAll('.n-layout-sider .n-submenu-children .n-menu-item').length
  }))
  check(
    afterClick.url === beforeClick.url,
    `点母 tab 不应导航，实际 ${beforeClick.url} → ${afterClick.url}`
  )
  check(
    beforeClick.kids !== afterClick.kids,
    `点母 tab 应展开/收起子 tab，实际子项数没变（${beforeClick.kids}）`
  )
  // 展开回来，后续用例要继续看子 tab
  await parentRow.locator('.n-menu-item-content-header').first().click()
  await page.waitForTimeout(800)
  check(
    (await page.locator('.n-layout-sider .n-submenu-children .n-menu-item').count()) > 0,
    '再次点母 tab 后子 tab 没有展开回来'
  )

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

  // --- 2b) 触发键必须**贴行尾**，而且是**实心三点** ---
  //
  // 为什么这两条也要真浏览器：位置是布局算出来的（grid/flex 谁吃掉剩余空间），
  // 图标实心还是细线要看渲染出来的 SVG 形状 —— 静态检查一个都看不见。
  // 曾经的实际观感：208px 侧栏里触发键落在 x≈64~83（紧跟姓名），
  // 而行内容区一直铺到 x=190，看着像"粘在名字上"，不像行尾的操作入口。
  const geometry = await page.evaluate(() =>
    [...document.querySelectorAll('.n-layout-sider .n-menu-item')]
      .filter((row) => row.querySelector('.n-menu-item-content-header__extra'))
      .map((row) => {
        const trigger = row.querySelector('.owner-menu-trigger')
        const header = row.querySelector('.n-menu-item-content-header')
        const name = row.querySelector('.n-menu-item-content-header a')
        const t = trigger?.getBoundingClientRect()
        const h = header?.getBoundingClientRect()
        const n = name?.getBoundingClientRect()
        return {
          name: name?.innerText.trim(),
          // 触发键右缘距行右缘的空隙，以及它是否真的落在姓名右边一大截之后
          gapToRowRight: t && h ? Math.round(h.right - t.right) : null,
          triggerLeft: t ? Math.round(t.left) : null,
          nameRight: n ? Math.round(n.right) : null,
          // 触发键自身的宽度：用来确认触控目标比图标大（不是个 18px 的裸图标）
          triggerW: t ? Math.round(t.width) : null,
          circles: trigger
            ? [...trigger.querySelectorAll('svg circle')].map((c) => ({
                cx: c.getAttribute('cx'),
                r: Number(c.getAttribute('r')),
                fill: c.getAttribute('fill')
              }))
            : []
        }
      })
  )
  check(geometry.length === 3, `应能量到 3 个用户子 tab 的触发键，实际 ${geometry.length}`)
  for (const g of geometry) {
    // 贴行尾：右缘与标题区右缘基本齐平（允许 2px 的亚像素差）
    check(
      g.gapToRowRight !== null && g.gapToRowRight <= 2,
      `${g.name} 的「更多」触发键没有靠右：距行右缘还有 ${g.gapToRowRight}px`
    )
    // 别退化成"紧跟在姓名后面"（那正是要修掉的样子）
    check(
      g.triggerLeft !== null && g.nameRight !== null && g.triggerLeft - g.nameRight > 20,
      `${g.name} 的「更多」触发键紧跟在姓名后面（姓名右缘 ${g.nameRight}、触发键左缘 ${g.triggerLeft}），没有靠右`
    )
    // 实心三点：3 个圆，且圆点用 currentColor 实心填充（Outline 版是 fill:none + stroke）
    check(
      g.circles.length === 3,
      `${g.name} 的「更多」图标应是三个点，实际 ${g.circles.length} 个圆`
    )
    check(
      g.circles.every((c) => c.fill === 'currentColor' && c.r >= 40),
      `${g.name} 的「更多」图标应是**实心**三点（细线 Outline 版太小看不清），实际 ${JSON.stringify(g.circles)}`
    )
    // 触控目标：28px 的按钮，比 18px 图标大
    check(
      g.triggerW !== null && g.triggerW >= 24,
      `${g.name} 的「更多」触发键触控目标偏小：宽 ${g.triggerW}px`
    )
  }

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

  // --- 5) 移动端抽屉里同样是「贴行尾的实心三点」---
  // 侧栏与抽屉是两处独立的 n-menu（同一份 options），只验侧栏会漏掉抽屉 ——
  // 改布局时最容易只对一处生效。
  const mctx = await browser.newContext({ viewport: { width: 390, height: 844 } })
  const mpage = await mctx.newPage()
  const merrs = []
  mpage.on('pageerror', (e) => merrs.push(e.message))
  await mpage.addInitScript(() => localStorage.setItem('lanfs-token', 'sidebar-check-mobile'))
  await mpage.goto(`${BASE}/files/mine`, { waitUntil: 'networkidle', timeout: 30000 })
  await mpage.waitForTimeout(800)
  // 打开抽屉导航
  await mpage.locator('.topbar .icon-button').first().click()
  await mpage.waitForTimeout(900)

  const mobileGeometry = await mpage.evaluate(() =>
    [...document.querySelectorAll('.n-drawer .n-menu-item')]
      .filter((row) => row.querySelector('.n-menu-item-content-header__extra'))
      .map((row) => {
        const trigger = row.querySelector('.owner-menu-trigger')
        const header = row.querySelector('.n-menu-item-content-header')
        const name = row.querySelector('.n-menu-item-content-header a')
        const t = trigger?.getBoundingClientRect()
        const h = header?.getBoundingClientRect()
        const n = name?.getBoundingClientRect()
        return {
          name: name?.innerText.trim(),
          gapToRowRight: t && h ? Math.round(h.right - t.right) : null,
          triggerLeft: t ? Math.round(t.left) : null,
          nameRight: n ? Math.round(n.right) : null,
          w: t ? Math.round(t.width) : null,
          circles: trigger ? trigger.querySelectorAll('svg circle').length : 0,
          solid: trigger
            ? [...trigger.querySelectorAll('svg circle')].every((c) => c.getAttribute('fill') === 'currentColor')
            : false,
          // 抽屉里不该出现横向溢出（触发键被顶出可视区就是这种症状）
          overflowX: (() => {
            const menu = row.closest('.n-menu')
            return menu ? menu.scrollWidth > menu.clientWidth + 1 : null
          })()
        }
      })
  )
  check(mobileGeometry.length > 0, '移动端抽屉里没有渲染出用户子 tab')
  for (const g of mobileGeometry) {
    check(
      g.gapToRowRight !== null && g.gapToRowRight <= 2,
      `移动端抽屉 ${g.name} 的「更多」触发键没有靠右：距行右缘 ${g.gapToRowRight}px`
    )
    check(
      g.triggerLeft !== null && g.nameRight !== null && g.triggerLeft - g.nameRight > 20,
      `移动端抽屉 ${g.name} 的「更多」触发键紧跟在姓名后面，没有靠右`
    )
    check(
      g.circles === 3 && g.solid,
      `移动端抽屉 ${g.name} 的「更多」图标应是实心三点，实际 ${g.circles} 个圆（实心=${g.solid}）`
    )
    check(
      g.w !== null && g.w >= 24,
      `移动端抽屉 ${g.name} 的「更多」触发键触控目标偏小：宽 ${g.w}px`
    )
    check(g.overflowX === false, `移动端抽屉 ${g.name} 一行出现横向溢出（触发键被顶出可视区）`)
  }
  check(merrs.length === 0, `移动端页面有 JS 运行时错误：${merrs.slice(0, 2).join(' | ')}`)
  await mctx.close()
} finally {
  await browser.close()
  stopAll()
}

if (failures) {
  console.error(`\n❌ 侧栏检查未通过（${failures} 项）：`)
  for (const p of problems) console.error(`   - ${p}`)
  process.exit(1)
}
console.log('✅ 侧栏检查通过：母 tab 只展开不导航，子 tab 只显示姓名，「⋯」贴行尾且是实心三点，下拉里勾选置顶且顺序真的变')
