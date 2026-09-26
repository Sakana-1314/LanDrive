# 局域网文件助手 — 设计说明

> 本文是**实现的唯一契约来源**：数据库结构、磁盘布局、接口形状、权限矩阵与生命周期都以本文为准。
> 代码与本文不一致时，视为缺陷。

- 后端：Go + Gin + MySQL，单二进制，**部署在内网**，只提供 `/api/**`
- 前端：Vue 3 + TypeScript + Vite + Vue Router + Naive UI，**部署在公网**静态托管
- 两者**分离部署、跨域通信**：前端通过构建期 `HOST` 指向内网 API（或用运行时 `LANDRIVE_API_BASE_URL` 覆盖），
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
| 上传文件（在「我的文件」内拖入任意位置，或用「上传文件」按钮选择） | ✅ | ✅ |
| 重命名 / 删除 | 仅自己上传的 | 任意文件 |
| 建文件夹 / 改名 / 删除文件夹 | 仅自己的 | 任意 |
| 上传到指定文件夹 | 仅自己的文件夹 | 自己的文件夹 |
| 创建分享链接（文件或文件夹） | 仅自己的 | 任意（含撤销他人的） |
| 查看分享列表 | ✅（**所有人创建的都能看到**） | ✅ |
| 撤销分享 | 仅自己创建的 | 任意 |
| 打开分享链接 | ✅ **无需登录** | ✅ **无需登录** |
| 查看回收站（已标记删除）、恢复、彻底删除 | ❌ | ✅ |
| 用户管理 | ❌ | ✅ |
| 系统管理（体积 / 类型 / 天数 / 分片 / 上传开关 + 存储一致性检查） | ❌ | ✅ |
| 统计、存储一致性扫描 | ❌ | ✅ |

- 登录凭据：**工号 + 密码**；`name` 仅用于展示，允许重名，不参与登录。
- 每个用户拥有独立目录 `users/<工号>`（**工号是账号的不可变标识**，因为它就是目录名），该相对路径显式记录在 `users.dir_rel`。
  建账号前会检查该目录是否已被占用并拒绝（409）：**存量数据的目录是早期的 `users/<id>`**，
  因此 `1`、`5` 这类纯数字工号会与旧目录同名，两个账号共用一个磁盘目录、互相看到对方文件。
- 属主校验在 handler 层完成：`file.owner_id == currentUser.id`。管理员走独立的 `/api/admin/*` 路由组。

---

## 2. 磁盘布局（与数据库一一对应）

```
<DATA_DIR>/
├── users/<工号>/<file_id><ext>              用户根目录下的文件；文件名 = 文件表主键 + 规范化扩展名
├── users/<工号>/<文件夹...>/<file_id><ext>  文件夹内的文件（**真实磁盘层级**，与 folders.path 一一对应）
└── tmp/chunks/<upload_id>/<idx>.part        分片上传的临时分片
```
- **文件夹是真实磁盘层级**：`folders.path`（相对用户根目录，如 `报表/2026`）就是磁盘上的
  `users/<工号>/报表/2026`。因此改名/移动文件夹要同时改数据库 `path`+`rel_path` 与磁盘目录，
  三者必须一致，否则一致性扫描会报缺失。

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
| `expires_at` | DATETIME **NULL** | 到期标记删除的时间点；**NULL = 永久**，不参与到期扫描（0004 迁移由 NOT NULL 改为可空） |
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
| `preview_max_size_mb` | `20` | 0–102400，**0 表示不限制预览体积** |
| `permanent_quota_mb` | `102400`（100G） | 0–104857600，**全站共享**的永久空间上限；**0 表示关闭「设为永久」** |

`preview_max_size_mb` 只约束**在线预览**，与 `max_file_size_mb`（能不能上传）是两件事：
预览要把整个文件读进浏览器（docx/xlsx/pptx 还要交给纯前端库解析），超大文件会把标签页
拖死，因此单独设一道闸。**下载始终不受它影响** —— 大文件下到本地看没问题。

`permanent_quota_mb` 是「永久文件」的总量闸（详见「文件有效期」一节）。两个反直觉点：
**全站一个池**（不是每人一份，否则总量随人数无限增长）、**0 = 关闭该功能**
（不是"只允许 0 字节"）。管理员可以把配额调到低于当前已用：已设为永久的文件**不回收**，
只是新的"设为永久"会因超额度被拒。

### folders

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | BIGINT UNSIGNED PK | |
| `owner_id` | BIGINT UNSIGNED | 属主，FK → `users(id)` ON DELETE CASCADE |
| `parent_id` | BIGINT UNSIGNED NULL | 上级目录；NULL = 根层的一级目录 |
| `name` | VARCHAR(255) | 单层目录名，不含斜杠 |
| `path` | VARCHAR(512) | 相对**用户根目录**的路径，如 `报表/2026`；`UNIQUE(owner_id, path)` |
| `status` | VARCHAR(16) | `active` / `deleted` |
| `created_at` / `updated_at` | DATETIME | UTC |

- 目录**软删除**：删除时置 `status='deleted'` 并把 `path` 追加 `:<id>` 墓碑后缀。原因有两个：
  分享指向目录记录，删行会让「文件夹已被删除」退化成「链接无效」；
  且 `UNIQUE(owner_id, path)` 会导致删掉 `报表` 后再也无法新建同名目录。
  后缀用 `:` 是安全的 —— 目录名清洗会剥掉冒号，用户造不出含冒号的路径。
