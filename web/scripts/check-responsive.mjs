// 响应式布局回归测试：用真实浏览器在多种视口下检查横向溢出。
//
// 为什么需要：布局问题在代码审查和单元测试里都看不出来，只有真正渲染才暴露。
// 已经踩过的坑（本测试即用于防止复发）：
//   - 缺全局 box-sizing: border-box → 自定义元素 width:100% + padding 超出父容器；
//   - grid 子项默认 min-width:auto → nowrap 内容（长文件名）把网格列撑宽，
//     内容被 n-scrollbar 裁掉，肉眼看到卡片被切一半；
//   - 页面根容器用 display:grid 而未写 grid-template-columns: minmax(0,1fr)，
//     隐式列按内容撑宽。
//
// 用法：
//   node scripts/check-responsive.mjs            # 自动起 mock 后端 + dev server
//   BASE=http://127.0.0.1:5173 node scripts/...  # 用已有的服务（需自行保证 /api/health 可达）
//
// ⚠️ 必须有可用的后端：内页都受路由守卫保护，接口不通时会被重定向到登录页，
// 于是「内页布局」实际没被检查到（只测了登录页）。所以这里强制要求
// /api/health 可达；接口不可用时直接失败，而不是给出一个假的绿色结果。
//
// 说明：需要 playwright（未写入 package.json —— 体积大且只是校验工具）。
// 未安装时默认跳过；但设置 REQUIRE_PLAYWRIGHT=1 时**必须**失败，
// 否则 CI 会在"什么都没检查"的情况下报绿 —— 这正是本脚本踩过的第三个假通过。
let chromium
try {
  ;({ chromium } = await import('playwright'))
} catch {
  try {
    // 退回全局安装位置（全局 node_modules 路径随环境而异，动态查询）
    const { execSync } = await import('node:child_process')
    const { createRequire } = await import('node:module')
    const globalRoot = execSync('npm root -g', { encoding: 'utf8' }).trim()
    const require = createRequire(import.meta.url)
    ;({ chromium } = require(`${globalRoot}/playwright`))
  } catch {
    if (process.env.REQUIRE_PLAYWRIGHT === '1') {
      console.error('❌ 未安装 playwright，但 REQUIRE_PLAYWRIGHT=1 要求必须执行检查')
      console.error('   安装：npm i -g playwright && npx playwright install chromium')
      process.exit(1)
    }
    console.log('⚠️  未安装 playwright，跳过响应式检查')
    console.log('   安装：npm i -g playwright && npx playwright install chromium')
    process.exit(0)
  }
}

const PAGES = [
  '/login',
  '/files',
  '/files/mine',
  // 子 tab 形态（菜单里选中某个用户的目录）
  '/files?owner=2',
  '/profile',
  '/admin/workbench',
  '/admin/users',
  '/admin/files',
  '/admin/settings'
]
const VIEWPORTS = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'laptop', width: 1100, height: 800 },
  { name: 'tablet', width: 900, height: 1000 },
  { name: 'mobile', width: 390, height: 844 },
  { name: 'mobile-small', width: 320, height: 640 }
]

let failures = 0
const problems = []

