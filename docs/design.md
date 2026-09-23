# 局域网文件助手 — 设计说明

> 本文是**实现的唯一契约来源**：数据库结构、磁盘布局、接口形状、权限矩阵与生命周期都以本文为准。
> 代码与本文不一致时，视为缺陷。

- 后端：Go + Gin + MySQL，单二进制，**部署在内网**，只提供 `/api/**`
- 前端：Vue 3 + TypeScript + Vite + Vue Router + Naive UI，**部署在公网**静态托管
- 两者**分离部署、跨域通信**：前端通过 `VITE_API_BASE_URL` 指向内网 API，
  后端通过 `LANDRIVE_CORS_ALLOW` 白名单放行前端域名
- 数据根目录：默认 `./data`，容器内 `/data`，由 `LANDRIVE_DATA_DIR` 指定

## 0. 部署拓扑与网络约定

```
公网前端 (web/)  ──跨域 HTTPS──►  内网 API (server/)  ──►  MySQL + 文件存储
```

**接口不可达时的用户提示**：前端在请求未到达服务器（超时 / 连接失败 / 被浏览器
CORS 拦截）时，必须提示固定文案
**「无法在此网络下使用，请更换网络再试！」**，不得提示"密码错误"等误导信息。

判定规则（`web/src/api/network.ts`）：

| 情况 | 判定 | 提示 |
| --- | --- | --- |
| axios 无 `response`（ECONNABORTED / ERR_NETWORK / 超时） | 网络不可用 | 「无法在此网络下使用，请更换网络再试！」 |
| 请求被浏览器 CORS 拦截（同样无 `response`） | 网络不可用 | 同上 |
| 有 `response` 但状态 ≥ 500 | 服务端故障 | 显示后端返回的错误信息 |
| 有 `response` 且 4xx（含 401/403） | 网络正常 | 按业务语义提示 |

**后端 CORS 要求**（`server/internal/router/router.go`）：

- `OPTIONS` 预检必须在路由匹配前直接返回 `204`，否则浏览器报 preflight 失败；
- `Access-Control-Allow-Headers` 必须包含 `Authorization`（Bearer 令牌）；
- `Access-Control-Expose-Headers` 需包含 `Content-Disposition`，否则前端读不到下载文件名；
- 不使用 Cookie 凭据（令牌走 Authorization 头），因此不返回
  `Access-Control-Allow-Credentials`；
- 白名单为空表示不放行任何跨域请求（适配同源或 vite 代理的开发场景）。

---

## 1. 角色与权限矩阵

| 能力 | 用户（user） | 管理员（admin） |
| --- | :---: | :---: |
| 浏览 / 搜索全部文件 | ✅ | ✅ |
| 在线预览、下载任意人的文件 | ✅ | ✅ |
| 上传文件 | ✅ | ✅ |
| 重命名 / 删除 | 仅自己上传的 | 任意文件 |
| 查看回收站（已标记删除）、恢复、彻底删除 | ❌ | ✅ |
| 用户管理 | ❌ | ✅ |
| 系统配置（体积 / 类型 / 天数 / 分片 / 上传开关） | ❌ | ✅ |
| 审计日志、统计、存储一致性扫描 | ❌ | ✅ |

- 登录凭据：**工号 + 密码**；`name` 仅用于展示，允许重名，不参与登录。
- 每个用户拥有独立目录 `users/<id>`，该相对路径显式记录在 `users.dir_rel`。
- 属主校验在 handler 层完成：`file.owner_id == currentUser.id`。管理员走独立的 `/api/admin/*` 路由组。

---

## 2. 磁盘布局（与数据库一一对应）

```
<DATA_DIR>/
├── users/<user_id>/<file_id><ext>        每个用户目录；文件名 = 文件表主键 + 规范化扩展名
└── tmp/chunks/<upload_id>/<idx>.part     分片上传的临时分片
```

- 数据库中**只保存相对数据根目录的路径**（`users.dir_rel`、`files.rel_path`、`upload_chunks.rel_path`），一律使用 `/` 分隔。
- 落库前由 `internal/storage` 校验：非空、`filepath.IsLocal`、不含 `..`、长度受限。
- 原始文件名只存数据库；磁盘名不可猜。下载时用 RFC 5987 还原原始文件名。
- 一致性：写盘前先算 `rel_path`，成功后入库；`complete` 入库失败会回滚已落盘文件。管理端「存储一致性扫描」给出孤儿文件 / 缺失文件 / 非法路径三类报告。

---

## 3. 数据模型

