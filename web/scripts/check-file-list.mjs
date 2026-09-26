// 文件列表形态的端到端回归测试（真实浏览器）。
//
// 守的是什么：子文件夹此前是表格**上方单独的一坨卡片**，文件是另一个列表 ——
// 同一个目录里的两类东西被拆在两处，用户得上下扫两遍才知道这一层有什么。
// 需求是对齐 Windows 资源管理器：**一个列表，文件夹排在文件前面**。
//
// 纯函数单测（fileRows.spec.ts）只能覆盖"行怎么合成"，覆盖不到"表格真的把它们
// 渲染成同一个列表、文件夹行在上、点进去真的会进目录"。
//
// 用法：
//   node scripts/check-file-list.mjs                    # 自带 mock 后端 + dev server
//   BASE=http://127.0.0.1:5173 node scripts/check-...   # 用已有服务
//
// 说明：需要 playwright（未装时默认跳过；REQUIRE_PLAYWRIGHT=1 时失败而不是跳过，
// 避免"什么都没检查却报绿"）。
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
    console.log('⚠️  未安装 playwright，跳过文件列表检查')
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
  const MOCK_PORT = 18093
  const DEV_PORT = 4178
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

/** 桌面表格的形态快照：文件夹行与文件行是否在**同一个表格**里、顺序如何。 */
const DESKTOP_SNAPSHOT = () => {
  const table = document.querySelector('.desktop-only .n-data-table')
  if (!table) return null
  const tbody = table.querySelector('tbody')
  const rows = [...(tbody?.querySelectorAll('tr') || [])].map((tr) => {
    const nameEl = tr.querySelector('.file-name')
    return {
      name: nameEl?.textContent?.trim() || '',
      // 文件夹行有 .folder-name，且第一列没有扩展名角标
      isFolder: !!tr.querySelector('.folder-name'),
      sizeCol: (tr.querySelectorAll('td')[1]?.textContent || '').trim(),
      btns: [...tr.querySelectorAll('button')].map((b) => b.textContent.trim()).filter(Boolean)
    }
  })
  return {
    rows,
    // 旧的静态文件夹网格若还在，这里会 >0
    legacyGrids: document.querySelectorAll('.folder-grid').length
  }
}

