// 主题覆盖的单元测试（Node 环境直接运行，零依赖）。
//
// 运行：node --experimental-strip-types src/utils/theme.spec.ts
//
// 为什么需要：明暗两套覆盖由同一个 createThemeOverrides(palette) 生成，
// 但**调色板本身是两份手写对象**。少写一个字段不会报错，只会让那一档
// 悄悄掉回 Naive UI 的内置值 —— 深色档的用户下拉菜单曾因此显示成
// Naive 默认的灰 rgb(72,72,78)，和本项目深色面板明显不是一套。
import assert from 'node:assert/strict'
import { darkThemeOverrides, lightThemeOverrides } from './theme.ts'

const light = lightThemeOverrides as unknown as Record<string, unknown>
const dark = darkThemeOverrides as unknown as Record<string, unknown>

// 1) 两套覆盖的结构必须完全一致：少一个 key 就会在那一档掉回内置值。
function paths(obj: unknown, prefix = ''): string[] {
  if (obj === null || typeof obj !== 'object') return [prefix]
  return Object.entries(obj as Record<string, unknown>).flatMap(([k, v]) =>
    paths(v, prefix ? `${prefix}.${k}` : k)
  )
}
assert.deepEqual(
  paths(dark).sort(),
  paths(light).sort(),
  '明暗两套主题覆盖的键必须完全一致（否则某一档会掉回 Naive 内置值）'
)

// 2) 关键回归：浮层底色必须显式给出。
//    Dropdown/Select 弹层用的是 common.popoverColor，不是 cardColor；
//    不覆盖它时深色档会显示 Naive 的 rgb(72,72,78)。
const lightCommon = light.common as Record<string, string>
const darkCommon = dark.common as Record<string, string>
for (const [name, common] of [['light', lightCommon], ['dark', darkCommon]] as const) {
  assert.ok(common.popoverColor, `${name} 档缺少 common.popoverColor（弹层会掉回 Naive 内置灰）`)
}
assert.notEqual(
  darkCommon.popoverColor,
  'rgb(72, 72, 78)',
  '深色档弹层底色不能是 Naive 内置灰，必须跟随本项目调色板'
)

// 3) 明暗两档的浮层底色必须不同：否则深色档会顶着一块浅色弹层（或反之）。
assert.notEqual(
  lightCommon.popoverColor,
  darkCommon.popoverColor,
  '明暗两档的弹层底色必须不同'
)

// 4) 语义色两套都要给：只改一套是维护约定里明确禁止的。
for (const key of ['bodyColor', 'cardColor', 'textColor1', 'textColor2', 'borderColor', 'dividerColor', 'primaryColor']) {
  assert.ok(lightCommon[key], `light 档缺少 common.${key}`)
  assert.ok(darkCommon[key], `dark 档缺少 common.${key}`)
}

console.log('✅ 主题覆盖测试全部通过（明暗结构一致、弹层底色已显式覆盖）')
