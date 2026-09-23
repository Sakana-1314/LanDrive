# Todo — 局域网文件助手

实施进度台账。规划与完成事件都追加到 `## Log`（最新在最上方）。

## Now

（全部完成 —— 代码、镜像、文档站均已发布；CI 全绿）

## Next

（无）

## Later

（无）

## Log

- **2026-09-23 P15 修复公开镜像泄露内网域名（并改为默认同源 /api）**
  `build-images.yml` 一直用 `--build-arg HOST=${{ vars.API_HOST }}` 把真实后端域名烘进前端产物，
  而 web 镜像推到**公开** ghcr，任何人都能解包读取。逐层下载镜像验证后确认泄露：
  第 10 层 `/usr/share/nginx/html/assets/index-*.js` 中可读到
  `oe(I().apiBaseUrl,"https://<真实内网域名>`。

  修复：
  1. 构建**不再传 HOST**，前端走同源 `/api`（与网页端自己的域名一致）——
     与 P14 的反代设计一致：不跨域、免 CORS，镜像与环境解耦，换地址只改运行时变量。
  2. 清除仓库内真实域名，示例统一 `example.com`（仓库源码与文档同样公开）。
  3. 新增 `web/scripts/check-no-internal-host.sh`：扫描会进入产物的构建期输入
     （`.env*` / `vite.config.ts` / `src`），拦截 private 域与内网网段；
     **排除测试文件**（`base-url.spec.ts` 里的 `192.168.1.100` 是验证跨域判定的样例，
     第一版守卫把它误报成泄露 —— 守卫一旦误报就会被绕过）。
     已接入 `build-images.yml` 构建前校验与 `npm run test:entrypoint`。
  4. AGENTS.md 改为硬规则：web 镜像不得注入 HOST；仓库内示例一律占位地址。

  验证：守卫对三种注入（src 写内网 IP、.env.production 写真实域名、vite.config.ts 默认值）
  均能拦截；重建镜像后逐层解包复查，内网域名命中 **0** 个文件，
  产物注入值为 `apiBaseUrl,""`（回退同源 `/api`）。
  遗留：仓库变量 `API_HOST` 已无人引用（仍存有真实域名），待确认是否删除。

- **2026-09-23 P14 前端镜像支持 /api 同源反代**
  新增 `LANDRIVE_API_PROXY=主机:端口`：前端容器内的 nginx 把同源 `/api` 转发到后端容器。
  浏览器只访问一个域名 → 不跨域、不需要 CORS，后端地址也不暴露。compose 已默认开启
  （`api:8080`），1Panel 只需为网页端配一个域名，`LANDRIVE_CORS_ALLOW` 可留空。

  实测踩到并修掉的问题（已写进 AGENTS.md 与回归测试）：
  1. nginx 不允许同一 location 里既 include `proxy_pass` 又写 `return` →
     把 `location /api/` 整体外置到 `proxy.d/api.conf`，由入口脚本生成。
  2. 未反代时 `/api` 落到 SPA 回退返回 200 + HTML → 健康探测误判为"已连通"，
     用户登录才失败。改为显式返回 JSON 404。
  3. `proxy_pass` 直接写主机名时 nginx 启动即解析，后端未就绪会
     `[emerg] host not found in upstream` 让**整个前端起不来** →
     改用变量 + `resolver`，把解析推迟到请求时。
  4. `grep` 按行校验 → 含**换行**的取值绕过校验并注入 nginx 指令 → 改用 `case` 整串匹配；
     另拒绝空主机名（`:8080`）、空端口（`api:`）、非数字/越界端口、路径与空格。
  5. 启用反代但前端仍用绝对地址 → 反代形同虚设。同时把运行时 `apiBaseUrl` 写成 `/api`。
  6. `COPY . .` 会把本机 node_modules/dist 带进镜像 → 新增 `web/.dockerignore`。

  前端侧把 API 基址解析与健康探测判定抽成纯函数模块（`base-url.ts` / `health.ts`），
  修掉 `probeHealth` 把 200 + HTML 当成已连通的误判。

  验证：真实 nginx 1.26 + Go 后端实测 —— /api 前缀与查询串保留、`Authorization` 与
  `X-Real-IP`/`X-Forwarded-For`/`X-Forwarded-Proto` 透传、20MB 与 200MB 上传字节数一致、
  5MB 下载完整、后端不可达返回 502 且首页仍可用、双层反代（1Panel → 前端 → 后端）正常、
  上游不存在时 `nginx -t` 仍通过。新增 `web/scripts/test-entrypoint.sh`（32 项断言），
  接入 `npm run test:entrypoint` / `make check` / CI。

  CI 两次失败均暴露了**测试自身的缺陷**，已修：
  - 测试把 nginx.conf 复制到临时目录时 include 模式写成 `*.conf`，而实际是 `api.conf`，
    替换静默失败；本机因手工建过该文件而"假通过"，CI 上才暴露。已改正模式并加自检
    （替换失败即判失败），且验证对两种注入变异都能报错。
  - 构建产物断言按字面量 `https://localhost/api` 判断 HOST 注入；重构后 `${HOST}/api`
    在运行时拼接，产物只留 `https://localhost`，行为未变但断言误报。改为断言注入值存在，
    拼接行为由 `base-url.spec.ts` 覆盖。

