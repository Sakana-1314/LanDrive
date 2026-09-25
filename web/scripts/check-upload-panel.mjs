// 上传进度浮窗的端到端回归测试（真实浏览器）。
//
// 守的是什么：上传进度原先只出现在「我的文件」页面里的队列块，一离开该页面
// 就看不见进度（组件卸载），右下角的浮窗要能**跨页面**继续显示每个文件的进度。
// 纯函数单测只能覆盖汇总口径，覆盖不到"浮窗真的渲染出来、真的在动、切页后还在"。
//
// 用法：
//   node scripts/check-upload-panel.mjs                  # 自带 mock 后端 + dev server
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
    console.log('⚠️  未安装 playwright，跳过上传浮窗检查')
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
  const MOCK_PORT = 18092
  const DEV_PORT = 4176
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
  if (!html.includes('局域网文件助手')) throw new Error('页面不是本项目')
} catch (e) {
  console.error(`❌ ${BASE} 不可用或不是本项目（${e.message}）`)
  stopAll()
  process.exit(1)
}

/** 浮窗当前展示的内容快照（标题、概览、每个任务的进度与状态）。 */
const PANEL_SNAPSHOT = () => {
  const panel = document.querySelector('.upload-panel')
  if (!panel) return null
  const r = panel.getBoundingClientRect()
  // 进度条的真实宽度只体现在 fill 的 max-width 上，必须读它才能判断
  // "进度真的在动"。顺序：第一个是浮窗顶部的总进度，其后每个任务一条
  // （与 UploadQueue 的渲染顺序一致）。
  const bars = [...panel.querySelectorAll('.n-progress-graph-line-fill')].map((el) => {
    const m = (el.getAttribute('style') || '').match(/max-width:\s*([\d.]+)%/)
    return m ? Number(m[1]) : -1
  })
  return {
    title: panel.querySelector('.upload-panel__title')?.textContent?.trim() || '',
    meta: panel.querySelector('.upload-panel__meta')?.textContent?.trim() || '',
    collapsed: panel.classList.contains('is-collapsed'),
    bars,
    tasks: [...panel.querySelectorAll('.task')].map((t) => ({
      name: t.querySelector('.task__name')?.textContent?.trim() || '',
      size: t.querySelector('.task__size')?.textContent?.trim() || '',
      state: t.querySelector('.n-tag')?.textContent?.trim() || ''
    })),
    rect: { left: r.left, right: r.right, top: r.top, bottom: r.bottom },
    viewport: { w: window.innerWidth, h: window.innerHeight }
  }
}