- 删目录时其中文件先软删（进回收站，管理员可恢复），语义与删文件一致。

### shares

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | BIGINT UNSIGNED PK | |
| `token` | CHAR(32) | URL 凭证，`crypto/rand` 生成，`UNIQUE` |
| `owner_id` | BIGINT UNSIGNED | 创建者，FK → `users(id)` ON DELETE CASCADE |
| `target_type` | VARCHAR(8) | `file` / `folder` |
| `file_id` / `folder_id` | BIGINT UNSIGNED NULL | 二选一，FK → `files(id)` / `folders(id)` ON DELETE CASCADE |
| `expire_days` | INT NULL | 有效期天数；**NULL = 永久** |
| `expires_at` | DATETIME NULL | 到期时刻；NULL = 永久 |
| `view_count` | BIGINT UNSIGNED | 成功访问次数，SQL 原子自增 |
| `created_at` / `updated_at` | DATETIME | UTC |

**分享指向记录，不指向磁盘路径** —— 这一条决定了三项需求语义同时成立：

| 事件 | 结果 | 原因 |
| --- | --- | --- |
| 文件改名 / 移动目录 | 链接**仍可用**，显示新名字 | 磁盘名是 `<file_id><ext>`，路径由记录解析 |
| 文件被删除（进回收站） | 明确提示**「分享的文件已被删除」** | 记录还在，`status=trashed` |
| 管理员恢复文件 | 同一条链接**自动恢复可用** | 一直指向同一 `file_id` |
| 记录被彻底清除 / 账号被删 | 分享行随外键 CASCADE 删除 → notfound | 记录已不存在 |
| 文件夹被删除 | 提示**「分享的文件夹已被删除」** | `folders.status='deleted'` |

- 有效期只允许 `1 / 3 / 7 / 30` 天或永久（NULL），其余一律 400。
- 过期分享在过期 **30 天**后被每日维护任务清除（`expires_at IS NULL` 的永久分享不受影响）；
  保留这 30 天是为了让分享者仍能在列表里看到「已过期」，收链接的人也能看到明确原因。

### user_pins

`owner_user_id`、`target_user_id`、`created_at`，主键 `(owner_user_id, target_user_id)`，索引 `(owner_user_id)`，两个外键均 `ON DELETE CASCADE`。

表示「谁置顶了谁的目录」。置顶是**每个账号各自一份**的个人偏好，因此按 `(owner, target)` 建表而不是在 `users` 上加全局标记位——不同人看到的顺序可以不同。删账号时双向 CASCADE 自动清理引用，不会留下指向已删用户的幽灵项。

`owner_user_id == target_user_id` 由服务层拒绝（400）：自己的文件在「我的文件」里始终可达，置顶自己没有意义。

> 审计日志（原 `op_logs` 表）已整体下线，迁移 `0002_drop_logs_user_pins.sql` 会 DROP 该表。

---

## 4. 文件生命周期

```
上传完成 ──► active ──(now ≥ expires_at)──► trashed ──(now ≥ purge_at)──► 物理删除（DB 行 + 磁盘文件）
              ▲                                │
              └──────── 管理员恢复 ◄────────────┘

永久文件（expires_at IS NULL）：active ──删除──► trashed ──► 物理删除
（**不参与到期扫描**，只能由删除/彻底删除终结）
```

- 上传完成时：`status=active`，`expires_at = now + retention_days`。
- 到期扫描（每 5 分钟）：`active` 且 `expires_at <= now` → `status=trashed`、`deleted_at=now`、`purge_at=now + trash_days`。
  `expires_at IS NULL` 的行天然不被 `<=` 命中，因此**永久文件不会被扫到** —— 这正是"永久"的实现方式。
- 物理清理（每 10 分钟）：`trashed` 且 `purge_at <= now` → 先删磁盘文件再删 DB 行；磁盘文件已不存在也照删 DB 行。
- 属主主动删除：立即 `status=trashed`、`deleted_at=now`、`purge_at=now + trash_days`。
- 管理员恢复：`status=active`、清空 `deleted_at`/`purge_at`；原本**有期限**的按 `now + retention_days` 重新计时，
  原本**永久**的保持 `expires_at` 为 NULL（恢复的语义是"回到删除前的样子"，不顺手改用户的设定）。
- **trashed 文件对普通用户（含属主）完全不可见、不可下载、不可预览**；只有管理员回收站可见。

### 文件有效期（永久）

`expires_at` 为 NULL 即"永久"：不会自动清理，需要手动删除。与 `shares` 表的
`expires_at IS NULL = 永久分享` 是同一套语义（用 NULL 而不是 9999 哨兵时间，理由见 0004 迁移注释）。