- **2026-09-23 P13 仓库改名为 LanDrive**
  仓库由 `Sakana-1314/lan-drive` 重命名为 **`Sakana-1314/LanDrive`**。
  因为 **GitHub Pages 路径区分大小写**，站点 base 与线上地址必须与仓库名完全一致，
  否则静态资源全部 404。同步了全部大小写敏感引用：
  - `docs/websites/.vitepress/config.ts`：`base` 改为 `/LanDrive/` + socialLinks
  - `.github/workflows/website.yml`：站点地址与链接校验前缀
  - `README.md` 徽章与仓库链接；文档站 `index.md` / `deploy.md` 的链接与
    `git clone` 目录名（`cd LanDrive`）
  - `AGENTS.md`：补充「改名必须同步的四处」清单

  **刻意保持小写**（Docker 与 ghcr 不接受大写，已在注释中说明）：
  ghcr 镜像路径 `ghcr.io/sakana-1314/lan-drive`、compose 的 `name` 与
  `container_name`、本地镜像 tag。同时更新了本地 git remote。

  验证：新地址 <https://sakana-1314.github.io/LanDrive/> 16 个页面全部 200、
  资源路径为 `/LanDrive/assets/...`；旧小写地址已 404（符合预期）；
  ghcr 两个镜像仍可匿名拉取（镜像路径未变）；两个徽章均 passing；
  分支保护（`测试通过`，strict）、仓库变量 `API_HOST`、Pages 配置在改名后均保留。