时间列一律存 **UTC**（DSN 固定 `parseTime=true&loc=UTC&charset=utf8mb4`），前端按本地时区展示。
DDL 只用 MySQL 8.0 与 MariaDB 11 都支持的标准语法：不使用 JSON 列、函数索引、`CHECK`、CTE 写入。

### users

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | BIGINT UNSIGNED PK AI | 主键，目录名使用它 |
| `employee_no` | VARCHAR(32) UNIQUE | 登录名（工号），创建后不可修改 |
| `name` | VARCHAR(64) | 姓名，仅展示，可重名 |
| `password_hash` | VARCHAR(100) | bcrypt |
| `role` | VARCHAR(16) | `admin` / `user` |
| `enabled` | TINYINT(1) | 停用后立即无法登录，已签发令牌失效 |
| `dir_rel` | VARCHAR(255) | 该用户目录相对数据根目录的路径，如 `users/12` |
| `last_login_at` | DATETIME NULL | 最后登录时间 |
| `created_at` / `updated_at` | DATETIME | |

### files

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | BIGINT UNSIGNED PK AI | 主键，同时是磁盘文件名 |
| `owner_id` | BIGINT UNSIGNED FK→users(RESTRICT) | 上传者 |
| `original_name` | VARCHAR(255) | 原始文件名（含扩展名） |
| `ext` | VARCHAR(32) | 规范化扩展名，小写、带点（如 `.pdf`）；无扩展名为空串 |
| `size_bytes` | BIGINT UNSIGNED | 字节数 |
| `mime` | VARCHAR(128) | 展示与预览用 |
| `sha256` | CHAR(64) | 合并完成后计算的校验值 |
| `rel_path` | VARCHAR(512) UNIQUE | 相对数据根目录的路径 |
| `status` | VARCHAR(16) | `active` / `trashed` |
| `expires_at` | DATETIME | 到期标记删除的时间点 |
| `deleted_at` | DATETIME NULL | 进入回收站的时刻 |
| `purge_at` | DATETIME NULL | 物理删除的时刻 |
| `created_at` / `updated_at` | DATETIME | |

索引：`(owner_id, status)`、`(status, expires_at)`、`(purge_at)`、`(ext)`、`(created_at)`。

### upload_sessions

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | CHAR(32) PK | uuid 去掉连字符 |
| `owner_id` | BIGINT UNSIGNED FK→users(CASCADE) | 会话属主，只有本人可操作 |
| `original_name` / `ext` | VARCHAR | 与最终文件一致 |
| `size_bytes` | BIGINT UNSIGNED | 声明大小（必须与分片累计一致才可合并） |
| `chunk_size` | INT | 服务端下发的分片大小，客户端必须服从 |
| `total_chunks` | INT | `ceil(size/chunk_size)`，0 字节文件为 1 |
| `received_bytes` | BIGINT UNSIGNED | 已接收字节 |
| `status` | VARCHAR(16) | `uploading` / `done` / `aborted` |
| `dir_rel` | VARCHAR(255) | 目标目录（上传时为属主目录） |
| `sha256` | CHAR(64) | 客户端声明值，可空 |
| `created_at` / `updated_at` | DATETIME | |

### upload_chunks

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `upload_id` | CHAR(32) FK→upload_sessions(CASCADE) | |
| `idx` | INT | 从 0 开始 |
| `size_bytes` | INT | |
| `rel_path` | VARCHAR(512) | 如 `tmp/chunks/<id>/0.part` |

主键 `(upload_id, idx)`。

### settings

`key` VARCHAR(64) PK、`value` TEXT、`updated_at` DATETIME。

| key | 默认 | 取值 |
| --- | --- | --- |
| `max_file_size_mb` | `500` | 1–102400 |
| `allowed_extensions` | 空串 = 允许全部 | 逗号分隔，小写去点；空表示不限 |
| `retention_days` | `15` | 1–3650，到期标记删除 |
| `trash_days` | `7` | 0–365，标记删除后继续保留的天数 |
| `chunk_size_mb` | `4` | 1–64 |
| `upload_enabled` | `true` | 维护期全局关闭上传 |

### op_logs

`id`、`user_id`（可空）、`employee_no`、`action`、`target_type`、`target_id`、`detail`、`ip`、`created_at`。
索引 `(created_at)`、`(action)`、`(user_id)`。动作取值：`login`、`login_failed`、`logout`、`upload`、`rename`、`delete`、`restore`、`purge`、`download`、`user_create`、`user_update`、`user_delete`、`password_change`、`password_reset`、`settings_update`、`maintain_expire`、`maintain_purge`、`maintain_orphan`、`storage_scan`。

