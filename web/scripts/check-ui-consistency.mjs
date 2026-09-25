// 检查页面头部结构是否统一（静态检查，不需要浏览器）。
//
// 为什么需要：头部控件的高度与左右分工靠**约定**维持，写错了类型检查和
// 单测都看不出来，只有肉眼翻页才发现。已经出现过：
//   - 同一类分段切换在「分享管理」靠左、在「文件与回收站」靠右，翻页找不到控件；
//   - 头部混用 size="small"（28px）与默认（34px），一行里高度参差不齐；
//   - 用户管理卡片自己写 border-radius: 8px，与其它卡片的 14px 不一致。
//
// 约定（见 styles.css 的 .section-head 注释）：
//   1. 页面头部用 .section-head；左区 .section-head__main 放标题/筛选，
//      右区 .section-head__actions 只放按钮；
//   2. 头部内的**交互控件**用默认尺寸（34px），不要传 size="small"；
//      n-tag 这类纯标签不受此限（22px 是有意为之）；
//   3. 卡片不要内联 border-radius，统一走 --radius-card。
import fs from 'node:fs'
import path from 'node:path'

const SRC = 'src'
const problems = []

/** 受尺寸约定约束的控件（标签/图标不算）。 */
const CONTROLS = ['n-button', 'n-input', 'n-select', 'n-input-number', 'n-radio-group', 'n-date-picker']

function walk(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = path.join(dir, e.name)
    return e.isDirectory() ? walk(p) : p.endsWith('.vue') ? [p] : []
  })
}

/** 取出 <template> 部分：样式/脚本里的同名类名不算数。 */
function templateOf(text) {
  const m = text.match(/<template>([\s\S]*)<\/template>/)
  return m ? m[1] : ''
}

/** 切出每个 .section-head 块：从起始标签按 div 配对找到块尾。 */
function headBlocks(tpl) {
  const out = []
  const re = /<div[^>]*class="[^"]*\bsection-head\b(?![_-])[^"]*"[^>]*>/g
  let m
  while ((m = re.exec(tpl))) {
    let depth = 0
    let end = tpl.length
    const tagRe = /<div\b|<\/div>/g
    tagRe.lastIndex = m.index
    let t
    while ((t = tagRe.exec(tpl))) {
      depth += t[0] === '</div>' ? -1 : 1
      if (depth === 0) { end = t.index + t[0].length; break }
    }
    out.push(tpl.slice(m.index, end))
  }
  return out
}

/** 找到一个控件的开标签，判断它是否带 size="small"。 */
function smallControls(block) {
  const found = []
  for (const c of CONTROLS) {
    const re = new RegExp(`<${c}\\b[^>]*>`, 'g')
    let m
    while ((m = re.exec(block))) {
      if (/size="small"/.test(m[0])) found.push(m[0].slice(0, 60))
    }
  }
  return found
}

