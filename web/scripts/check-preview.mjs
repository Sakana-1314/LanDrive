// 预览弹层的端到端回归（真实浏览器）。
//
// 守的是什么（用户需求）：
//   1. 点「预览」**不再新开标签页**，而是在当前页弹出弹层；
//   2. 弹层内部是一个 **iframe**，预览内容在 iframe 里渲染；
//   3. 预览顶部**没有**「文件名 + 类型/大小 + 下载」那条栏；
//   4. 深色档下弹层与预览面不能用写死的浅色（白底黑字错配）。
//
// 为什么必须用真实浏览器：这三条都是「DOM 里到底有没有这些东西」「点一下是否
// 新开标签」的行为，类型检查与纯函数单测都覆盖不到。tear-off：iframe 内是独立
// 文档，只有浏览器能真正跨进去看。
//
// 用法：
//   node scripts/check-preview.mjs                    # 自带 mock 后端 + dev server
//   BASE=http://127.0.0.1:5173 node scripts/check-preview.mjs
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
    console.log('⚠️  未安装 playwright，跳过预览弹层检查')
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
  const MOCK_PORT = 18095
  const DEV_PORT = 4181
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

/**
 * 弹层现状快照。
 *
 * 关键断言口径：
 *   - `popups` 记录是否发生了 window.open（新开标签）；
 *   - `shell` 是弹层容器；`frameSrc` 取 iframe 的实际地址；
 *   - `barText` 收集预览面里是否还残留「下载」这类顶部栏文案。
 */
const SNAPSHOT = () => {
  const shell = document.querySelector('.preview-shell')
  const frame = shell?.querySelector('iframe')
  return {
    hasShell: !!shell,
    frameSrc: frame?.getAttribute('src') || '',
    frameCount: shell ? shell.querySelectorAll('iframe').length : 0,
    // 关闭键必须悬浮可点（去掉顶部栏后这是唯一的鼠标退出方式）
    hasClose: !!shell?.querySelector('.preview-close'),
    // 旧顶部栏的痕迹：返回键与下载按钮
    legacyBar: !!document.querySelector('.preview-bar'),
    shellRect: shell
      ? { w: Math.round(shell.getBoundingClientRect().width), h: Math.round(shell.getBoundingClientRect().height) }
      : null,
    modalMasks: document.querySelectorAll('.n-modal-mask').length
  }
}