接口：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/files/permanent` | `{enabled, quota_bytes, used_bytes, free_bytes}`；**全站**口径，前端在二次确认里展示"还剩多少" |
| PUT | `/api/files/:id/permanent` | `{permanent?: bool}`，省略或 `true` = 设永久；`false` = 改回有期限（按当前保留天数重新计时） |
| PUT | `/api/folders/:id/permanent` | 同上，但作用范围是**该目录树下（含子孙）的所有 active 文件**；返回 `{affected}` |

几个刻意的取舍：

- **有效期是文件的属性，不是目录的**。目录级接口只是一次"批量操作的范围"，
  所以返回受影响文件数、不写 folders 表。范围用 `rel_path` 前缀匹配，
  与"统计该目录下有多少文件"同一套算法，口径天然一致。
- **配额是全站共享的一个池**，计数口径是"所有 `active` 且 `expires_at IS NULL` 文件的
  `size_bytes` 之和"。**刻意不含回收站里的永久文件**：算进来会让"删文件"这个唯一的自救
  手段不释放额度（额度满时用户就完全没出路了）。
  已知代价（如实记下来）：**属主删单个文件**会设 `purge_at`，到点后磁盘被回收，所以那部分
  释放是临时的；但**删目录**进回收站的文件 `purge_at` 是 `NULL`（`store.SoftDeleteFilesUnderPath`
  的既有行为，与"属主主动删除"那条 SQL 不一致），而 `ListPurgeable` 要求 `purge_at` 非空 ——
  这些行永远不会被自动清理。于是"删目录"能释放额度却不释放磁盘，这层放大不会自行收敛。
  要修的是删目录那条既有 SQL，不属本次范围，已记入 `todo.md` 待办。
- 鉴权：属主或管理员（与改名/删除同一套 `checkCanModify`）；只对 **active** 文件开放 ——
  回收站里的文件先恢复再说，否则设了永久也马上会被清理掉。
  ⚠️ `checkCanModify` 对管理员**直接放行**（管理员要能打理回收站），所以这条不能只靠它守，
  服务层必须自己再判一次 `status`，否则管理员能给回收站里的文件设永久：它不计入配额
  （配额只算 active），恢复之后却突然变成永久 —— 由 `TestPermanentQuotaEdgeCases` 守卫。
- 配额校验是"读已用 → 判定 → 单条 UPDATE"，**没有事务也没有行锁**：并发下两个请求可能
  一起通过、略微超出上限。它是业务闸而不是安全边界，所以没有为此把写入串行化；
  真要卡死得改成条件 UPDATE 或 `SELECT … FOR UPDATE`。
- 配额不足的提示要写明**本次涉及几个文件**（目录级用 `PermanentizableStats` 的真实计数）。
  写死成 1 的话，200 个文件的目录被拒时会告诉用户"涉及 1 个文件"，与他要清理的范围对不上。
- 单文件设永久的字节数**只算"还不是永久"的那些**：已是永久的不再重复计入新增占用，
  否则对同一个文件重发一次（超时重试、双标签页）就会被 409 拒掉 —— PUT 应当幂等。
- 超配额返回 **409**（与当前服务端状态冲突，清点东西再试即可），
  错误文案带「已用 / 上限 / 还需多少」，用户可直接照做；配额为 0（功能关闭）返回 **403**。

#### 回滚注意（0004 迁移不可逆）

`0004` 只把 `files.expires_at` 由 `NOT NULL` 改成可空，**没有回滚脚本**。要退回旧版
server 镜像（AGENTS.md §9 的 `images-before.txt` 那套流程）**必须先补数据**：

```sql
UPDATE files SET expires_at = NOW() + INTERVAL <retention_days> DAY WHERE expires_at IS NULL;
```

原因是旧代码把该列直接 `Scan` 进 `time.Time`（不是 `sql.NullTime`），遇到 NULL 会让
**所有**走 `scanFile` 的接口整体 500 —— 文件列表、详情、预览、分享解析全都受影响，
不是"跳过一个文件"那么轻。补完之后该列不再有 NULL，旧代码即可正常读。

### 定时任务

| 周期 | 动作 |
| --- | --- |
| 5 分钟 | 到期标记删除 |
| 10 分钟 | 物理清理（磁盘 + 数据库） |
| 1 小时 | 僵尸上传会话（`uploading` 且 24h 未更新）连分片一起清理 |
| 每天 03:30 | 孤儿文件扫描（`users/**` 中不在 `files.rel_path` 的文件移入 `tmp/orphans`，再保留 7 天后删除）；清理过期超过 30 天的分享 |

多副本部署时用 `GET_LOCK('lanfs_maintain', 0)` 保证同一时刻只有一个实例执行清理。

---

## 5. HTTP 接口契约

- 统一前缀 `/api`；请求与响应均为 JSON（文件上传分片为裸二进制）。
- 错误响应统一为 `{"error":"中文消息"}`，并带语义化的 HTTP 状态码。
- 认证：`Authorization: Bearer <JWT>`，HS256，有效期 12 小时。401 统一跳转登录页。
- 所有时间字段为 RFC 3339 UTC 字符串（如 `2026-09-01T03:04:05Z`）。

**接口分四档**，新增接口必须明确落在其中一档：

| 档 | 路径 | 鉴权 |
| --- | --- | --- |
| 公开 | `/api/health`、`/api/auth/login` | 无 |
| **免登录（分享）** | `/api/s/:token`、`/api/s/:token/download`、`/api/s/:token/content` | **无**，凭证就是 token |
| 登录 | `/api/auth/*`、`/api/files/*`、`/api/folders/*`、`/api/shares/*`、`/api/uploads/*` | `RequireAuth` |
| 管理员 | `/api/admin/*` | `RequireAdmin` |

免登录档的安全边界完全依赖 token，因此有两条硬约束：
1. 这三个接口**只接受 token**，不接受任何 `id` / 路径参数 —— 否则等于给出一个枚举他人文件的入口。
2. `/content` 仍然只对 `storage.IsInlinePreviewable` 放行的类型内联，其余强制附件下发。
   `.svg` / `.html` / `.js` 绝不内联，否则免登录链接会变成「托管并执行任意脚本」的公开入口。

### 公共类型

```ts
type User = {
  id: number; employee_no: string; name: string; role: 'admin' | 'user';
  enabled: boolean; dir_rel: string;
  file_count: number; used_bytes: number;
  last_login_at: string | null; created_at: string;
}

type FileItem = {
  id: number; owner_id: number; folder_id: number; /* 0 = 用户根目录 */
  owner_name: string; owner_employee_no: string;
  original_name: string; ext: string; size_bytes: number; mime: string; sha256: string;
  rel_path: string; status: 'active' | 'trashed';
  expires_at: string | null;  /* null = 永久 */
  deleted_at: string | null; purge_at: string | null;
  /* days_left 对永久文件恒为 0；判断"是否永久"要看 permanent，别用 days_left 推断 */
  days_left: number; permanent: boolean;
  is_mine: boolean; can_edit: boolean;
  created_at: string; updated_at: string;
}

