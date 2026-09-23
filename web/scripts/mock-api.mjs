// 响应式检查用的最小后端：只为让各页面能渲染出真实内容，无业务逻辑。
//
// 为什么需要它：内页受路由守卫保护，接口不通时全部会被重定向到登录页，
// 响应式检查就只测到登录页却显示"通过"。这个 mock 提供足够的接口让
// 文件列表、管理端各页都渲染出来，从而使布局检查真正生效。
//
// 用法：node scripts/mock-api.mjs [port]   （默认 18081）
import http from 'node:http'

const PORT = Number(process.argv[2] || 18081)

const USERS = [
  { id: 1, employee_no: '1001', name: '张伟', role: 'admin', enabled: true, dir_rel: 'users/1', file_count: 4, used_bytes: 12345678, last_login_at: '2026-09-23T02:25:00Z', created_at: '2026-01-05T02:00:00Z' },
  { id: 2, employee_no: '1002', name: '李静', role: 'user', enabled: true, dir_rel: 'users/2', file_count: 4, used_bytes: 8765432, last_login_at: null, created_at: '2026-01-06T02:00:00Z', pinned: true },
  { id: 3, employee_no: '1003', name: '王强', role: 'user', enabled: false, dir_rel: 'users/3', file_count: 4, used_bytes: 2345678, last_login_at: null, created_at: '2026-01-07T02:00:00Z' },
  // 一个文件都没有的账号：不应出现在「全部文件」的子 tab 里
  { id: 4, employee_no: '1004', name: '赵六', role: 'user', enabled: true, dir_rel: 'users/1004', file_count: 0, used_bytes: 0, last_login_at: null, created_at: '2026-01-08T02:00:00Z' }

]

const NAMES = [
  '车间设备巡检记录表.xlsx', '电气安全隐患整改通知.docx', '2026年第一季度备件采购清单.pdf',
  '现场故障处理照片.png', '安全操作规程培训材料.pptx', '设备台账导出.csv',
  '线路改造方案说明.docx', '变频器说明书.pdf', '巡检路线图.png',
  '月度用电统计.xlsx', '一个名字特别长的文件用来验证移动端省略号是否正常工作.xlsx', '历史故障归档.zip'
]
const EXTS = ['.xlsx', '.docx', '.pdf', '.png', '.pptx', '.csv', '.docx', '.pdf', '.png', '.xlsx', '.xlsx', '.zip']

const FILES = NAMES.map((name, i) => ({
  id: i + 1,
  owner_id: (i % 3) + 1,
  owner_name: USERS[i % 3].name,
  owner_employee_no: USERS[i % 3].employee_no,
  original_name: name,
  ext: EXTS[i],
  size_bytes: 245678 * (i + 1),
  mime: 'application/octet-stream',
  sha256: 'a'.repeat(64),
  rel_path: `users/${(i % 3) + 1}/${i + 1}`,
  status: 'active',
  expires_at: new Date(Date.now() + (i + 1) * 86400000).toISOString(),
  deleted_at: null,
  purge_at: null,
  created_at: new Date(Date.now() - i * 3600000).toISOString(),
  updated_at: new Date().toISOString(),
  days_left: i + 1,
  can_edit: i % 3 === 0,
  preview_kind: 'none'
}))

const SETTINGS = {
  max_file_size_mb: 500, allowed_extensions: '', retention_days: 15,
  trash_days: 7, chunk_size_mb: 4, upload_enabled: true
}

const json = (res, data, status = 200) => {
  res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8' })
  res.end(JSON.stringify(data))
}

http
  .createServer((req, res) => {
    const url = new URL(req.url, 'http://localhost')
    const p = url.pathname

    if (p === '/api/health') return json(res, { status: 'ok', service: 'lan-drive-api', uptime_s: 1 })
    if (p === '/api/auth/me') return json(res, { user: USERS[0], settings: SETTINGS })
    if (p === '/api/auth/login')
      return json(res, { token: 'mock', expires_at: new Date(Date.now() + 86400000).toISOString(), user: USERS[0] })
    if (p === '/api/files') {
      const scope = url.searchParams.get('scope')
      const items = scope === 'mine' ? FILES.filter((f) => f.owner_id === 1) : FILES
      return json(res, { items, total: items.length, page: 1, page_size: 20 })
    }
    if (p === '/api/files/owners')
      return json(res, {
        items: USERS.map((u) => ({
          user_id: u.id, name: u.name, employee_no: u.employee_no,
          file_count: u.file_count, used_bytes: u.used_bytes,
          // 置顶状态由服务端给出并决定排序
          pinned: !!u.pinned
        })).sort((a, b) => Number(b.pinned) - Number(a.pinned) || a.user_id - b.user_id)
      })
    // 置顶切换：mock 里也真正改状态，便于端到端验证"点击 → 顺序变化"
    const pinMatch = p.match(/^\/api\/files\/owners\/(\d+)\/pin$/)
    if (pinMatch) {
      const id = Number(pinMatch[1])
      const target = USERS.find((u) => u.id === id)
      if (target) target.pinned = req.method === 'PUT'
      res.writeHead(204)
      return res.end()
    }
    if (p === '/api/uploads/config')
      return json(res, {
        allow_all: true, allowed_extensions: null, chunk_size: 4194304, chunk_size_mb: 4,
        max_file_size: 524288000, max_file_size_mb: 500, upload_enabled: true
      })
    if (p === '/api/admin/stats')
      return json(res, {
        users: 3, files: FILES.length, trashed: 2, total_bytes: 23456789,
        trashed_bytes: 11111111, expiring_7d: 3, uploads_in_progress: 0, expired_not_purged: 0
      })
    if (p === '/api/admin/users') return json(res, { items: USERS, total: USERS.length, page: 1, page_size: 20 })
    if (p === '/api/admin/files') return json(res, { items: FILES, total: FILES.length, page: 1, page_size: 20 })
    if (p === '/api/admin/settings') return json(res, SETTINGS)

    json(res, { error: 'not found', path: p }, 404)
  })
  .listen(PORT, '127.0.0.1', () => console.log(`mock api on http://127.0.0.1:${PORT}`))