---

## 4. 文件生命周期

```
上传完成 ──► active ──(now ≥ expires_at)──► trashed ──(now ≥ purge_at)──► 物理删除（DB 行 + 磁盘文件）
              ▲                                │
              └──────── 管理员恢复 ◄────────────┘
```

- 上传完成时：`status=active`，`expires_at = now + retention_days`。
- 到期扫描（每 5 分钟）：`active` 且 `expires_at <= now` → `status=trashed`、`deleted_at=now`、`purge_at=now + trash_days`。
- 物理清理（每 10 分钟）：`trashed` 且 `purge_at <= now` → 先删磁盘文件再删 DB 行；磁盘文件已不存在也照删 DB 行。
- 属主主动删除：立即 `status=trashed`、`deleted_at=now`、`purge_at=now + trash_days`。
- 管理员恢复：`status=active`、清空 `deleted_at`/`purge_at`、`expires_at = now + retention_days`。
- **trashed 文件对普通用户（含属主）完全不可见、不可下载、不可预览**；只有管理员回收站可见。

### 定时任务

| 周期 | 动作 |
| --- | --- |
| 5 分钟 | 到期标记删除 |
| 10 分钟 | 物理清理（磁盘 + 数据库） |
| 1 小时 | 僵尸上传会话（`uploading` 且 24h 未更新）连分片一起清理 |
| 每天 03:30 | 审计日志裁剪（保留 90 天）；孤儿文件扫描（`users/**` 中不在 `files.rel_path` 的文件移入 `tmp/orphans`，再保留 7 天后删除） |

多副本部署时用 `GET_LOCK('lanfs_maintain', 0)` 保证同一时刻只有一个实例执行清理。

---

## 5. HTTP 接口契约

- 统一前缀 `/api`；请求与响应均为 JSON（文件上传分片为裸二进制）。
- 错误响应统一为 `{"error":"中文消息"}`，并带语义化的 HTTP 状态码。
- 认证：`Authorization: Bearer <JWT>`，HS256，有效期 12 小时。401 统一跳转登录页。
- 所有时间字段为 RFC 3339 UTC 字符串（如 `2026-09-01T03:04:05Z`）。

### 公共类型

```ts
type User = {
  id: number; employee_no: string; name: string; role: 'admin' | 'user';
  enabled: boolean; dir_rel: string;
  file_count: number; used_bytes: number;
  last_login_at: string | null; created_at: string;
}

type FileItem = {
  id: number; owner_id: number; owner_name: string; owner_employee_no: string;
  original_name: string; ext: string; size_bytes: number; mime: string; sha256: string;
  rel_path: string; status: 'active' | 'trashed';
  expires_at: string; deleted_at: string | null; purge_at: string | null;
  days_left: number; is_mine: boolean; can_edit: boolean;
  created_at: string; updated_at: string;
}

type UploadSession = {
  upload_id: string; original_name: string; size_bytes: number;
  chunk_size: number; total_chunks: number; uploaded: number[];
  received_bytes: number; status: 'uploading' | 'done' | 'aborted';
  expires_at: string;
}

type Settings = {
  max_file_size_mb: number; allowed_extensions: string;
  retention_days: number; trash_days: number;
  chunk_size_mb: number; upload_enabled: boolean;
}

type Stats = {
  users: number; files: number; trashed: number;
  total_bytes: number; trashed_bytes: number; expiring_7d: number; uploads_in_progress: number;
}

type Paged<T> = { items: T[]; total: number; page: number; page_size: number }
```

### 认证

| 方法 | 路径 | 请求 | 响应 |
| --- | --- | --- | --- |
| POST | `/api/auth/login` | `{employee_no, password}` | `{token, expires_at, user: User}` |
| GET | `/api/auth/me` | — | `{user: User, settings: Settings}` |
| POST | `/api/auth/password` | `{old_password, new_password}` | `{ok: true}` |

登录失败限流：同 IP + 同工号 5 次 / 5 分钟 → 429。失败提示统一为「工号或密码错误」。