type PermanentStatus = {
  enabled: boolean;      /* 配额为 0 时为 false（永久功能被关闭） */
  quota_bytes: number; used_bytes: number;
  free_bytes: number;    /* 已超额时为 0，不会是负数 */
}

type UploadSession = {
  upload_id: string; original_name: string; size_bytes: number;
  chunk_size: number; total_chunks: number; uploaded: number[];
  received_bytes: number; status: 'uploading' | 'done' | 'aborted';
  expires_at: string;
}

type Folder = {
  id: number; owner_id: number; parent_id: number | null;
  name: string; path: string;            /* 相对用户根目录，如 "报表/2026" */
  status: 'active' | 'deleted';
  owner_name: string; owner_employee_no: string;
  file_count: number;    /* 该目录**直接**包含的文件数（不含子目录） */
  used_bytes: number; sub_folder_count: number;
  created_at: string; updated_at: string;
}

type FolderListing = {
  owner_id: number; folder_id: number;
  folders: Folder[];
  breadcrumb: Folder[];       /* 从根到当前目录，供面包屑 */
  current: Folder | null;
}

type Share = {
  id: number; token: string; owner_id: number;
  target_type: 'file' | 'folder';
  file_id: number | null; folder_id: number | null;
  expire_days: number | null;  /* null = 永久 */
  expires_at: string | null;   /* null = 永久 */
  view_count: number;
  owner_name: string; owner_employee_no: string;
  target_name: string; target_size_bytes: number;
  target_deleted: boolean;     /* 目标已被删除，前端提示「已被删除」 */
  expired: boolean;
  created_at: string; updated_at: string;
}

/* 免登录解析结果。status 必须区分三种失效原因，否则用户无法判断该找谁。 */
type ShareResolved = {
  status: 'ok' | 'expired' | 'deleted' | 'notfound';
  name: string; size_bytes: number; mime: string; ext: string; kind: string;
  file_count: number; total_bytes: number; owner_name: string;
  target_type: 'file' | 'folder' | '';
  expire_days: number | null; expires_at: string | null;
  created_at: string | null; view_count: number;
}