const browser = await chromium.launch()
try {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 860 } })
  const page = await ctx.newPage()
  await page.addInitScript(() => localStorage.setItem('lanfs-token', 'upload-panel-check'))
  await page.goto(`${BASE}/files/mine`, { waitUntil: 'networkidle', timeout: 30000 })
  await page.waitForTimeout(800)

  // 队列为空时浮窗必须完全不渲染：没有上传却在角落挂着一个空面板是噪音。
  check(
    (await page.locator('.upload-panel').count()) === 0,
    '队列为空时不应渲染上传浮窗'
  )

  // 通过隐藏的 file input 入队两个 8MB 文件（mock 每片 1MB、每片 900ms，
  // 每文件 3 并发 → 每个约 3.6s）。文件要够大，否则浮窗还没来得及被观察
  // 就已经传完，后面"切页后仍在传"的断言会变成时序碰运气。
  await page.locator('input[type=file]:not([webkitdirectory])').setInputFiles([
    { name: '季度报表.xlsx', mimeType: 'application/vnd.ms-excel', buffer: Buffer.alloc(12 * 1024 * 1024, 1) },
    { name: '设备台账.csv', mimeType: 'text/csv', buffer: Buffer.alloc(12 * 1024 * 1024, 2) }
  ])

  await page.waitForSelector('.upload-panel', { timeout: 10000 })
  const early = await page.evaluate(PANEL_SNAPSHOT)
  check(!!early, '入队后浮窗没有出现')

  // --- 1) 每个文件都要有自己的条目（这是需求的原话："实时看每个文件的上传进度"）---
  check(early.tasks.length === 2, `浮窗应列出 2 个任务，实际 ${early.tasks.length} 个`)
  for (const want of ['季度报表.xlsx', '设备台账.csv']) {
    check(early.tasks.some((t) => t.name === want), `浮窗缺少文件「${want}」的进度`)
  }

  // --- 2) 位置：右下角，且不越出视口 ---
  const { rect, viewport } = early
  check(
    rect.right > viewport.w / 2 && rect.bottom > viewport.h / 2,
    `浮窗不在右下角（rect=${JSON.stringify(rect)}）`
  )
  check(
    rect.right <= viewport.w + 1 && rect.bottom <= viewport.h + 1 && rect.left >= -1,
    `浮窗超出视口（rect=${JSON.stringify(rect)}, viewport=${JSON.stringify(viewport)}）`
  )

  // --- 3) 标题与概览要说清"在传、传了多少" ---
  check(/正在上传 2 个文件/.test(early.title), `标题应显示进行中数量，实际「${early.title}」`)
  check(/%/.test(early.meta), `概览应带总进度百分比，实际「${early.meta}」`)

  // --- 4) 进度必须真的在动：读进度条本体（fill 的 max-width），
  //        只看标题文字会漏掉一条永远 0% 的假进度条。---
  const pctOf = (s) => Number((s.meta.match(/(\d+)%/) || [])[1] ?? -1)
  const first = await page.evaluate(PANEL_SNAPSHOT)
  check(
    !!first && first.bars.length === 3,
    `浮窗应有 1 条总进度 + 2 条单文件进度，实际 ${first ? first.bars.length : 'null'} 条`
  )
  check(
    !!first && first.bars.every((b) => b >= 0),
    `进度条百分比读不到（bars=${first && JSON.stringify(first.bars)}）`
  )
  // 轮询而不是固定 sleep：分片完成才刷新进度，固定等待会把断言变成时序碰运气。
  await page
    .waitForFunction(
      (base) => {
        const el = document.querySelector('.upload-panel .n-progress-graph-line-fill')
        const m = (el?.getAttribute('style') || '').match(/max-width:\s*([\d.]+)%/)
        return m ? Number(m[1]) > base : false
      },
      first ? first.bars[0] : 0,
      { timeout: 10000 }
    )
    .catch(() => {})
  const later = await page.evaluate(PANEL_SNAPSHOT)
  check(
    !!later && later.bars[0] > first.bars[0],
    `总进度条没有增长（${first.bars[0]}% → ${later && later.bars[0]}%），浮窗没有实时刷新`
  )
  check(
    !!later && pctOf(later) > pctOf(first),
    `标题的百分比没有跟着涨（${pctOf(first)}% → ${later && pctOf(later)}%）`
  )

  // --- 5) 核心回归：切到别的页面后浮窗仍在，并且还在动 ---
  // 用 SPA 导航（点侧栏链接）而不是 page.goto：goto 是整页刷新，
  // 内存里的队列本来就会没了，那样测的是刷新而不是"切页"。
  await page.getByRole('link', { name: '分享管理' }).click()
  await page.waitForURL('**/shares', { timeout: 15000 })
  await page.waitForTimeout(400)
  const other = await page.evaluate(PANEL_SNAPSHOT)
  check(!!other, '切到「分享管理」后浮窗消失（上传进度只在「我的文件」可见）')
  check(
    !!other && other.tasks.length === 2,
    `切页后浮窗任务丢失（实际 ${other ? other.tasks.length : 'null'} 个）`
  )
  check(
    !!other && /正在上传/.test(other.title),
    `切页后上传仍在进行，标题应保持进行中，实际「${other && other.title}」`
  )
  // 切页之后进度条还得继续往前走（组件重建后必须仍绑定同一份队列）
  check(
    !!later && !!other && other.bars[0] > later.bars[0],
    `切页后进度停住了（${later && later.bars[0]}% → ${other && other.bars[0]}%）`
  )

  // --- 5b) 回到「我的文件」：队列不能被清空 ---
  // 「我的文件」每次进入都会 prepare 一次（幂等要求），若那次调用顺手
  // 重建了队列，用户切回来就会看到"进度没了"。这是本改动最容易踩的坑。
  await page.getByRole('link', { name: '我的文件' }).click()
  await page.waitForURL('**/files/mine', { timeout: 15000 })
  await page.waitForTimeout(300)
  const back = await page.evaluate(PANEL_SNAPSHOT)
  check(
    !!back && back.tasks.length === 2,
    `回到「我的文件」后队列被清空了（实际 ${back ? back.tasks.length : 'null'} 个任务）`
  )

  // --- 6) 手动收起/展开：收起后只剩标题一行 ---
  // 前面任一断言失败时浮窗可能已经不在，这里先确认再点，
  // 否则会抛异常中断脚本、把已收集到的失败清单吞掉。
  if (await page.locator('.upload-panel__head').count()) {
    await page.locator('.upload-panel__head').click()
    const collapsed = await page.evaluate(PANEL_SNAPSHOT)
    check(!!collapsed && collapsed.collapsed, '点击标题后浮窗没有收起')
    check(
      (await page.locator('.upload-panel__body').count()) === 0,
      '收起后仍渲染了队列内容（收起没有真正省地方）'
    )
    await page.locator('.upload-panel__head').click()
    check(
      (await page.locator('.upload-panel__body').count()) === 1,
      '再次点击标题后浮窗没有展开'
    )
  } else {
    check(false, '回到「我的文件」后浮窗消失，无法验证收起/展开')
  }

  // --- 7) 全部传完：标题变成完成态，且自动收起 ---
  // 完成提示由全局队列订阅发出，这里也顺带确认"上传成功"的提示还在
  await page.waitForFunction(
    () => {
      const t = document.querySelector('.upload-panel__title')?.textContent || ''
      return /已完成/.test(t)
    },
    { timeout: 30000 }
  ).catch(() => {})
  const done = await page.evaluate(PANEL_SNAPSHOT)
  check(!!done && /已完成 2 个上传/.test(done.title), `完成后标题不对：「${done && done.title}」`)
  check(!!done && done.collapsed, '全部传完后浮窗应自动收起为一行')
  check(!!done && /共 2 个/.test(done.meta), `完成后概览应为总数，实际「${done && done.meta}」`)

  // --- 8) 队列清空后浮窗要消失，不能永久占着右下角 ---
  // 注意：此刻已经自动收起，先展开才能点到「清除已完成」。
  if (done && done.collapsed) await page.locator('.upload-panel__head').click()
  if (await page.locator('.upload-panel__body').count()) {
    await page.getByRole('button', { name: '清除已完成' }).click()
    await page.waitForTimeout(300)
    check(
      (await page.locator('.upload-panel').count()) === 0,
      '清空队列后浮窗仍然存在（没有上传却一直挂在右下角）'
    )
  } else {
    check(false, '队列里没有可清除的任务，无法验证浮窗在清空后消失')
  }

  await ctx.close()

  // --- 9) 窄屏：浮窗不能横向溢出，也不能把页面撑出滚动条 ---
  const mobile = await browser.newContext({ viewport: { width: 390, height: 844 } })
  const mp = await mobile.newPage()
  await mp.addInitScript(() => localStorage.setItem('lanfs-token', 'upload-panel-check'))
  await mp.goto(`${BASE}/files/mine`, { waitUntil: 'networkidle', timeout: 30000 })
  await mp.waitForTimeout(600)
  await mp.locator('input[type=file]:not([webkitdirectory])').setInputFiles([
    { name: '窄屏验证.xlsx', mimeType: 'application/vnd.ms-excel', buffer: Buffer.alloc(3 * 1024 * 1024, 3) }
  ])
  await mp.waitForSelector('.upload-panel', { timeout: 10000 })
  const mob = await mp.evaluate(PANEL_SNAPSHOT)
  check(
    !!mob && mob.rect.left >= -1 && mob.rect.right <= mob.viewport.w + 1,
    `390px 下浮窗超出视口（rect=${mob && JSON.stringify(mob.rect)}）`
  )
  const overflow = await mp.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  )
  check(overflow <= 2, `390px 下浮窗把页面撑出横向滚动 ${overflow}px`)

  // --- 10) 失败态：必须留在展开状态，不能被自动收起藏起来 ---
  // mock 里文件名含「失败」的会话固定返回 500（触发不了重试成功）。
  await mp.locator('input[type=file]:not([webkitdirectory])').setInputFiles([
    { name: '必然失败.xlsx', mimeType: 'application/vnd.ms-excel', buffer: Buffer.alloc(2 * 1024 * 1024, 4) }
  ])
  await mp
    .waitForFunction(
      () => /上传失败/.test(document.querySelector('.upload-panel__title')?.textContent || ''),
      { timeout: 30000 }
    )
    .catch(() => {})
  const failed = await mp.evaluate(PANEL_SNAPSHOT)
  check(
    !!failed && /1 个文件上传失败/.test(failed.title),
    `失败后标题应报出失败数量，实际「${failed && failed.title}」`
  )
  check(
    !!failed && !failed.collapsed,
    '有失败时浮窗被自动收起了（失败被藏起来，用户不会去展开看）'
  )
  check(
    !!failed && failed.tasks.some((t) => t.name === '必然失败.xlsx' && t.state === '失败'),
    `失败任务应逐条标出「失败」，实际 ${failed && JSON.stringify(failed.tasks)}`
  )
  check(
    !!failed && (await mp.locator('.upload-panel__body').count()) === 1,
    '失败时面板内容没有保持可见'
  )
  await mobile.close()
} finally {
  await browser.close()
  stopAll()
}

if (failures) {
  console.error(`\n❌ 上传进度浮窗检查未通过（${failures} 项）：`)
  for (const p of problems) console.error(`   - ${p}`)
  process.exit(1)
}
console.log('✅ 上传进度浮窗检查通过：右下角常驻、逐文件进度实时刷新、切页仍在、传完自动收起')