const browser = await chromium.launch()
try {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
  const page = await ctx.newPage()
  const errs = []
  page.on('pageerror', (e) => errs.push(e.message))

  // 记录是否发生了「新开标签/窗口」
  let popups = 0
  page.on('popup', () => popups++)

  await page.addInitScript(() => localStorage.setItem('lanfs-token', 'preview-check'))
  await page.goto(`${BASE}/files/mine`, { waitUntil: 'networkidle', timeout: 30000 })
  await page.waitForTimeout(900)

  // --- 1) 点文件行的「预览」：应弹层而非新标签 ---
  const previewBtn = page.locator('.desktop-only tbody tr', { hasText: '预览' }).first().getByText('预览', { exact: true })
  check((await previewBtn.count()) > 0, '文件行里没有找到「预览」按钮')
  await previewBtn.first().click()
  await page.waitForTimeout(1200)

  check(popups === 0, `点预览不应新开标签页，实际新开了 ${popups} 个`)

  const snap = await page.evaluate(SNAPSHOT)
  check(snap.hasShell, '点预览后没有出现预览弹层（.preview-shell）')
  if (!snap.hasShell) throw new Error('无弹层，后续断言无意义')

  // --- 2) 弹层内部必须是 iframe，且指向预览路由 ---
  check(snap.frameCount === 1, `弹层内应有且仅有一个 iframe，实际 ${snap.frameCount} 个`)
  check(
    /\/preview\/\d+/.test(snap.frameSrc),
    `iframe 应指向 /preview/<id>，实际 src="${snap.frameSrc}"`
  )
  check(/embed=1/.test(snap.frameSrc), `iframe 应带 embed=1（预览页据此不再渲染自身 chrome），实际 src="${snap.frameSrc}"`)

  // --- 3) 顶部名称/下载栏必须彻底消失 ---
  check(!snap.legacyBar, '预览弹层里还残留旧的顶部栏（.preview-bar）')
  const shellText = await page.locator('.preview-shell').innerText().catch(() => '')
  check(!/下载/.test(shellText), `弹层内不应出现「下载」按钮，实际文案「${shellText.replace(/\n/g, ' ')}」`)

  // --- 4) iframe 里真的渲染出了文件内容（不是空白/报错 iframe） ---
  const inner = page.frameLocator('.preview-shell iframe')
  const innerBar = await inner.locator('.preview-bar').count().catch(() => -1)
  check(innerBar === 0, `iframe 内还残留顶部栏 .preview-bar（${innerBar} 个）`)
  // 嵌入态不该有返回键（外层已有悬浮关闭键）
  const innerBack = await inner.locator('.preview-back').count().catch(() => -1)
  check(innerBack === 0, `嵌入态不应渲染返回键 .preview-back（${innerBack} 个）`)

  // --- 4b) 内容是真的渲染出来了 ---
  //     只断言「body 里有字」是不够的：预览失败时弹的是一块错误提示，
  //     照样有字。这里挑一个**必定能渲染**的文本文件（.csv，mock 会回
  //     文本内容），断言它的正文出现在 iframe 里，才算真的打通了链路。
  await page.locator('.preview-close').click()
  await page.waitForTimeout(600)
  await page.goto(`${BASE}/files`, { waitUntil: 'networkidle', timeout: 30000 })
  await page.waitForTimeout(900)
  await page
    .locator('.desktop-only tbody tr', { hasText: '设备台账导出.csv' })
    .getByText('预览', { exact: true })
    .click()
  await page.waitForTimeout(1600)
  const csvInner = (await page.frameLocator('.preview-shell iframe').locator('body').innerText().catch(() => '')) || ''
  check(
    csvInner.includes('MOCK 预览内容第一行'),
    `iframe 里没有渲染出文件正文，实际内容「${csvInner.replace(/\n/g, ' ').slice(0, 120)}」`
  )

  // --- 4c) 悬浮关闭键不能盖住预览内容自己的右上角控件 ---
  //     文本预览的「复制全部」正好在右上角。第一版没有给这个位置留安全区，
  //     两个按钮贴着（仅差 8px），窄屏或文案变长就会真的叠上、点不动。
  //     这里直接量两者的外接矩形：必须不重叠，且点击后弹层不能被关掉。
  const closeBox = await page.locator('.preview-close').boundingBox()
  const copyBox = await page
    .frameLocator('.preview-shell iframe')
    .locator('.text-wrap button')
    .boundingBox()
  check(!!closeBox && !!copyBox, '没能量到关闭键 / 「复制全部」的位置')
  if (closeBox && copyBox) {
    const overlap = !(
      copyBox.x + copyBox.width < closeBox.x ||
      copyBox.x > closeBox.x + closeBox.width ||
      copyBox.y + copyBox.height < closeBox.y ||
      copyBox.y > closeBox.y + closeBox.height
    )
    check(!overlap, '悬浮关闭键与预览内容的右上角控件重叠（该控件会点不动）')
    // 实测点击：若被关闭键吃掉，弹层会被关掉
    await page.mouse.click(copyBox.x + copyBox.width / 2, copyBox.y + copyBox.height / 2)
    await page.waitForTimeout(500)
    check(
      await page.evaluate(() => !!document.querySelector('.preview-shell')),
      '点击预览内容的「复制全部」把弹层关掉了（说明关闭键盖住了它）'
    )
  }

  // --- 5) 关闭：点悬浮关闭键后弹层消失 ---
  check(snap.hasClose, '弹层里没有悬浮关闭键')
  await page.locator('.preview-close').click()
  await page.waitForTimeout(700)
  const closed = await page.evaluate(() => ({
    shell: document.querySelectorAll('.preview-shell').length,
    frame: document.querySelectorAll('.preview-shell iframe').length
  }))
  check(closed.shell === 0, `关闭后弹层应消失，实际还有 ${closed.shell} 个`)
  check(closed.frame === 0, `关闭后 iframe 应卸载，实际还有 ${closed.frame} 个`)

  // --- 6) 深色档：预览面不能是写死的白底 ---
  await page.evaluate(() => localStorage.setItem('lanfs-theme-mode', 'dark'))
  await page.reload({ waitUntil: 'networkidle' })
  await page.waitForTimeout(900)
  await page.locator('.desktop-only tbody tr', { hasText: '预览' }).first().getByText('预览', { exact: true }).click()
  await page.waitForTimeout(1200)

  const dark = await page.evaluate(() => {
    const shell = document.querySelector('.preview-shell')
    const f = shell?.querySelector('iframe')
    const d = f?.contentDocument
    const page_ = d?.querySelector('.preview-page')
    const cs = page_ ? getComputedStyle(page_) : null
    return {
      outerDark: document.documentElement.dataset.theme,
      innerDark: d?.documentElement?.dataset.theme,
      innerBg: cs?.backgroundColor
    }
  })
  check(dark.outerDark === 'dark', `外层应处于深色档，实际 ${dark.outerDark}`)
  check(dark.innerDark === 'dark', `iframe 内也应处于深色档，实际 ${dark.innerDark}`)
  // 深色档下预览面的底色不能是白色/接近白色
  const rgb = (dark.innerBg || '').match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/)
  const bright = rgb ? Number(rgb[1]) + Number(rgb[2]) + Number(rgb[3]) : 0
  check(
    !!rgb && bright < 300,
    `深色档下预览面底色过亮（${dark.innerBg}）—— 说明还在用写死的浅色`
  )

  check(errs.length === 0, `页面有 JS 运行时错误：${errs.slice(0, 2).join(' | ')}`)

  // --- 7) 各类型的观感必须统一 ---
  //     这是用户反馈的问题：同一份文件在不同类型下舞台底色/留白/滚动归属各不相同
  //     （docx 浅灰舞台、pptx 深灰、xlsx·text 全白、pdf 干脆没舞台）。
  //     静态检查（check-ui-consistency.mjs）只守「组件不许自带舞台」这条约定；
  //     「运行时各类型的舞台真的算出同一个值」只有浏览器能量。
  //     mock 对 docx/xlsx/png/pdf 回真实字节，因此这里真的走到了各自的渲染分支。
  const STAGE = () => {
    const stage = document.querySelector(
      '.preview-page .preview-stage, .preview-page .preview-state'
    )
    if (!stage) return null
    const cs = getComputedStyle(stage)
    return {
      cls: stage.className,
      bg: cs.backgroundColor,
      padding: `${cs.paddingTop} ${cs.paddingRight} ${cs.paddingBottom} ${cs.paddingLeft}`,
      // 内容面（舞台的直接子元素）是否撑满舞台 —— 高度链断掉时这里会是 auto/0
      childH: stage.firstElementChild
        ? Math.round(stage.firstElementChild.getBoundingClientRect().height)
        : 0,
      stageH: Math.round(stage.getBoundingClientRect().height)
    }
  }

  // mock 的行 id 与扩展名：1=xlsx 2=docx 3=pdf 4=png 6=csv
  const kinds = [
    ['1', 'xlsx'],
    ['2', 'docx'],
    ['3', 'pdf'],
    ['4', 'png'],
    ['6', 'text']
  ]
  const seen = []
  for (const [id, label] of kinds) {
    await page.goto(`${BASE}/preview/${id}?embed=1`, { waitUntil: 'domcontentloaded', timeout: 30000 })
    // office 类型要等库渲染完（动态 import + 解析）
    await page.waitForTimeout(label === 'xlsx' || label === 'docx' ? 4000 : 1800)
    const st = await page.evaluate(STAGE)
    check(!!st, `${label} 类型没有渲染出统一舞台 .preview-stage`)
    if (st) seen.push({ label, ...st })
  }

  if (seen.length >= 2) {
    const first = seen[0]
    for (const s of seen.slice(1)) {
      check(
        s.bg === first.bg,
        `各类型舞台底色不统一：${first.label}=${first.bg} 但 ${s.label}=${s.bg}`
      )
      check(
        s.padding === first.padding,
        `各类型舞台留白不统一：${first.label}="${first.padding}" 但 ${s.label}="${s.padding}"`
      )
    }
  }
  // 舞台必须真的把内容面撑起来（高度链断了会退化成 0 / 贴顶）
  for (const s of seen) {
    check(
      s.childH > s.stageH * 0.5,
      `${s.label} 的内容面没有被舞台撑开（内容面 ${s.childH}px / 舞台 ${s.stageH}px）——高度链断了`
    )
  }

  // --- 8) 窄屏下 pptx 不得被裁 ---
  //     pptx-preview 的渲染宽度由我们传给它。此前写的是 Math.max(640, …)，
  //     于是 390px 手机上幻灯片被硬渲染成 640px 宽、右侧 250px 直接被裁掉。
  //     宽度必须跟着容器走（只设上限，不设下限）。
  const mobile = await ctx.newPage()
  await mobile.setViewportSize({ width: 390, height: 844 })
  await mobile.goto(`${BASE}/preview/5?embed=1`, { waitUntil: 'domcontentloaded', timeout: 30000 })
  await mobile.waitForTimeout(4500)
  const narrow = await mobile.evaluate(() => {
    const de = document.documentElement
    const stage = document.querySelector('.preview-stage')
    const slide = document.querySelector('.pptx-preview-wrapper')
    return {
      docOverflowX: de.scrollWidth - de.clientWidth,
      stageScrollX: stage ? stage.scrollWidth - stage.clientWidth : null,
      stageW: stage ? stage.clientWidth : null,
      slideW: slide ? Math.round(slide.getBoundingClientRect().width) : null
    }
  })
  check(narrow.docOverflowX <= 2, `390px 下页面横向溢出 ${narrow.docOverflowX}px`)
  check(
    narrow.stageScrollX !== null && narrow.stageScrollX <= 2,
    `390px 下幻灯片被裁：幻灯片 ${narrow.slideW}px 宽，舞台只有 ${narrow.stageW}px（横向溢出 ${narrow.stageScrollX}px）`
  )
  await mobile.close()

  // --- 9) 可预览文件大小限制（默认 20MB）：超限不给在线预览，但仍可下载 ---
  //
  // 为什么要真浏览器：这是"前端拿到 kind=too-large 后有没有走对分支"的渲染结果 ——
  // 单测与类型检查都看不见。超限时**绝不能**再去取文件内容：那道闸要防的
  // 正是"把超大文件读进浏览器"，如果前端照旧拉 blob，限制就形同虚设。
  const limit = await ctx.newPage()
  const limitErrs = []
  limit.on('pageerror', (e) => limitErrs.push(e.message))
  // 记录这个页面发起的 /content 请求：超限文件一个都不该有
  const contentHits = []
  limit.on('request', (r) => {
    if (/\/api\/files\/\d+\/content/.test(r.url())) contentHits.push(r.url())
  })
  await limit.addInitScript(() => localStorage.setItem('lanfs-token', 'preview-check'))
  // mock 里 id=100 是 30MB 的 .mp4（> 默认 20MB 上限）
  await limit.goto(`${BASE}/preview/100?embed=1`, { waitUntil: 'networkidle', timeout: 30000 })
  await limit.waitForTimeout(1200)
  const tooLarge = await limit.evaluate(() => {
    const page_ = document.querySelector('.preview-page')
    const state = page_?.querySelector('.preview-state')
    return {
      text: state ? state.innerText.replace(/\s+/g, ' ').trim() : '',
      hasStage: !!page_?.querySelector('.preview-stage'),
      hasFrame: !!page_?.querySelector('iframe')
    }
  })
  check(
    /上限/.test(tooLarge.text) && /下载/.test(tooLarge.text),
    `超限文件应给出「超过上限、请下载」的说明，实际文案「${tooLarge.text}」`
  )
  check(!tooLarge.hasStage, '超限文件不应渲染预览舞台（说明前端仍走了渲染分支）')
  check(!tooLarge.hasFrame, '超限文件不应渲染 iframe（说明前端仍走了内联分支）')
  check(
    contentHits.length === 0,
    `超限文件不应请求 /content（限制要防的正是把大文件读进浏览器），实际请求了 ${JSON.stringify(contentHits)}`
  )
  check(limitErrs.length === 0, `超限预览页有 JS 运行时错误：${limitErrs.slice(0, 2).join(' | ')}`)
  await limit.close()

  await ctx.close()
} finally {
  await browser.close()
  stopAll()
}

if (failures) {
  console.error(`\n❌ 预览弹层检查未通过（${failures} 项）：`)
  for (const p of problems) console.error(`   - ${p}`)
  process.exit(1)
}
console.log('✅ 预览弹层检查通过：页内弹层 + iframe 内嵌、无顶部栏、深色适配、可关闭')