type Settings = {
  max_file_size_mb: number; allowed_extensions: string;
  retention_days: number; trash_days: number;
  chunk_size_mb: number; upload_enabled: boolean;
  preview_max_size_mb: number;  // 0 = 不限制预览体积
  permanent_quota_mb: number;   // 全站共享；0 = 关闭「设为永久」
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
| GET | `/api/files` | 查询参数 `scope=all\|mine`、`owner_id`、`folder_id`（只看该文件夹）、`folder_root=1\|true`（只看根目录）、`q`、`ext`、`page`（默认 1）、`page_size`（默认 20，上限 200）、`sort=created_at\|size_bytes\|expires_at\|original_name`、`order=desc\|asc`；返回 `Paged<FileItem>`。`scope=mine` 等价于 `owner_id=自己`。`folder_id` 与 `folder_root` 需要独立表达：`0` 本身就是"根目录"这个合法取值，没法用零值同时表示"不过滤" |
| GET | `/api/files/owners` | 返回 `{items: [{user_id, employee_no, name, file_count, used_bytes, pinned}], total}`，仅统计 `active`；`pinned` 与排序按当前登录者计算（置顶优先） |
| PUT | `/api/files/owners/:id/pin` | 置顶某人目录（幂等）→ 204 |
| DELETE | `/api/files/owners/:id/pin` | 取消置顶（幂等）→ 204 |
| GET | `/api/files/:id` | `FileItem` |
| GET | `/api/files/:id/content` | 内联字节流，支持 `Range`，`Content-Type` 取 `mime`；用于预览与音视频拖动。**超 `preview_max_size_mb` 的类型不内联**（降级为附件），下载仍可用 |
| GET | `/api/files/:id/download` | 附件下载，`Content-Disposition` 用 RFC 5987 还原原始文件名 |
| PATCH | `/api/files/:id` | `{original_name}`，仅属主；只允许改主名，扩展名不可变（否则 400）；重名自动追加 ` (n)` |
| DELETE | `/api/files/:id` | 属主软删除（进回收站），返回 `{ok: true}` |

### 文件夹（登录即可）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/folders` | `owner_id`（0=自己）、`folder_id`（0=根层）；返回 `FolderListing`（子目录 + 面包屑） |
| POST | `/api/folders` | `{name, parent_id?, owner_id?}`；普通用户只能在自己的目录里建（否则 403）；同名 409 |
| PATCH | `/api/folders/:id` | `{name, parent_id?}`，改名或移动（`parent_id` 省略则位置不变）；移动到自身或子孙下 400 |
| DELETE | `/api/folders/:id` | 软删除目录及子孙，其中文件先软删（进回收站）；返回 `{ok: true, soft_deleted_files: n}` |

### 分享（登录即可，**打开链接不需要登录**）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/shares` | `mine=1` 只看自己创建的；默认**返回所有人的分享**（含创建者姓名/工号）。返回 `Paged<Share>` |
| GET | `/api/shares/options` | `{expire_days: [1,3,7,30]}`；由服务端给出，避免前后端各硬编码一套 |
| POST | `/api/shares` | `{target_type: 'file'\|'folder', target_id, expire_days?}`；`expire_days` 省略/`null` = **永久**（默认），其余只允许 1/3/7/30（否则 400）；普通用户只能分享自己的（403），管理员可分享任意 |
| DELETE | `/api/shares/:id` | 撤销；创建者本人或管理员（否则 403）；撤销后链接立即失效 |

### 分享的免登录访问（**无鉴权**）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/s/:token` | 解析分享。**统一返回 200 + `status` 字段**（`ok` / `expired` / `deleted` / `notfound`）—— 用 404 表示一切会让前端无法区分"过期""文件被删""链接错了" |
| GET | `/api/s/:token/download` | 附件下载；失效时 `410`（过期/已删除，文案分别是「该分享链接已过期」「分享的文件已被删除」）或 `404`（无效）。文件夹分享返回**流式 zip**（上限 2000 个文件 / 2 GiB，超限 413） |
| GET | `/api/s/:token/content` | 内联字节流，仍受 `IsInlinePreviewable` 限制；其余强制附件下发。**超 `preview_max_size_mb` 的文件同样降级为附件**（下载可用，只是不内联） |

`GET /api/s/:token` 返回的 `kind` 同样经过预览体积闸（`too-large`），
与登录态 `GET /api/files/:id/preview` 完全一致 —— 免登录链接不是绕过上限的后门。

### 分片上传

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/uploads/init` | `{file_name, file_size, sha256?, folder_id?}` → `UploadSession`。`folder_id` 为 0/省略即用户根目录；服务端据 id 解析磁盘路径（**只收 id 不收路径字符串**，客户端传不了 `../`），并校验该目录属于上传者本人（否则 403）。同属主 + 同名 + 同大小 + 同 sha 的未完成会话直接复用（断点续传）；超体积 / 扩展名不允许 → 413 / 400，且不产生任何磁盘写入；`upload_enabled=false` → 403 |
| PUT | `/api/uploads/:id/chunks/:idx` | 裸二进制写入第 `idx` 片；幂等覆盖；`idx` 越界 400；累计字节超过声明大小 → 413 |
| GET | `/api/uploads/:id` | 会话状态与已上传分片索引（刷新后续传依据）；会话不存在 / 已清理 → 410 |
| POST | `/api/uploads/:id/complete` | 校验分片齐全且累计字节一致 → 顺序合并 → sha256 → 落最终文件 → 入库 → 返回 `FileItem`；重复调用返回已生成文件（幂等） |
| DELETE | `/api/uploads/:id` | 取消会话，删除分片 |

### 管理端（`/api/admin`，仅管理员）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/admin/users` | `q`（工号或姓名）、`page`、`page_size` → `Paged<User>` |
| POST | `/api/admin/users` | `{employee_no, name, role, password}` → `User`；同时创建 `users/<工号>` 目录。工号只允许字母、数字、`_` `-` `.` |
| PATCH | `/api/admin/users/:id` | `{name?, role?, enabled?}` → `User`；工号不可改 |
| POST | `/api/admin/users/:id/password` | `{new_password}` → `{ok: true}` |
| DELETE | `/api/admin/users/:id` | 先软删该账号全部文件 → 物理清除其文件与磁盘目录 → 再删账号；不能删自己与最后一个管理员 |
| GET | `/api/admin/settings` | `Settings` |
| PUT | `/api/admin/settings` | 部分字段更新 → `Settings`（校验 + 刷新缓存） |
| GET | `/api/admin/files` | `status=active\|trashed\|all`、`owner_id`、`q`、`page`、`page_size` → `Paged<FileItem>` |
| POST | `/api/admin/files/:id/restore` | 恢复 → `FileItem` |
| DELETE | `/api/admin/files/:id` | 立即彻底删除（磁盘 + 行） |
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

---

## 8. 前端能力

| 路由 | 页面 | 说明 |
| --- | --- | --- |
| `/login` | 登录 | 工号 + 密码 |
| `/files` | 按人查看 | **只承载 `?owner=<id>`**（侧栏「全部文件」展开后的用户子 tab 落点）：看某个人的目录，人人可下载 / 预览。**不带 owner 时重定向到 `/files/mine`** —— 「全部人员混合视图」已下线（详见下方「侧栏用户子 tab」）。子 tab 只显示姓名，行尾「⋯」下拉里以**勾选项**切换置顶 |
| （列表形态） | 文件浏览器 | **文件夹与文件在同一个列表里，文件夹排在前面**（Windows 资源管理器的形态；此前子文件夹是列表上方单独的一坨卡片，同一个目录的两类东西被拆在两处、要上下扫两遍）。桌面端表格的「名称」列混合两种行（文件夹行有底色、显示项数而不是体积），移动端卡片同理。行模型是纯函数 `web/src/utils/fileRows.ts`：文件夹只在第 1 页列出（文件由服务端分页、文件夹不是，每页重复渲染会出现「内容重复」的错觉）、按名称本地排序（数字/字母在前、中文在后，不跟服务端文件排序器走）、搜索时按**文件夹名**本地匹配（服务端 `q` 只作用于文件，全藏会让用户进不去命中的目录、全留则会掺入无关目录） |
| （有效期操作） | 设为永久 | 文件行有「设为永久」/「改回有期限」（已永久时）、文件夹行有「有效期」（**递归到目录下所有文件**）。**两者都必须二次确认**，确认框里给出永久空间「已用 / 上限 / 还剩」——需求要求提示永久空间只有 100G，一个写死的数字没用，用户要看到自己还剩多少。到期列的渲染有坑：永久文件后端 `days_left=0`，**直接按 days_left 渲染会显示成「今天到期」**，必须先判 `permanent`（由 `scripts/check-file-list.mjs` 守卫）。后端按到期时间排序时永久文件**固定排最后**（否则升序时 NULL 冒到最前，看着像"马上到期"） |
| `/files/mine` | 我的文件 | 仅自己的文件，可改名 / 删除；**文件夹导航**（面包屑、新建/改名/删除、进入子目录）；上传入口为**整页拖放**（拖到页面任意位置松手即传，**传到当前所在目录**）+ 工具条「上传文件」/「上传文件夹」按钮（移动端无拖拽操作）；**拖入或选择文件夹时按 `webkitGetAsEntry()` / `webkitRelativePath` 还原层级**，逐级建目录（同名复用）后把文件放进各自目录 |
| `/shares` | 分享管理 | **所有人创建的分享都能看到**（带创建者、有效期、查看次数）；可切「只看我的」、复制链接、撤销（自己的或管理员的） |
| `/s/:token` | 分享页（**免登录**） | 不在主框架内；显示文件名/大小/分享者/有效期，图片·PDF·音视频内联，其余下载；目录分享给打包 zip。三种失效状态各有文案：已过期 / 已被删除 / 链接无效 |
| `/preview/:id` | 预览 | **嵌在列表页弹层里的预览面**（`?embed=1`）；也可独立访问（留一个返回入口）。**没有顶部名称/下载栏**，只有外层弹层右上角一个悬浮关闭键（详见表下） |
| `/admin/users` | 用户管理 | 仅管理员 |
| `/admin/settings` | 系统管理 | 上传策略 + 存储一致性检查，仅管理员 |
| `/admin/files` | 全部文件（含回收站） | 仅管理员 |
| `/admin/workbench` | 工作台 | 关键指标 + 到期提醒，仅管理员 |
| `/profile` | 个人设置 | 修改本人密码 |
| `/network-blocked` | 网络不可用 | 接口不可达时展示，文案「无法在此网络下使用，请更换网络再试！」 |

### 侧栏用户子 tab（`layouts/MainLayout.vue`）

「全部文件」展开后按人列子 tab（`?owner=<id>`）：

- **母 tab「全部文件」不可点击导航**：它只是**展开/收起的分组标题**，内部没有
  `<a>`，点它只切换子 tab、不改 URL。
  此前它是「标签导航 + 箭头展开」的双重控件（点文字去看"全部人员"、点箭头展开），
  同一个控件两个动作会让点击预期不确定 —— 想展开的人被带走、想进列表的人
  不知道该点哪。**「全部人员混合视图」已随之整体下线**：它把所有人的文件混在一页，
  既不能上传（后端只允许传到自己目录）、也没有目录导航（跨人同名文件夹没有意义），
  是个信息量低又容易误操作的页面。要看某人就直接点他的子 tab。
  配套：`/files` 不带 owner 时重定向到 `/files/mine`（保住旧书签与预览页返回逻辑，
  不直接删路由）；FileTable 的「上传者」列在该页已无意义（整列都是同一个人）故不显示。
  由 `scripts/check-sidebar.mjs` 的「母 tab 不可导航」一组断言守卫（桌面端）。
- **文案只显示姓名**，不带文件数。侧栏是导航而不是数据看板：带上计数会让每行变长、
  姓名被挤窄，而"谁的文件多"对"点进去找文件"没有帮助。
  （名下没有文件的账号仍然不列出 —— 列出也只会点进空列表；此时分组整个不渲染，
  避免留下一个展开了空无一物的父级。）
- **置顶收在行尾的「⋯」下拉里**，是一个**勾选项**：已置顶时该项显示对勾，
  触发键本身也点亮。不再是一个行内图钉按钮 —— 图钉只能表达一件事，
  而且"点下去是置顶还是取消"只能靠悬停提示区分，触屏上根本没有提示。
- **触发键要贴行尾**：`n-menu` 把 `extra` 放在**标题单元格内部**，默认只会紧跟在
  姓名后面 —— 实测 208px 侧栏里落在 x≈64~83，而标题区一直铺到 x=190，
  看着像"粘在名字上"，不像行尾的操作入口。改法是把标题单元格改成 flex、
  让 `extra` 用 `margin-left: auto` 吃掉剩余空间，**只作用于挂了 `owner-tab`
  类的行**（类名由 `:node-props` 按选项上的 `ownerTab` 标记下发，规则在
  `web/src/styles.css`）。桌面侧栏与移动端抽屉共用同一条规则（两处都验）。
- **图标用实心三点**（`EllipsisHorizontal`），不用 Outline 细线版：18px 下细线三点
  几乎看不清，用户认不出这是"更多"；实心三点是这类菜单的通用形状。
  触发键是 28px 的 `button`（比 18px 图标大，移动端好点），hover/focus 画出
  底色与描边，`pinned` 时点亮为品牌色。
- 触发键要 `stopPropagation`：否则点它会被 `n-menu` 当成"选中该项"而触发导航。
- **勾选态必须画在 `label` 里**：`extra` 是 **`n-menu` 专有**字段，`n-dropdown`
  根本不读它 —— 写在 `extra` 上的对勾会**静默丢失**（不报错、类型检查也通过）。
- 同一机制的另一面：**侧栏折叠成 64px 图标栏后，子项走的是 `n-dropdown` 弹层，
  那里连「⋯」触发键本身都不渲染**（该弹层只列姓名链接，置顶入口只在展开态可用）。
  这是既有取舍、不是回归；改这块时别误以为折叠态也该有触发键。
- 以上各条由 `scripts/check-sidebar.mjs` 守卫（桌面侧栏 + 移动端抽屉两处都量）。

### 上传进度浮窗（全局，不属于某个路由）

登录后的所有页面右下角都有上传进度浮窗（`components/UploadPanel.vue`，
挂在 `layouts/MainLayout.vue` 上）：

- **队列状态是全局单例**（`stores/upload.ts` 的 `uploadQueueState`），上传逻辑在
  `utils/upload.ts` 的 `UploadManager` 里。队列不再由「我的文件」页面持有 ——
  组件一卸载队列就从界面上消失，用户切页后既看不到进度也没法取消。
- **逐文件进度**：每个任务一行，显示文件名（含相对目录）、状态、已传/总字节与进度条；
  顶部另有一条按**字节**折算的总体进度（不是按任务数，否则传大文件时会虚高）。
- **跨页面常驻**：上传中切到任何页面，浮窗继续刷新；传完的提示也由浮窗发出
  （只有它常驻，挂在页面里会随卸载一起消失）。
- **空队列不渲染**；全部传完后自动收起为一行（结果仍可展开回看），
  但**有失败时保持展开** —— 失败不能被自动折叠藏起来。
- 折叠状态记在 `localStorage`（`lanfs-upload-panel-collapsed`），刷新后保持。

### 预览弹层（全局，不属于某个路由）

点列表里的「预览」**不再 `window.open` 新标签**，而是在当前页弹出铺满视口的弹层
（`components/PreviewModal.vue`，与上传浮窗一样挂在 `layouts/MainLayout.vue` 上）：

- **弹层里没有标题栏**：文件名、类型/大小标签、下载按钮那条顶部栏已按要求删除，
  只保留右上角一个悬浮关闭键（Esc 也可关闭）。
- **弹层内部是 iframe**，`src="/preview/<id>?embed=1"`。预览状态是全局单例
  （`stores/preview.ts` 的 `previewState`），入口有 `useFileActions`（表格/移动卡片）
  与上传队列的「预览」，都只调 `openPreview(id)`——状态放页面里就等于把预览逻辑复制多份，
  任意一处忘记挂弹层就点不开。
- iframe 用**同源真实路由**而不是 `srcdoc` + `Teleport`：`srcdoc` 里手工搬 Naive UI 的
  样式试过，内容能渲染，但 Teleport 出去的部分拿不到 Naive 的 CSS 变量作用域，
  深色档下提示文字仍是黑色；真同源文档自己跑一遍 SPA 启动，Naive 与明暗主题天然生效。
- `?embed=1` 让预览页知道「自己被嵌着」，据此**不渲染返回键等自身 chrome**、铺满不留内边距；
  直接访问 `/preview/:id` 时保留一个左上角悬浮返回键，避免用户困住。
- **深色适配**：预览专用令牌在 `styles.css`（`--color-preview-stage` / `-paper` /
  `-code-bg` / `-code-border`，明暗两档都覆盖）。约定是
  「**纸张是内容、舞台是 UI**」：docx / pptx 库渲染出的白纸保持白色（那是文档自身样式），
  但纸张外的舞台底色、滚动区、边框必须走令牌，深色档下不能出现一块刺眼的浅灰底。
  注意 docx-preview 的类名由 `renderAsync` 的 `className` 派生
  （容器 `<className>-wrapper`、纸张 `section.<className>`），此前按默认的 `.docx-wrapper`
  写覆盖，**从未生效**。

### 预览观感统一（所有类型共用一套骨架）

**问题**：观感不统一是**逐类型累积**出来的 —— 每个预览器当初各自决定了舞台底色、
内边距、圆角与「谁来滚动」，单看任何一个都合理，摆在一起才割裂：

| 类型 | 改动前的舞台 | 问题 |
| --- | --- | --- |
| docx | 自绘浅灰 `#e9edf3` | 与 xlsx/text 的全白不一致 |
| pptx | 自绘深灰 `#3d4148` | 浅色档下是一块深灰，最刺眼 |
| xlsx / text | 自绘全白 | 与 docx/pptx 不一致 |
| pdf | 无舞台 | 唯一没有统一骨架的类型 |
| image / 音视频 | 又一套（媒体还用了第三块深灰底） | 各写各的 |

**统一契约**（改 `PreviewView` 一处即改所有类型）：

- 舞台唯一：所有可渲染类型都包在 `.preview-stage` 里，底色只用
  `--color-preview-stage`；**预览组件不许自带舞台底色**（只画「内容面」）。
- 留白唯一：`padding: var(--preview-gutter) var(--preview-pad) var(--preview-pad)`。
  顶部更大是给悬浮的返回键/关闭键让位，否则会盖住内容自己的右上角控件
  （文本预览的「复制全部」被盖过就是先例）。
- 滚动唯一：各类型在**自己的内容面内部**滚，外层舞台不滚 —— 否则滚动时
  纸张/卡片会跟内容一起滑走，视觉上「框没了」。
- **禁用 `calc(100dvh - 魔数)`**：高度交给统一骨架，各预览自己撑满即可。
  魔数是各类型留白不一致的直接来源。
- 库的硬编码底要盖掉：`pptx-preview` 会把 wrapper 写成 `background:#000`，
  不覆盖就出现「双重舞台」（我们的舞台外再套一圈黑底）。

**守卫**：
- `scripts/check-ui-consistency.mjs`（静态，进 `npm test`）：预览组件不许出现
  `--color-preview-stage` 与 `calc(100dvh - …)`，且 `PreviewView` 必须存在
  用统一令牌的 `.preview-stage`。检查前会剥掉 CSS 注释，避免说明文字被误判。
- `scripts/check-preview.mjs`（真实浏览器）：把 docx/xlsx/png/pdf/text **真的渲染一遍**
  （mock 对这些扩展名回真实字节），断言各类型的舞台底色与留白**完全一致**、
  内容面被撑满、且 390px 窄屏下 pptx 不被裁。

前端环境变量（构建期注入）：

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `HOST` | 空 | 后端域名（仅 origin，不含 `/api`），构建期注入；留空走同源 `/api` |
| `VITE_API_PROBE_TIMEOUT` | `6000` | 连通性探测超时（毫秒） |
| `VITE_DEV_API_TARGET` | `http://127.0.0.1:8080` | 仅开发态 vite 代理目标 |

容器部署时还有**运行时**变量（不重新构建镜像即可切换内网地址）：

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `LANDRIVE_API_BASE_URL` | 构建期值 | 容器启动时写入 `/config.js`，优先级高于构建期 `HOST` |
| `LANDRIVE_API_PROBE_TIMEOUT` | `6000` | 运行时覆盖探测超时（毫秒） |

API 地址解析优先级：**运行时 `/config.js` → 构建期 `HOST`（拼成 `${HOST}/api`）→ 同源 `/api`**，
实现只在 `web/src/api/index.ts` 的 `resolveApiBaseUrl`。

连通性探测：登录页与路由守卫调用 `GET /api/health`，用它区分
「网络不可达」与「账号密码错误」。

预览矩阵（**纯前端**，通过 `/api/files/:id/content` 取 blob 后本地渲染，渲染在预览弹层的 iframe 内）：

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
| 超过 `preview_max_size_mb`（默认 20MB） | `kind=too-large`：提示「超过在线预览上限，请下载后查看」，**前端不去取内容**，下载仍可用 |

预览体积闸的口径（详见 §7 的 `preview_max_size_mb`）：

- 判定函数只有一个 —— `storage.PreviewKindFor(ext, sizeBytes, maxBytes)`，
  **先判体积再判格式**：超限文件无论什么格式一律 `too-large`（对超大文件来说
  "请下载"就是结论，不必再劝用户换格式）。
- 登录预览与免登录分享**共用**它，两个 `/content` 接口也各自再判一次并降级为附件：
  只在 `/preview` 的返回值里写 `kind` 是不够的 —— 用户可以直接请求 `/content`
  （或拿一条旧链接），那样"限制"就只是一句提示，浏览器仍会去渲染超大文件。
- 前端拿到 `too-large` 后**不请求内容**（这正是这道闸要防的事）：`PreviewView.vue`
  把它与 `unsupported` / `legacy-office` 一样走"不下载、只提示 + 下载入口"的分支。
- 上限配成 `0` 表示**不限制**（不是"一律不给预览"）：`Values.PreviewMaxSizeBytes() <= 0`
  即放行（判定集中在 `Values.PreviewSizeAllowed`），`TestPreviewLimitZeroMeansUnlimited`
  守住这条语义。

依赖体积控制：预览器一律 `defineAsyncComponent` + 动态 `import()`，主包不引入任何预览库。

---

## 9. 安全与健壮性

- 路径只来自数据库，绝不直接拼接用户输入；`storage` 层校验全部相对路径。
- 扩展名策略在 `init` 与 `complete` 双重校验；以真实扩展名为准，大小写不敏感。
- 未知类型强制 `application/octet-stream` + `Content-Disposition: attachment`；服务端不执行任何存储内容。
- 密码 bcrypt（cost 10）；登录失败限流；修改密码后旧令牌因 `pwd_ver` 声明不匹配而失效。
- 审计日志功能已下线（不记录、不展示）。
- 数据库连接失败时启动重试 60 秒，超时给出 DSN 提示（适配容器启动顺序）。
