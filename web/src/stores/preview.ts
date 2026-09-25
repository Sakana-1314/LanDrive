// 预览弹层的全局状态（reactive 单例，不引入 Pinia —— 见 AGENTS.md）。
//
// 为什么做成全局单例：预览入口散落在文件表格、移动端卡片、上传队列等多处，
// 如果各页面自己持有一份弹层状态，就等于把「预览」这件事复制了多份，
// 任意一处忘记挂弹层就点不开。状态集中在这里后，弹层只挂一次（MainLayout），
// 任何地方调 openPreview(id) 都能打开。
import { reactive } from 'vue'

/** 当前预览的文件 id；0 表示没有打开任何预览。 */
export const previewState = reactive<{ id: number }>({ id: 0 })

/** 打开某个文件的预览弹层。 */
export function openPreview(id: number): void {
  if (!Number.isFinite(id) || id <= 0) return
  previewState.id = id
}

/** 关闭预览弹层。 */
export function closePreview(): void {
  previewState.id = 0
}
