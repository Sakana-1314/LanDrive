# Todo — 局域网文件助手

实施进度台账。规划与完成事件都追加到 `## Log`（最新在最上方）。

## Now

（全部完成 —— 前后端分离重构完成；内网 API 冒烟 51/51 通过、集成测试与单元测试全绿）

## Next

（无）

## Later

（无）

## Log

- **2026-09-23 P10 架构调整：改为 server / web 前后端分离（按用户要求）**
  用户指出目录设计有误：应分为 `server`、`web` 两个目录，前后端分离；**前端部署在公网，接口在内网**；
  且接口访问不通时必须提示「无法在此网络下使用，请更换网络再试！」。改动如下：

  1. **目录重构**：Go 代码全部移入 `server/`（含 `cmd`/`internal`/`go.mod`/`Dockerfile`），前端保持 `web/`；
     删除了 `internal/webui` 与 `docker/` 目录，二进制不再内嵌前端（体积 18M → 15M）。
  2. **后端改为纯内网 API**：不再托管页面；`/api/**` 之外一律返回明确提示；
     重写 CORS 中间件——预检请求在路由前直接返回 204、放行 `Authorization` 头、
     暴露 `Content-Disposition`（让前端能读到下载文件名）、不使用 Cookie 凭据、
     白名单为空即不放行任何跨域。
  3. **前端适配公网部署**：新增 `VITE_API_BASE_URL`（构建期注入内网地址）与 `VITE_API_PROBE_TIMEOUT`；
     新增 `src/api/network.ts` 统一判定「请求未到达服务器」（超时/连接失败/被 CORS 拦截），
     固定文案 `NETWORK_UNAVAILABLE_MESSAGE = '无法在此网络下使用，请更换网络再试！'`；
     新增 `/network-blocked` 页面，登录页进入即探测 `/api/health` 并在不可达时给出提示。
  4. **部署与文档**：`docker-compose.yml` 改为「内网 API + MySQL」（数据库不暴露端口）；
     新增 `server/Dockerfile`（仅含 API）与 `web/README.md`、`web/.env.example`；
     README 重写为三步部署（部署内网 API → 构建前端到公网 → 配置 CORS 白名单）并附网络排查表。
  5. **验证**：冒烟测试扩充到 **51 项**（新增 CORS 预检与「只提供接口」校验），全部通过；
     新增前端网络判定单元测试（`npm test`）接入 `make check`；
     实测白名单内 Origin 放行、白名单外不返回 `Allow-Origin`、内网不可达判定为网络问题、
     401 仍按业务错误处理；跨域登录/上传/下载全链路验证通过。
  6. 顺带修复：纯中文文件名的 `Content-Disposition` 回退值曾被压成 `.txt`（只剩扩展名），
     现在是 `download.txt`，并补充了 4 组文件名回归测试。

- **2026-09-23 P9 真实运行验证（关键阶段）**：用内嵌 MySQL 兼容引擎把二进制**真正跑起来**做端到端验证，发现并修复 3 个只有运行才能暴露的真实缺陷：
  1. **SPA 全部返回 404**：gin 在 NoRoute 分支前已把响应状态预置为 404，`webui.Serve` 未覆盖，导致整个前端（含 assets 静态资源）无法加载，页面完全打不开。（P10 已随架构调整移除该代码路径）
  2. **用户缓存被污染**：登录处理器把「已放入 UserCache 的同一 `*User` 指针」原地清空密码，使缓存中的用户丢失密码哈希，后续所有请求的令牌指纹校验失败（表现为创建用户后立即提示"密码已变更，请重新登录"）。
  3. **上传完成响应缺展示字段**：`complete` 直接返回数据库原始行，前端拿到 `days_left=0`、`can_edit=false`，上传进度与「我的文件」显示错误。

  另外修复：一致性扫描把上传中的分片误报为孤儿文件、上传总开关误用 400（契约规定 403）、MySQL 8.0.20+ 已弃用的 `VALUES()` 写法、外键错误仅按英文文案判定（改为按错误码 1216/1217/1451/1452）。