### 文件（登录即可）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/files` | 查询参数 `scope=all\|mine`、`owner_id`、`q`、`ext`、`page`（默认 1）、`page_size`（默认 20，上限 200）、`sort=created_at\|size_bytes\|expires_at\|original_name`、`order=desc\|asc`；返回 `Paged<FileItem>`。`scope=mine` 等价于 `owner_id=自己` |
| GET | `/api/files/owners` | 返回 `{items: [{user_id, employee_no, name, file_count, used_bytes}], total}`，仅统计 `active` |
| GET | `/api/files/:id` | `FileItem` |
| GET | `/api/files/:id/content` | 内联字节流，支持 `Range`，`Content-Type` 取 `mime`；用于预览与音视频拖动 |
| GET | `/api/files/:id/download` | 附件下载，`Content-Disposition` 用 RFC 5987 还原原始文件名 |
| PATCH | `/api/files/:id` | `{original_name}`，仅属主；只允许改主名，扩展名不可变（否则 400）；重名自动追加 ` (n)` |
| DELETE | `/api/files/:id` | 属主软删除（进回收站），返回 `{ok: true}` |

### 分片上传

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/uploads/init` | `{file_name, file_size, sha256?}` → `UploadSession`。同属主 + 同名 + 同大小 + 同 sha 的未完成会话直接复用（断点续传）；超体积 / 扩展名不允许 → 413 / 400，且不产生任何磁盘写入；`upload_enabled=false` → 403 |
| PUT | `/api/uploads/:id/chunks/:idx` | 裸二进制写入第 `idx` 片；幂等覆盖；`idx` 越界 400；累计字节超过声明大小 → 413 |
| GET | `/api/uploads/:id` | 会话状态与已上传分片索引（刷新后续传依据）；会话不存在 / 已清理 → 410 |
| POST | `/api/uploads/:id/complete` | 校验分片齐全且累计字节一致 → 顺序合并 → sha256 → 落最终文件 → 入库 → 返回 `FileItem`；重复调用返回已生成文件（幂等） |
| DELETE | `/api/uploads/:id` | 取消会话，删除分片 |

### 管理端（`/api/admin`，仅管理员）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/admin/users` | `q`（工号或姓名）、`page`、`page_size` → `Paged<User>` |
| POST | `/api/admin/users` | `{employee_no, name, role, password}` → `User`；同时创建 `users/<id>` 目录 |
| PATCH | `/api/admin/users/:id` | `{name?, role?, enabled?}` → `User`；工号不可改 |
| POST | `/api/admin/users/:id/password` | `{new_password}` → `{ok: true}` |
| DELETE | `/api/admin/users/:id` | 有文件时 409（附 `file_count`）；`?purge_files=1` 先彻底删除其文件再删账号 |
| GET | `/api/admin/settings` | `Settings` |
| PUT | `/api/admin/settings` | 部分字段更新 → `Settings`（校验 + 审计 + 刷新缓存） |
| GET | `/api/admin/files` | `status=active\|trashed\|all`、`owner_id`、`q`、`page`、`page_size` → `Paged<FileItem>` |
| POST | `/api/admin/files/:id/restore` | 恢复 → `FileItem` |
| DELETE | `/api/admin/files/:id` | 立即彻底删除（磁盘 + 行） |
| GET | `/api/admin/logs` | `action`、`q`、`user_id`、`days=1\|7\|30`、`from`、`to`、`page`、`page_size` → `Paged<LogItem>` |
| GET | `/api/admin/stats` | `Stats` |
| POST | `/api/admin/storage/scan` | `{orphans: string[], missing: string[], invalid: string[], scanned_at: string}` |

---

## 6. 分片上传协议（前后端约定）

1. 客户端请求 `init`，携带 `file_name`、`file_size`、可选的 `sha256`（小文件即时算，大文件可留空）。
2. 服务端返回 `chunk_size`（来自设置）、`total_chunks`、`uploaded[]`。
3. 客户端按 3 个并发 `PUT chunks/:idx` 上传缺失分片；每片完成即刷新进度。
4. 全部完成后 `POST complete`，服务端合并、校验、入库。
5. 续传：`localStorage` 键 `lanfs-upload:<工号>:<文件名>:<大小>:<最后修改时间>` 存 `{upload_id, chunk_size, done[]}`；页面刷新后 `GET /api/uploads/:id` 校验，410 则重新 `init`。
6. 失败重试：单分片最多重试 3 次；会话 24 小时未活动被清理，客户端收到 410 自动重开。
7. 0 字节文件：`total_chunks = 1`，允许空分片体。

---

