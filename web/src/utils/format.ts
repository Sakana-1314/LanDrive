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

/**
 * 文件类型的中文名，用在类型角标上。
 * 用中文名（Word / Excel / 图片）而不是裸扩展名，界面上一眼能看懂，
 * 也就不需要在表格上方再写一行说明文字。
 */
const EXT_LABELS: Record<string, string> = {
  '.doc': 'Word',
  '.docx': 'Word',
  '.xls': 'Excel',
  '.xlsx': 'Excel',
  '.csv': 'Excel',
  '.ppt': 'PPT',
  '.pptx': 'PPT',
  '.pdf': 'PDF',
  '.txt': '文本',
  '.md': '文本',
  '.log': '文本',
  '.json': '文本',
  '.xml': '文本',
  '.yml': '文本',
  '.yaml': '文本',
  '.ini': '文本',
  '.png': '图片',
  '.jpg': '图片',
  '.jpeg': '图片',
  '.gif': '图片',
  '.webp': '图片',
  '.bmp': '图片',
  '.ico': '图片',
  '.tiff': '图片',
  '.mp4': '视频',
  '.webm': '视频',
  '.mov': '视频',
  '.ogg': '音视频',
  '.mp3': '音频',
  '.wav': '音频',
  '.m4a': '音频',
  '.flac': '音频',
  '.aac': '音频',
  '.zip': '压缩包',
  '.rar': '压缩包',
  '.7z': '压缩包',
  '.tar': '压缩包',
  '.gz': '压缩包'
}

/** 旧版 Office 二进制格式：浏览器无法在线预览，只能下载。 */
const LEGACY_OFFICE = ['.doc', '.xls', '.ppt']

/** 取扩展名的展示形式（已知类型给中文名，未知类型回退到大写扩展名）。 */
export function extLabel(ext: string): string {
  const e = (ext || '').toLowerCase()
  if (!e) return '文件'
  return EXT_LABELS[e] || e.replace(/^\./, '').toUpperCase().slice(0, 6)
}

/** 该文件能否在线预览（旧版 Office 格式只能下载后查看）。 */
export function canPreview(ext: string): boolean {
  return !LEGACY_OFFICE.includes((ext || '').toLowerCase())
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