for (const file of walk(SRC)) {
  const text = fs.readFileSync(file, 'utf8')
  const tpl = templateOf(text)

  for (const block of headBlocks(tpl)) {
    // 1) 头部内的交互控件不要用 small（会与 34px 的同行控件高度不齐）
    for (const tag of smallControls(block)) {
      problems.push(`${file}: 头部控件用了 size="small"（应为默认 34px）→ ${tag}`)
    }

    // 2) 结构完整性：有右区就必须有左区
    const hasActions = /section-head__actions/.test(block)
    const hasMain = /section-head__main/.test(block)
    if (hasActions && !hasMain) {
      problems.push(`${file}: 头部有 .section-head__actions 却没有 .section-head__main（左筛选/右按钮结构不完整）`)
    }

    // 3) 筛选器必须落在左区内：.filters/.user-toolbar 出现在 .section-head__main 之前即为旧结构
    const filtersAt = block.search(/class="(filters|user-toolbar)\b/)
    if (filtersAt >= 0) {
      const mainAt = block.indexOf('section-head__main')
      if (mainAt < 0 || filtersAt < mainAt) {
        problems.push(`${file}: .filters/.user-toolbar 不在 .section-head__main 内（筛选应靠左）`)
      }
    }
  }

  // 4) 卡片圆角不要内联写死（统一 --radius-card）
  for (const tag of tpl.match(/<n-card[^>]*>/g) || []) {
    if (/border-radius\s*:/.test(tag)) {
      problems.push(`${file}: n-card 内联 border-radius（应统一用 .card-surface / --radius-card）`)
    }
  }

  // 5) 空状态只能由 FileTable 负责：页面级再写一条会与表格内的空提示同时出现。
  //    真实事故：FilesView 在 FileTable 之上又加了一条「这里还没有文件」，
  //    与表格里的「暂无文件」同屏渲染，页面上出现两遍空提示。
  if (/FileTable/.test(tpl) && /<n-empty/.test(tpl)) {
    problems.push(`${file}: 用了 FileTable 又自己写 <n-empty>（空状态重复渲染，页面会出现两遍提示）；请交给 FileTable 的 #empty / 移动端空态`)
  }
}

// 6) 预览的类型观感必须统一（见 docs/design.md「预览观感统一」）。
//
//    为什么需要静态检查：观感不统一是**逐类型累积**出来的 —— 每个预览器当初
//    各自决定了舞台底色、内边距、谁来滚动，单看任何一个都合理，摆在一起才割裂
//    （docx 浅灰舞台 / pptx 深灰 / xlsx·text 全白 / pdf 没舞台）。这种漂移
//    类型检查与单测都发现不了，只有把「不许各自为政」写成规则才守得住。
const previewDir = path.join(SRC, 'components', 'preview')
/** 各预览组件：只许画内容面，不许自带舞台底色 / 魔数高度。 */
const PREVIEW_FILES = fs
  .readdirSync(previewDir)
  .filter((f) => f.endsWith('.vue'))
  .map((f) => path.join(previewDir, f))

/** 去掉 CSS 注释：否则「不再用 calc(100dvh - 魔数)」这类说明文字会被误判为违规。 */
function stripCssComments(css) {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

for (const file of PREVIEW_FILES) {
  const text = fs.readFileSync(file, 'utf8')
  const style = stripCssComments((text.match(/<style[^>]*>([\s\S]*?)<\/style>/) || [, ''])[1])

  // 6a) 不许自带舞台底色：舞台色只能由 PreviewView 的 .preview-stage 提供。
  //     允许 paper / surface / code-bg 这些「内容面」颜色。
  if (/background:\s*var\(--color-preview-stage\)/.test(style)) {
    problems.push(`${file}: 预览组件自带 --color-preview-stage 舞台底色（舞台只能由 PreviewView 的 .preview-stage 提供，否则各类型底色会走样）`)
  }

  // 6b) 不许再用 calc(100dvh - 魔数) 限制高度：滚动归舞台/内容面自己撑满。
  if (/calc\(\s*100dvh\s*-/.test(style)) {
    problems.push(`${file}: 预览组件用了 calc(100dvh - 魔数) 限制高度（高度应交给统一骨架，避免各类型留白不一致）`)
  }
}

// 6c) 舞台骨架必须存在且用统一令牌。
const viewFile = path.join(SRC, 'views', 'PreviewView.vue')
const viewText = fs.readFileSync(viewFile, 'utf8')
if (!/\.preview-stage\b/.test(viewText)) {
  problems.push(`${viewFile}: 缺少统一的 .preview-stage 骨架（所有预览类型必须共用同一个舞台）`)
}
if (!/padding:\s*var\(--preview-gutter\)\s+var\(--preview-pad\)/.test(viewText)) {
  problems.push(`${viewFile}: .preview-stage 未使用统一的 --preview-gutter / --preview-pad 内边距`)
}

if (problems.length) {
  console.error(`❌ 发现 ${problems.length} 处头部/卡片/空状态/预览一致性不一致：`)
  for (const p of problems) console.error('   - ' + p)
  process.exit(1)
}
console.log('✅ 头部结构检查通过：左筛选/右按钮、控件统一 34px、卡片圆角统一、空状态唯一、预览舞台统一')