- **2026-09-23 P12 修复构建/测试报错、精简 README、新增 VitePress 文档站**
  1. **修复构建与测试报错（全新克隆必现）**：`make check` / `make test` 里的 `npx vue-tsc`
     在**未安装依赖**时会去远端拉取任意版本的 vue-tsc，与项目锁定的 typescript 不兼容，
     报 `ERR_PACKAGE_PATH_NOT_EXPORTED: Package subpath './lib/tsc'` 而失败。
     用全新 `git clone` 复现后，改为「依赖 `webinstall` + `npm run typecheck`」走本地依赖。
     顺带发现本环境 `NODE_ENV=production` 会让 npm 跳过 devDependencies（VitePress 装不上），
     已在 Makefile / 文档 / 工作流中统一补 `--include=dev`。
  2. **README 精简**：只保留功能介绍与文档站入口，删掉实际用途描述与技术细节。
  3. **新增 `docs/websites/` 文档站（VitePress）**：16 个页面 —— 入门（这是什么/功能一览/
     部署/前后端分离部署/常见问题）、使用教程（登录/浏览与下载/上传/在线预览/个人设置）、
     管理员（统计看板/用户/设置/文件与回收站/日志）。面向使用者、口语化，部署拓扑与文件
     生命周期用 mermaid 画。新增 `make docs` / `docsdev` / `docsinstall`。
  4. **新增 `website.yml`**：PR 只构建校验（含 1318 个链接/资源的硬校验），push `main`
     构建后发布到 `gh-pages`，由 GitHub Pages 提供服务。已启用 Pages。
  5. **修正仓库名大小写**：GitHub 建仓时把仓库名存成了 `LanDrive`，而 GitHub Pages 路径
     **区分大小写**，会导致站点 base 与线上地址 404。已把仓库重命名为 `lan-drive`，
     仓库内全部引用（VitePress `base`、badges、文档链接、工作流校验前缀）统一为小写，
     AGENTS.md 增加「base 必须与仓库路径大小写一致」的规则。
  6. **验证**：文档站已上线 <https://sakana-1314.github.io/lan-drive/>，
     16 个页面全部 200，静态资源与本地搜索索引（112 条文档）正常；
     `测试通过` 与 `构建并发布文档站` 在 main 上全绿；全新克隆 `make check` 通过。

- **2026-09-23 P11 发布到 GitHub 并接入 CI/CD**
  1. **命名**：仓库定为 **lan-drive**（镜像 `ghcr.io/sakana-1314/lan-drive`），
     已建公开仓库并推送：<https://github.com/Sakana-1314/lan-drive>。
  2. **前端容器化**：新增 `web/Dockerfile`（node 构建 → nginx 运行）+ `web/nginx.conf`
     （SPA 回退、`/config.js` 禁缓存、assets 长缓存）+ `docker-entrypoint.d/40-lan-drive-config.sh`。
     关键设计：**运行时注入 API 地址**——镜像保持"地址无关"，同一 tag 可部署到任意环境，
     换内网地址只需 `LANDRIVE_API_BASE_URL` 重启容器，无需重新构建。
     前端地址解析优先级：运行时 `/config.js` → 构建期后端域名 → 同源 `/api`（原用 `VITE_API_BASE_URL`，P12 起改为 `HOST`）。
  3. **自动测试**（`test.yml`）：变更路径检测 + 后端起 MySQL 8.0 service 跑单元与集成测试 +
     前端 typecheck/网络判定单测/生产构建（并校验产物含固定提示文案）；汇总 job `测试通过`。
  4. **自动构建镜像**（`build-images.yml`）：推送 ghcr，**固定 tag `:server` / `:web`**，
     另打时间戳 tag 便于回滚；按变更路径只构建改动的镜像，手动触发时两个都构建。
  5. **AGENTS.md**：参照 `御坂学习v4` 的规范结构编写，覆盖项目结构、提交规范、
     通用代码规范、server/web 分端约定、CI/CD 与安全红线。
  6. **踩到并修复的两个真实问题**：
     - **ghcr 镜像名必须全小写**：`github.repository_owner` 是 `Sakana-1314`（含大写），
       首次构建报 `repository name must be lowercase`；改为小写常量并加了一步前置校验。
     - **必需检查工作流不能按路径过滤**：`测试通过` 是分支保护必需检查，原先 `on:` 带
       `paths` 过滤，导致只改 docs/README 的 PR 完全不触发工作流，必需检查永不回报，
       PR 永久卡在 `BLOCKED`（已用 PR #1 复现）。改为「总是触发 + job 内按目录跳过」，
       并在 AGENTS.md 写明该规则；验证后 `BLOCKED → CLEAN` 正常合并。
  7. **验证**：`测试通过` 与 `构建并推送镜像` 在 main 上全绿；两个镜像匿名可拉取
     （server 36.5 MB / web 26.1 MB）；分支保护已启用（必需检查 `测试通过`）；
     仓库已打 topics 并配置描述。

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
# PR 门禁验证