/** 没给 BASE 时自己起 mock 后端 + dev server，让脚本一条命令可跑。 */
const { spawn } = await import('node:child_process')
const procs = []
function start(cmd, args, env = {}) {
  const p = spawn(cmd, args, { stdio: 'ignore', env: { ...process.env, ...env } })
  procs.push(p)
  return p
}
function stopAll() {
  for (const p of procs) {
    try {
      p.kill('SIGTERM')
    } catch {
      /* 已退出 */
    }
  }
}

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
  const MOCK_PORT = 18081
  const DEV_PORT = 4174
  target = `http://127.0.0.1:${DEV_PORT}`
  console.log('未指定 BASE，自动启动 mock 后端与 dev server…')
  start(process.execPath, ['scripts/mock-api.mjs', String(MOCK_PORT)])
  if (!(await waitFor(`http://127.0.0.1:${MOCK_PORT}/api/health`, 15000))) {
    console.error('❌ mock 后端启动失败')
    stopAll()
    process.exit(1)
  }
  start('npx', ['vite', '--port', String(DEV_PORT), '--host', '127.0.0.1'], {
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

// 预检：目标必须真的能打开，否则所有页面都是空白页，
// 「无横向溢出」会**假通过**（这个坑已经踩过一次）。
try {
  const res = await fetch(BASE, { signal: AbortSignal.timeout(5000) })
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
} catch (e) {
  console.error(`❌ 无法访问 ${BASE}（${e.message}）`)
  console.error('   请先启动前端：npm run dev，或直接不带 BASE 运行本脚本')
  process.exit(1)
}

// 后端可用性预检：拿到 200 才说明内页能正常渲染。
try {
  const res = await fetch(new URL('/api/health', BASE), { signal: AbortSignal.timeout(5000) })
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
} catch (e) {
  console.error(`❌ ${BASE}/api/health 不可达（${e.message}）`)
  console.error('   内页会被路由守卫重定向到登录页，检查将失去意义。')
  console.error('   请连上可用的后端（本地可用 mock 或 docker compose）。')
  process.exit(1)
}

const browser = await chromium.launch()
for (const vp of VIEWPORTS) {
  const ctx = await browser.newContext({ viewport: { width: vp.width, height: vp.height } })
  const page = await ctx.newPage()
  // 写入令牌：否则路由守卫会把内页全部跳到登录页，测不到真实布局。
  await page.addInitScript(() => localStorage.setItem('lanfs-token', 'responsive-check'))

  for (const path of PAGES) {
    await page.goto(BASE + path, { waitUntil: 'networkidle', timeout: 20000 }).catch(() => {})
    await page.waitForTimeout(400)

    // 页面必须真的渲染出内容：空白页的横向溢出恒为 0，会让检查失去意义。
    const rendered = await page.evaluate(() => {
      const app = document.querySelector('#app')
      return !!app && app.children.length > 0 && document.body.innerText.trim().length > 0
    })
    if (!rendered) {
      failures++
      problems.push(`${vp.name} ${path}: 页面没有渲染内容（检查目标可能未启动或路由被拦截）`)
      continue
    }
    // 内页被重定向到登录页 = 没测到目标布局，属于失败而不是通过。
    if (path !== '/login') {
      const landed = await page.evaluate(() => location.pathname)
      if (landed.startsWith('/login') || landed.startsWith('/network-blocked')) {
        failures++
        problems.push(`${vp.name} ${path}: 被重定向到 ${landed}，未检查到目标页面`)
        continue
      }
    }

    // 页面自身不允许横向滚动
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth
    )
    if (overflow > 2) {
      failures++
      problems.push(`${vp.name} ${path}: 页面横向溢出 ${overflow}px`)
    }

    // 逐个元素比对右边界：n-scrollbar / data-table 这类自带横向滚动的容器内部
    // 允许比视口宽，因此只看"不在滚动容器内"的元素。
    const bleeding = await page.evaluate(() => {
      const out = []
      for (const el of document.querySelectorAll('body *')) {
        const r = el.getBoundingClientRect()
        if (r.width === 0 || r.right <= window.innerWidth + 2) continue
        // 跳过自带横向滚动的祖先
        let p = el.parentElement
        let scrollable = false
        while (p) {
          const cs = getComputedStyle(p)
          if (cs.overflowX === 'auto' || cs.overflowX === 'scroll') {
            scrollable = true
            break
          }
          p = p.parentElement
        }
        if (scrollable) continue
        out.push(`${el.tagName.toLowerCase()}.${String(el.className || '').split(' ')[0]}`)
      }
      return [...new Set(out)].slice(0, 4)
    })
    if (bleeding.length) {
      failures++
      problems.push(`${vp.name} ${path}: 元素超出视口 → ${bleeding.join(', ')}`)
    }
  }
  await ctx.close()
  console.log(`  ✅ ${vp.name} (${vp.width}px) 检查完毕`)
}
await browser.close()

console.log()
if (failures) {
  console.error(`❌ 响应式检查发现 ${failures} 处问题：`)
  for (const p of problems) console.error('   - ' + p)
  stopAll()
  process.exit(1)
}
console.log('✅ 响应式检查通过：所有视口下均无横向溢出')
stopAll()
