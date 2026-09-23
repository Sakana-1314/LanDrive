// 检查「模板里用了 <n-xxx>，但脚本里没导入 NXxx」。
//
// 为什么需要这个检查：本项目按需导入 Naive UI 组件（没有全局注册），
// 漏掉导入时 Vue 会把标签当成**未解析的自定义元素**：
//   - 不报错、不崩溃，控制台也未必有提示；
//   - 元素照常出现在 DOM 里但没有任何行为。
//
// 真实事故：LoginView 的 <n-form> 从未导入 NForm，于是 `attr-type="submit"`
// 的登录按钮点下去什么都不发生（一直只能用回车登录），
// 从初始版本起就存在；UsersView 的 <n-empty>/<n-pagination> 同样漏了。
// 这类问题类型检查与单测都发现不了，只能靠静态比对。
import fs from 'node:fs'
import path from 'node:path'

const SRC = 'src'

/** n-data-table → NDataTable（Naive UI 的 PascalCase 命名规则）。 */
function toPascal(tag) {
  return 'N' + tag.slice(2).split('-').map((p) => p.charAt(0).toUpperCase() + p.slice(1)).join('')
}

function walk(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = path.join(dir, e.name)
    return e.isDirectory() ? walk(p) : p.endsWith('.vue') ? [p] : []
  })
}

const problems = []
for (const file of walk(SRC)) {
  const text = fs.readFileSync(file, 'utf8')
  const used = new Set([...text.matchAll(/<(n-[a-z0-9-]+)/g)].map((m) => m[1]))
  const imported = new Set()
  for (const m of text.matchAll(/import\s*\{([^}]*)\}\s*from\s*['"]naive-ui['"]/gs)) {
    for (const part of m[1].split(',')) {
      const name = part.trim().split(/\s+as\s+/)[0].trim()
      if (name.startsWith('N')) imported.add(name)
    }
  }
  for (const tag of used) {
    const comp = toPascal(tag)
    if (!imported.has(comp)) problems.push(`${file}: 用到 <${tag}>，但未导入 ${comp}`)
  }
}

if (problems.length) {
  console.error(`❌ 发现 ${problems.length} 处 Naive UI 组件未导入（点了没反应、且不报错）：`)
  for (const p of problems) console.error('   - ' + p)
  process.exit(1)
}
console.log('✅ Naive UI 组件导入检查通过：模板里用到的组件都已导入')