const browser = await chromium.launch()
try {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
  const page = await ctx.newPage()
  const errs = []
  page.on('pageerror', (e) => errs.push(e.message))
  await page.addInitScript(() => localStorage.setItem('lanfs-token', 'file-list-check'))
  // 记录 /api/files 的排序查询串（断言排序键没被改名改坏）
  const sortRequests = []
  page.on('request', (r) => {
    if (r.url().includes('/api/files?')) sortRequests.push(r.url().split('/api/files?')[1])
  })
  // mock 的根目录有「报表」「文档」两个文件夹 + 12 个文件
  await page.goto(`${BASE}/files/mine`, { waitUntil: 'networkidle', timeout: 30000 })
  await page.waitForTimeout(900)

  const snap = await page.evaluate(DESKTOP_SNAPSHOT)
  check(!!snap, '桌面表格没有渲染出来')
  if (!snap) throw new Error('无表格，后续断言无意义')

  // --- 1) 核心：文件夹和文件必须在**同一个列表**里 ---
  check(
    snap.legacyGrids === 0,
    `页面上还残留独立的文件夹卡片区（.folder-grid × ${snap.legacyGrids}）——文件夹不该单独放上面`
  )
  const folderRows = snap.rows.filter((r) => r.isFolder)
  const fileRows = snap.rows.filter((r) => !r.isFolder)
  check(folderRows.length === 2, `同一个列表里应有 2 个文件夹行，实际 ${folderRows.length} 个`)
  check(fileRows.length > 0, '列表里没有文件行')

  // --- 1b) 排序键必须仍是后端白名单里的 `original_name` ---
  //     这次改了「名称」列的 key（原为 original_name），若被改成 name，
  //     服务端 sortColumn 会静默掉回 created_at —— 表面看着能排，实际排错了。
  await page.locator('.desktop-only thead th', { hasText: '名称' }).first().click()
  await page.waitForTimeout(1000)
  check(
    sortRequests.some((q) => /sort=original_name/.test(q)),
    `按名称排序时应把 sort=original_name 发给后端，实际收到 ${JSON.stringify(sortRequests)}`
  )

  // --- 2) 文件夹排在文件前面（资源管理器的默认） ---
  const firstFileAt = snap.rows.findIndex((r) => !r.isFolder)
  const lastFolderAt = snap.rows.map((r) => r.isFolder).lastIndexOf(true)
  check(
    firstFileAt === -1 || lastFolderAt < firstFileAt,
    `文件夹没有排在文件前面（最后一个文件夹在第 ${lastFolderAt} 行，第一个文件在第 ${firstFileAt} 行）`
  )

  // --- 3) 文件夹名要在列表里，且按名称排序（ASCII/数字在前、中文在后） ---
  const folderNames = folderRows.map((r) => r.name)
  check(
    folderNames.includes('报表') && folderNames.includes('文档'),
    `文件夹行缺少预期目录，实际 ${JSON.stringify(folderNames)}`
  )
  check(
    JSON.stringify(folderNames) === JSON.stringify(['报表', '文档']),
    `文件夹应按名称排序（报表 < 文档），实际 ${JSON.stringify(folderNames)}`
  )

  // --- 4) 文件夹没有"大小/到期"，显示项数；不能显示成 0 B ---
  const reportRow = snap.rows.find((r) => r.isFolder && r.name === '报表')
  check(!!reportRow, '没有找到「报表」行')
  check(
    !!reportRow && /项/.test(reportRow.sizeCol),
    `文件夹的大小列应显示项数，实际「${reportRow && reportRow.sizeCol}」`
  )

  // --- 5) 文件夹行有打开/分享/重命名/删除，文件行有预览/下载/重命名/删除 ---
  check(
    !!reportRow && ['打开', '分享', '重命名', '删除'].every((t) => reportRow.btns.includes(t)),
    `文件夹行缺少操作按钮，实际 ${JSON.stringify(reportRow && reportRow.btns)}`
  )
  const fileRow = snap.rows.find((r) => !r.isFolder)
  check(
    !!fileRow && fileRow.btns.includes('预览') && fileRow.btns.includes('下载'),
    `文件行缺少预览/下载，实际 ${JSON.stringify(fileRow && fileRow.btns)}`
  )

  // --- 6) 汇总文案要区分文件与文件夹（分页只对文件生效） ---
  const pagerText = (await page.locator('.n-pagination').first().innerText().catch(() => '')) || ''
  check(
    /个文件/.test(pagerText) && /个文件夹/.test(pagerText),
    `分页汇总应区分文件与文件夹，实际「${pagerText.replace(/\n/g, ' ')}」`
  )

  // --- 7) 点文件夹行真的会进入该目录 ---
  await page.locator('.desktop-only tbody tr').first().locator('.file-name').click()
  await page.waitForTimeout(1200)
  check(
    /folder=/.test(page.url()),
    `点文件夹行没有进入目录（地址仍是 ${page.url()}）`
  )
  // 进入「报表」后应有面包屑显示当前目录
  const crumbs = await page.locator('.crumbs').innerText().catch(() => '')
  check(/报表/.test(crumbs), `进入目录后面包屑没有反映当前位置，实际「${crumbs.replace(/\n/g, ' ')}」`)

  // --- 7b) 搜索：命中的文件夹要留下，不命中的要滤掉 ---
  //     服务端的关键字只作用于文件，文件夹靠本地名称匹配 ——
  //     否则搜「报表」时进不去那个明明叫「报表」的目录。
  await page.goto(`${BASE}/files/mine`, { waitUntil: 'networkidle', timeout: 30000 })
  await page.waitForTimeout(900)
  await page.locator('.toolbar__search input').fill('报表')
  await page.keyboard.press('Enter')
  await page.waitForTimeout(1200)
  const searched = await page.evaluate(DESKTOP_SNAPSHOT)
  const searchedFolders = (searched?.rows || []).filter((r) => r.isFolder).map((r) => r.name)
  check(
    searchedFolders.includes('报表'),
    `搜索「报表」时应保留命中的文件夹，实际 ${JSON.stringify(searchedFolders)}`
  )
  check(
    !searchedFolders.includes('文档'),
    `搜索「报表」时不应保留无关文件夹，实际 ${JSON.stringify(searchedFolders)}`
  )
  await page.locator('.toolbar__search input').fill('')
  await page.keyboard.press('Enter')
  await page.waitForTimeout(1000)

  // --- 8) 移动端也必须是同一个列表（文件夹行 + 文件行混排） ---
  const mobile = await browser.newContext({ viewport: { width: 390, height: 844 } })
  const mp = await mobile.newPage()
  await mp.addInitScript(() => localStorage.setItem('lanfs-token', 'file-list-check'))
  await mp.goto(`${BASE}/files/mine`, { waitUntil: 'networkidle', timeout: 30000 })
  await mp.waitForTimeout(900)
  const msnap = await mp.evaluate(() => ({
    cards: [...document.querySelectorAll('.mobile-only .file-card')].map((c) => ({
      name: c.querySelector('.file-card__name')?.textContent?.trim() || '',
      isFolder: !!c.querySelector('.n-icon svg') && c.textContent.includes('项')
    })),
    legacyGrids: document.querySelectorAll('.folder-grid').length
  }))
  check(msnap.legacyGrids === 0, '移动端还残留独立的文件夹卡片区')
  check(
    msnap.cards.length > 0 && msnap.cards[0].isFolder,
    `移动端第一个卡片应是文件夹，实际 ${JSON.stringify(msnap.cards.slice(0, 3))}`
  )
  check(
    msnap.cards.some((c) => c.isFolder) && msnap.cards.some((c) => !c.isFolder),
    '移动端列表里没有同时出现文件夹与文件'
  )
  // 移动端文件夹卡片要有"可进入"的箭头：没有静态"打开"按钮，光靠可点击看不出来
  const enterArrows = await mp.evaluate(
    () => document.querySelectorAll('.mobile-only .file-card__enter').length
  )
  check(enterArrows === 2, `移动端文件夹卡片应各有一个进入箭头，实际 ${enterArrows} 个`)
  const overflow = await mp.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  )
  check(overflow <= 2, `390px 下文件列表横向溢出 ${overflow}px`)
  await mobile.close()

  // --- 9) 永久有效期：显示「永久」+ 二次确认里提示 100G 配额 ---
  //
  // 需求：支持把有效期设为永久，但要二次确认并提示永久空间只有 100G（可配置）。
  // 这里守的两个点都是"渲染结果"，单测与类型检查看不见：
  //   a) 永久文件（后端 days_left=0、expires_at=null）**不能**显示成"今天到期"；
  //   b) 点「设为永久」必须弹确认框，且框里带永久空间数字 —— 不能一点就生效。
  await page.goto(`${BASE}/files/mine`, { waitUntil: 'networkidle', timeout: 30000 })
  await page.waitForTimeout(1000)
  const expiryOf = async (name) =>
    page.evaluate((n) => {
      const tr = [...document.querySelectorAll('.desktop-only tbody tr')].find((r) =>
        r.innerText.includes(n)
      )
      if (!tr) return null
      return [...tr.querySelectorAll('.n-tag')].map((t) => t.innerText.trim()).join('|')
    }, name)
  const permBadge = await expiryOf('永久保留的台账.xlsx')
  check(!!permBadge, '找不到"已是永久"的 mock 文件行')
  check(/永久/.test(permBadge || ''), `已永久文件的到期列应显示「永久」，实际「${permBadge}」`)
  check(
    !/今天|明天|天后/.test(permBadge || ''),
    `已永久文件不该显示成倒计时（days_left=0 会被误读成今天到期），实际「${permBadge}」`
  )
  // 该行应给"改回有期限"的出口，而不是又显示"设为永久"
  const permRow = page.locator('.desktop-only tbody tr', { hasText: '永久保留的台账.xlsx' }).first()
  check(
    (await permRow.getByText('改回有期限', { exact: true }).count()) > 0,
    '已永久文件应提供「改回有期限」的入口'
  )

  // 点「设为永久」→ 必须出现二次确认，且提到永久空间
  const datedRow = page.locator('.desktop-only tbody tr', { hasText: '车间设备巡检记录表' }).first()
  await datedRow.getByText('设为永久', { exact: true }).click()
  await page.waitForTimeout(900)
  const dialogText = await page
    .locator('.n-dialog')
    .innerText()
    .then((t) => t.replace(/\s+/g, ' ').trim())
    .catch(() => '')
  check(dialogText.length > 0, '点「设为永久」没有弹出二次确认框（一点就生效）')
  check(
    /永久空间|已用|还剩/.test(dialogText),
    `二次确认里应提示永久空间情况，实际「${dialogText}」`
  )
  check(/100/.test(dialogText), `二次确认里应带上可配置的配额数字（默认 100G），实际「${dialogText}」`)
  // 取消后不应改动任何东西（确认框的意义就在这里）
  await page.locator('.n-dialog button', { hasText: '取消' }).first().click()
  await page.waitForTimeout(800)
  const afterCancel = await expiryOf('车间设备巡检记录表.xlsx')
  check(
    !/永久/.test(afterCancel || ''),
    `取消二次确认后文件不该变成永久，实际到期列「${afterCancel}」`
  )

  check(errs.length === 0, `页面有 JS 运行时错误：${errs.slice(0, 2).join(' | ')}`)
  await ctx.close()
} finally {
  await browser.close()
  stopAll()
}

if (failures) {
  console.error(`\n❌ 文件列表形态检查未通过（${failures} 项）：`)
  for (const p of problems) console.error(`   - ${p}`)
  process.exit(1)
}
console.log('✅ 文件列表形态检查通过：文件夹与文件同一个列表、文件夹在前、点进即入目录')
