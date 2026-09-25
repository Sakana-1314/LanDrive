// 上传队列的「汇总口径」：把一堆任务折算成浮窗标题上的一行字。
//
// 为什么单独抽成纯函数：右下角浮窗是常驻组件，它的取值口径（总进度、
// 汇总速度、剩余时间、标题文案）最容易在改动中悄悄跑偏 —— 比如进度条
// 按"任务数"算、速度只取最后一个任务、失败时还显示"上传中"。
// 这些都无法靠类型检查发现，这里做成纯函数并用单测钉住。
import type { UploadState, UploadTask } from './upload'

/** 折算汇总时用得到的最小字段集（UploadTask 天然满足）。 */
export type SummaryTask = Pick<
  UploadTask,
  'state' | 'loaded' | 'total' | 'speed' | 'eta' | 'percent'
>

export interface UploadSummary {
  /** 已完成（含取消）的任务数 */
  finished: number
  /** 正在跑（hashing/uploading/merging）的任务数 */
  active: number
  /** 失败待处理的任务数 */
  failed: number
  /** 队列里的任务总数 */
  total: number
  /** 总体进度 0-100（按字节折算，全部完成即 100） */
  percent: number
  /** 全部正在跑的任务的速度之和 */
  speed: number
  /** 按汇总速度估算的剩余秒数（无速度时为 0） */
  eta: number
}

/** 正在跑的状态：这些任务在占用带宽，进度与速度都要算进去。 */
function isActive(t: SummaryTask): boolean {
  return t.state === 'uploading' || t.state === 'hashing' || t.state === 'merging'
}

/** 折算整个队列的进度。按字节而不是按任务数：后者会在传大文件时虚高。 */
export function summarizeUploads(tasks: SummaryTask[]): UploadSummary {
  let loaded = 0
  let total = 0
  let speed = 0
  let active = 0
  let finished = 0
  let failed = 0

  for (const t of tasks) {
    if (t.state === 'done' || t.state === 'canceled') {
      finished++
      // 完成的任务按它的全部字节计入分子，否则总体进度永远差一口气。
      loaded += t.total
      total += t.total
      continue
    }
    if (t.state === 'error') {
      failed++
      total += t.total
      continue
    }
    if (isActive(t)) {
      active++
      speed += t.speed || 0
    }
    loaded += t.loaded
    total += t.total
  }

  const percent = total > 0 ? Math.min(100, Math.floor((loaded / total) * 100)) : 0
  const remain = Math.max(0, total - loaded)
  return {
    finished,
    active,
    failed,
    total: tasks.length,
    percent,
    speed,
    eta: speed > 0 ? remain / speed : 0
  }
}

/**
 * 浮窗标题：一眼看出"现在是什么状态、还有几个"。
 *
 * 顺序是有意的 —— 失败最需要立刻处理，其次才是进行中/已完成，
 * 否则传了 20 个只错 1 个时标题显示"已完成"，用户不会去展开看。
 */
export function panelTitle(s: UploadSummary): string {
  if (s.failed > 0) return `${s.failed} 个文件上传失败`
  if (s.active > 0) return `正在上传 ${s.active} 个文件`
  if (s.total === 0) return '上传队列'
  return `已完成 ${s.finished} 个上传`
}

/** 任务状态的中文文案（浮窗列表与页面队列共用同一套口径）。 */
export function uploadStateLabel(state: UploadState, resumed = false): string {
  switch (state) {
    case 'pending':
      return '等待中'
    case 'hashing':
      return '校验中'
    case 'uploading':
      return resumed ? '续传中' : '上传中'
    case 'merging':
      return '合并中'
    case 'done':
      return '已完成'
    case 'error':
      return '失败'
    case 'canceled':
      return '已取消'
  }
}

/**
 * 浮窗右上角那个圆环/角标的颜色语义。
 * 与 Naive UI 的 progress status 取值保持一致，避免两处各写一套判断。
 */
export function summaryStatus(s: UploadSummary): 'default' | 'success' | 'error' {
  if (s.failed > 0) return 'error'
  if (s.active === 0 && s.total > 0) return 'success'
  return 'default'
}