## 7. 配置（环境变量）

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `LANDRIVE_LISTEN` | `:8080` | 监听地址 |
| `LANDRIVE_MYSQL_DSN` | 必填 | 如 `lanfs:pass@tcp(mysql:3306)/lanfs` |
| `LANDRIVE_AUTO_CREATE_DB` | `true` | 自动 `CREATE DATABASE IF NOT EXISTS` |
| `LANDRIVE_JWT_SECRET` | 必填，≥32 字符 | JWT 签名密钥 |
| `LANDRIVE_DATA_DIR` | `./data` | 数据根目录 |
| `LANDRIVE_ADMIN_EMPLOYEE_NO` | `admin` | 首次启动种子管理员工号 |
| `LANDRIVE_ADMIN_PASSWORD` | 空 | 首次启动种子管理员密码；`users` 表为空且未设置时启动失败 |
| `LANDRIVE_ADMIN_NAME` | `系统管理员` | 种子管理员姓名 |
| `LANDRIVE_TRUST_PROXY` | `false` | 是否采信 `X-Forwarded-For` |
| `LANDRIVE_CORS_ALLOW` | 空 | ★ 逗号分隔的**前端域名**白名单；公网前端必须加入，否则跨域被拦截 |
| `LANDRIVE_LOG_KEEP_DAYS` | `90` | 审计日志保留天数 |

---

## 8. 前端能力

| 路由 | 页面 | 说明 |
| --- | --- | --- |
| `/login` | 登录 | 工号 + 密码 |
| `/files` | 全部文件 | 左侧用户目录树 + 右侧文件表，人人可下载 / 预览 |
| `/files/mine` | 我的文件 | 仅自己的文件，可改名 / 删除 |
| `/upload` | 上传 | 分片、并发、断点续传、进度与重试 |
| `/preview/:id` | 预览 | 全屏预览 |
| `/admin/users` | 用户管理 | 仅管理员 |
| `/admin/settings` | 系统配置 | 仅管理员 |
| `/admin/files` | 全部文件（含回收站） | 仅管理员 |
| `/admin/logs` | 审计日志 | 仅管理员 |
| `/admin/dashboard` | 统计看板 | 仅管理员 |
| `/profile` | 个人设置 | 修改本人密码 |
| `/network-blocked` | 网络不可用 | 接口不可达时展示，文案「无法在此网络下使用，请更换网络再试！」 |

前端环境变量（构建期注入）：

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `VITE_API_BASE_URL` | `/api` | 内网 API 完整地址（含 `/api`）；公网部署必设 |
| `VITE_API_PROBE_TIMEOUT` | `6000` | 连通性探测超时（毫秒） |
| `VITE_DEV_API_TARGET` | `http://127.0.0.1:8080` | 仅开发态 vite 代理目标 |

容器部署时还有**运行时**变量（不重新构建镜像即可切换内网地址）：

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `LANDRIVE_API_BASE_URL` | 构建期值 | 容器启动时写入 `/config.js`，优先级高于构建期 `VITE_API_BASE_URL` |
| `LANDRIVE_API_PROBE_TIMEOUT` | `6000` | 运行时覆盖探测超时（毫秒） |

API 地址解析优先级：**运行时 `/config.js` → 构建期 `VITE_API_BASE_URL` → 同源 `/api`**，
实现只在 `web/src/api/index.ts` 的 `resolveApiBaseUrl`。

连通性探测：登录页与路由守卫调用 `GET /api/health`，用它区分
「网络不可达」与「账号密码错误」。

预览矩阵（**纯前端**，通过 `/api/files/:id/content` 取 blob 后本地渲染）：

| 类型 | 实现 |
| --- | --- |
| `.docx` | `docx-preview` `renderAsync` |
| `.xlsx` | `exceljs` 解析后自建表格渲染（限制 2000 行 / 100 列并提示截断） |
| `.pptx` | `pptx-preview` `init` + `preview` |
| `.pdf` | `<iframe>` + blob URL |
| 图片 | `<img>` |
| 文本类 | `<pre>` 纯文本转义 |
| 音视频 | `<audio>` / `<video>`（依赖后端 Range） |
| `.doc/.xls/.ppt` 旧格式、压缩包等 | 提示「该格式不支持在线预览，请下载后查看」 |

依赖体积控制：预览器一律 `defineAsyncComponent` + 动态 `import()`，主包不引入任何预览库。

---

## 9. 安全与健壮性

- 路径只来自数据库，绝不直接拼接用户输入；`storage` 层校验全部相对路径。
- 扩展名策略在 `init` 与 `complete` 双重校验；以真实扩展名为准，大小写不敏感。
- 未知类型强制 `application/octet-stream` + `Content-Disposition: attachment`；服务端不执行任何存储内容。
- 密码 bcrypt（cost 10）；登录失败限流；修改密码后旧令牌因 `pwd_ver` 声明不匹配而失效。
- 所有写操作记审计日志（含下载）。
- 数据库连接失败时启动重试 60 秒，超时给出 DSN 提示（适配容器启动顺序）。
