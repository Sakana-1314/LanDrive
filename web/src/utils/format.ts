// 通用格式化工具。

/** 字节数转可读字符串。 */
export function formatBytes(n: number): string {
  if (!n || n <= 0) return '0 B'
  const unit = 1024
  if (n < unit) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB', 'PB']
  let v = n / unit
  let i = 0
  while (v >= unit && i < units.length - 1) {
    v /= unit
    i++
  }
  return `${v.toFixed(v >= 100 ? 0 : 1)} ${units[i]}`
}

/** ISO 时间转本地时间字符串。 */
export function formatTime(iso: string | null | undefined, withSeconds = false): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  const pad = (n: number) => String(n).padStart(2, '0')
  const base = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
  return withSeconds ? `${base}:${pad(d.getSeconds())}` : base
}

/** 剩余天数转友好文案。 */
export function formatDaysLeft(days: number): string {
  if (days > 1) return `${days} 天后到期`
  if (days === 1) return '明天到期'
  if (days === 0) return '今天到期'
  return '已到期'
}

/** 取扩展名的展示形式。 */
export function extLabel(ext: string): string {
  if (!ext) return '无后缀'
  return ext.replace(/^\./, '').toUpperCase()
}

/** 截断过长文件名用于展示（保留扩展名）。 */
export function shorten(name: string, max = 42): string {
  if (name.length <= max) return name
  const dot = name.lastIndexOf('.')
  if (dot <= 0 || dot < max - 8) return `${name.slice(0, max - 1)}…`
  const ext = name.slice(dot)
  const keep = max - ext.length - 1
  if (keep <= 1) return `${name.slice(0, max - 1)}…`
  return `${name.slice(0, keep)}…${ext}`
}

/** 把秒数转「x 分 y 秒」。 */
export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '—'
  const s = Math.floor(seconds % 60)
  const m = Math.floor((seconds / 60) % 60)
  const h = Math.floor(seconds / 3600)
  if (h > 0) return `${h} 小时 ${m} 分`
  if (m > 0) return `${m} 分 ${s} 秒`
  return `${s} 秒`
}

/** 上传速度（字节/秒）。 */
export function formatSpeed(bytesPerSecond: number): string {
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) return '—'
  return `${formatBytes(bytesPerSecond)}/s`
}