- **2026-09-23 P9 真实运行验证（关键阶段）**：用内嵌 MySQL 兼容引擎把二进制**真正跑起来**做端到端验证，发现并修复 3 个只有运行才能暴露的真实缺陷：

  1. **SPA 全部返回 404**：gin 在 NoRoute 分支前已把响应状态预置为 404，`webui.Serve` 未覆盖，导致整个前端（含 assets 静态资源）无法加载，页面完全打不开。
  2. **用户缓存被污染**：登录处理器把「已放入 UserCache 的同一 `*User` 指针」原地清空密码，使缓存中的用户丢失密码哈希，后续所有请求的令牌指纹校验失败（表现为创建用户后立即提示"密码已变更，请重新登录"）。
  3. **上传完成响应缺展示字段**：`complete` 直接返回数据库原始行，前端拿到 `days_left=0`、`can_edit=false`，上传进度与「我的文件」显示错误。

  另外修复：一致性扫描把上传中的分片误报为孤儿文件、上传总开关误用 400（契约规定 403）、MySQL 8.0.20+ 已弃用的 `VALUES()` 写法、外键错误仅按英文文案判定（改为按错误码 1216/1217/1451/1452）。

  验证结果：部署冒烟 **48/48 通过**；store 与 maintain 集成测试全绿（含「15 天到期 → 7 天回收站 → 彻底删除磁盘文件与数据库记录」的完整生命周期）；单元测试全绿；前端 `vite build` 与 `vue-tsc` 干净；迁移 SQL 与 51 条真实 SQL 语句在 MySQL 引擎上执行通过，外键 RESTRICT/CASCADE 行为符合预期。

  同时清理了未接线的死代码（`readLimited` / `parseIDList` / `Storage.Stat` / `CountUploadSessions` / `OrphanBytesUnknown`）与空生命周期钩子。

- 2026-09-23 P8：离线校验体系 —— 单元测试（storage 路径安全、settings 配置校验、auth 令牌与限流、upload 分片数学、files 权限判定、config）、数据库集成测试（store 仓储 + maintain 生命周期，未设置 DSN 时自动跳过）、部署冒烟脚本 `scripts/smoke.sh`（48 项断言）。

- 2026-09-23 P7：部署与文档 —— `docker/Dockerfile.app`（三段构建）、`docker-compose.yml`、`Makefile`、`README.md`、`.gitignore` / `.dockerignore`、`scripts/smoke.sh`；接口契约冻结于 `docs/design.md`。

- 2026-09-23 P6：管理端页面（统计看板 / 用户管理 / 全部文件与回收站 / 审计日志 / 系统配置）+ 个人设置页 + 路由守卫与构建配置。

- 2026-09-23 P4–P5：前端骨架与核心页面 —— 登录、主框架布局、全部文件（左目录树 + 表格）、我的文件、上传（分片断点续传）、预览（docx/xlsx/pptx/pdf/图片/文本/音视频）、API 客户端、上传引擎、路由与守卫。

- 2026-09-23 P3：生命周期与运维落地于 `internal/maintain` —— 到期标记、物理清理（先删盘后删行）、僵尸上传会话清理（连分片目录）、审计日志裁剪、孤儿文件归档；`GET_LOCK` 保证多副本只跑一份。

- 2026-09-23 P2：文件与上传落地 —— `internal/storage`（相对路径规范 / 安全拼接 / 落盘 / 合并 / 哈希 / 遍历）、`internal/upload`（init / 写分片 / 状态 / complete / cancel，幂等与 CAS）、`internal/files`（列表查询、权限判定、改名、软删除、恢复、彻底删除、所有者聚合）。

- 2026-09-23 P1：后端骨架落地 —— `go.mod`、`internal/config`（环境变量 + 校验）、`internal/model`（领域对象与枚举）、`internal/store`（连接 / DSN 补参 / 自动建库 / 嵌入式迁移 / 账号·文件·上传·配置·日志·统计仓储）、`internal/auth`（bcrypt / JWT / 限流 / 用户缓存）、`internal/settings`（配置读写校验与缓存）、`internal/webui`（go:embed SPA 托管）。

- 2026-09-23 计划批准并冻结接口契约：写入 `docs/design.md`（数据模型、磁盘布局、权限矩阵、生命周期、全部 HTTP 接口形状、分片协议、预览矩阵、环境变量）。

- 2026-09-23 环境勘察：工作区为空，无 Go / MySQL / Docker；Go 工具链与依赖在 `/root/go/pkg/mod` 有缓存，网络可达；参照同工作区 `分布式代理`、`备件本地管理` 的工程惯例。

- 2026-09-23 需求确认（用户选择）：只交付代码 + docker-compose，不做本机安装；但允许离线 `go build/vet/test` 与 `npm install && build` 做编译校验。到期语义为「15 天标记删除 + 再保留 7 天」。Office 预览必须纯前端实现。登录用「工号 + 密码」，需要分片断点续传。
