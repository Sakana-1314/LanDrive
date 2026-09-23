# AGENTS.md

局域网文件助手（LanDrive）仓库的智能体开发规范。所有改动须遵循本规范。

## 1. 项目结构

| 目录 | 说明 |
|---|---|
| `server/` | Go + Gin 内网 API（单一二进制，只提供 `/api/**`，**部署在内网**） |
| `web/` | Vue 3 + TS + Vite + Naive UI 前端（**部署在公网**静态托管） |
| `.github/workflows/` | CI/CD 工作流 |
| `docs/design.md` | 设计与接口契约，**实现以它为准** |
| `docs/websites/` | 文档站（VitePress），面向使用者的教程；`pages/` 是内容 |
| `docs/scripts/smoke.sh` | 部署后端到端冒烟脚本 |

**部署拓扑是硬约束**：前端在公网、API 在内网，两者跨域通信。任何改动都不得破坏这条边界，
也不得让后端承担托管页面的职责（`server` 不内嵌前端产物）。

## 2. 协作与提交规范

- **作者身份**：所有提交（author 与 committer）统一为 `Sakana <admin@yangrucheng.top>`。
- **分支命名**：`feat/`、`fix/`、`refactor/`、`docs/`、`chore/` 前缀。
- **提交信息**：中文，格式 `type(scope): 描述`，如 `feat(server): 新增存储一致性扫描`；scope 用 `server` / `web` / `ci` / `docs`。
- **工作流命名**：所有 `name` 用中文（专有名词除外），job 的 `name` 同理。
- **合并门禁**：`测试通过`（`test.yml` 的 `test-summary`）为必需检查；未全绿不得合并。
- **不改契约**：改接口必须同步 `docs/design.md` 与 `web/src/api/types.ts`，三者形状保持一致（后端 JSON 字段 → 文档表格 → 前端类型）。

## 3. 通用代码规范

- **注释用中文**，写**为什么**而不是重复代码在做什么；不做无谓抽象，只调用一两次的逻辑直接平铺。
- **命名**：Go 标识符按 Go 惯例（导出用 `CamelCase`，包内用 `camelCase`）；前端变量/函数 `camelCase`、类型 `PascalCase`、常量 `UPPER_SNAKE`。
- **错误处理**：Go 用 `%w` 包装 + `errors.Is` 判定，领域错误集中在各包的 `var ErrXxx = errors.New("中文消息")`，由 `handler.failErr` 统一映射 HTTP 状态码；前端所有面向用户的提示用中文。
- **前端提示文案**：`无法在此网络下使用，请更换网络再试！` 是**需求指定的固定文案**，定义在 `web/src/api/network.ts` 的 `NETWORK_UNAVAILABLE_MESSAGE`，禁止改写、禁止在别处重复字面量。

## 4. server（Go + Gin）

- **布局**：`cmd/landrive/main.go`（配置、依赖组装、迁移播种、维护任务调度、优雅关闭）+ `internal/{config,model,store,auth,settings,storage,upload,files,maintain,handler,router}`。跨包依赖单向：`handler → {files,upload,maintain,settings,store,model}`，`storage` 与 `settings` 不反向依赖上层。
- **配置**：全部走环境变量并在 `internal/config/config.go` 的 `Load()` 内校验，变量名前缀 `LANDRIVE_`；必填项缺失必须启动失败并给出可操作的提示。新增可调项同步 README 配置表。
- **数据库**：`internal/store` 独占数据访问，结构变更只改 `internal/store/migrations/NNNN_*.sql`（**不提交手工迁移脚本**，启动时自动执行、幂等）。DDL 只使用 MySQL 8.0 与 MariaDB 11 都支持的标准语法，不用 JSON 列 / 函数索引 / CHECK / CTE 写入；时间列一律 UTC。
- **存储路径**：数据库中**只保存相对数据根目录的路径**（`/` 分隔）。任何路径在使用前必须经 `storage.SafeRel` 校验；磁盘文件名只由主键 + 规范化扩展名生成，原始文件名只进数据库。
- **权限**：属主校验集中在 `internal/files`（`checkCanModify`）与 handler；`trashed` 文件对普通用户（含属主）完全不可见，只有管理员能看回收站。新增接口必须明确落在 `/api/**` 的公开 / 登录 / 管理员三档之一。
- **并发与幂等**：分片写入按 `(upload_id, idx)` 幂等覆盖；`complete` 用 CAS（`UPDATE ... WHERE status='uploading'`）保证只成功一次，重复调用返回同一文件（靠 `upload_sessions.file_id`）。
- **维护任务**：新增定时任务挂到 `internal/maintain`，并确保经 `withLock`（数据库 `GET_LOCK`）执行，保证多副本部署只跑一份。
- **安全**：只有图片/PDF/音视频内联展示（`storage.IsInlinePreviewable`），其余一律附件下发；`.svg`/`.html`/`.js` 绝不内联。CORS 白名单为空即不放行任何跨域。
- **代码质量**：`gofmt` / `go vet` / `go test ./...` 全绿；有真实 MySQL 时跑 `LANDRIVE_TEST_MYSQL_DSN=... go test ./internal/store/ ./internal/maintain/`。**测试失败要查清是代码问题还是测试引擎限制，不得为了让测试变绿而放宽断言。**

## 5. web（Vue 3 + TS + Vite + Naive UI）

- **技术栈固定**：Vue 3 + TypeScript + Vite + Vue Router + Naive UI；状态用 `reactive` 单例（`src/stores/`），**不引入 Pinia**；时间筛选与图标不引入 date-fns / 额外图标库（用 `@vicons/ionicons5`）。
- **接口层单点**：所有请求经 `src/api/index.ts` 的 axios 实例；类型定义在 `src/api/types.ts` 并与 `docs/design.md` 同步。**禁止在组件里直接 `fetch`/`axios`。**
- **API 地址**：优先级为「运行时 `/config.js` → 构建期 `HOST`（`__API_HOST__`，拼成 `${HOST}/api`）→ 同源 `/api`」，解析逻辑只在 `src/api/index.ts` 的 `resolveApiBaseUrl` 里。`HOST` 是**构建期**环境变量（仅 origin，不含 `/api`），与同组织其它前端项目保持一致；容器部署还可用运行时 `LANDRIVE_API_BASE_URL` 覆盖，**不要为了换地址重新构建镜像**。
- **网络判定**：区分「请求未到达服务器」（超时/连接失败/被 CORS 拦截 → 提示更换网络）与「有响应但业务失败」（按业务提示）。判定逻辑只在 `src/api/network.ts`。**绝不能把网络不可达显示成"密码错误"。**
- **上传**：走 `src/utils/upload.ts` 的 `UploadManager`（分片并发、断点续传、重试）；分片大小必须服从服务端 `init` 返回的 `chunk_size`，前端不得自行决定。
- **预览**：预览库一律 `defineAsyncComponent` + 动态 `import()`，**主包不得引入 docx-preview / exceljs / pptx-preview**；新增预览类型时同步 `previewKind`（server）与预览矩阵表（`docs/design.md`）。
- **样式**：用 Naive UI 组件与 `n-space`/`n-card` 布局，避免自写大段 CSS；主题令牌集中在 `src/utils/theme.ts`。
- **代码质量**：`npm run typecheck`（vue-tsc）、`npm test`（网络判定单测）、`npm run build` 全绿。

## 6. docs（文档站）

- **技术栈**：VitePress（源码 `docs/websites/pages/`，构建产物 `docs/websites/.vitepress/dist/`）。
- **面向使用者**：文档站只讲**怎么部署、怎么用**，不写实现细节（那些放 `docs/design.md`）。语言要口语化、面向非开发同事。
- **`base` 必须与仓库路径大小写完全一致**（当前为 `/lan-drive/`）。GitHub Pages 的路径**区分大小写**，仓库改名（尤其大小写）会让线上资源全部 404，必须同步改 `docs/websites/.vitepress/config.ts` 与 `website.yml` 的校验前缀。
- **图用 Mermaid 写**（```` ```mermaid ```` 代码块，已接入 `vitepress-plugin-mermaid`），状态机用 `stateDiagram-v2`、流程用 `flowchart`；不要贴图片。
- **站内互引用相对路径**（如 `./deploy`、`../usage/login`）；VitePress 对死链只警告不报错，因此 `website.yml` 里有一次硬校验，改链接后请本地 `make docs` 确认。
- **不要把 `node_modules` / `.vitepress/dist` 提交**（已在 `.gitignore`）。
- 本地预览：`make docsdev`；构建：`make docs`。注意本仓库开发环境的 `NODE_ENV=production` 会让 npm 跳过 devDependencies，**安装时必须带 `--include=dev`**，否则 VitePress 装不上。

## 7. CI/CD

- **`test.yml`（测试）**：push 任意分支与 PR 触发。`detect` 按变更目录过滤（`server/**` / `web/**`）；后端起 MySQL 8.0 service 跑单元 + 集成测试，前端跑 typecheck / 单测 / 构建并校验产物（含固定文案存在性）。汇总 job `测试通过` 是分支保护的必需检查，路径跳过按成功处理。
- **⚠️ 必需检查工作流禁止在 `on:` 上写 `paths` 过滤**：`测试通过` 是分支保护的必需检查，若在触发层就按路径过滤，只改 `docs/`、`README.md`、`todo.md` 的 PR 不会触发工作流，必需检查永不回报，PR 会永久卡在 `BLOCKED` 无法合并。正确做法是「**总是触发 + 在 job 内按变更目录跳过**」：`on` 不写 `paths`，由 `detect` 判断、子任务 `if` 跳过，汇总 job 始终回报状态。`build-images.yml` 不是必需检查，可以保留路径过滤以省额度。
- **`build-images.yml`（构建并推送镜像）**：push `main` 与手动触发。推送到 **ghcr.io**，**固定 tag**：
  - `ghcr.io/sakana-1314/lan-drive:server`（内网 API）
  - `ghcr.io/sakana-1314/lan-drive:web`（公网前端）
  
  同时打时间戳 tag（`server-YYYYMMDD-HHMMSS`）便于回滚。按变更路径只构建改动过的镜像；手动触发时两个都构建。web 镜像通过 `--build-arg HOST=${{ vars.API_HOST }}` 注入后端域名，**域名只允许来自仓库变量 `vars.API_HOST`，禁止写进工作流文件**。
- **`website.yml`（构建并发布文档站）**：push `main` 与 PR（`docs/**` 变更）+ 手动触发。PR 只构建校验、不发布；push `main` 时构建后强推 `gh-pages` 分支，由 GitHub Pages 发布到 <https://sakana-1314.github.io/lan-drive/>。发布前会硬校验站内链接与资源是否都存在。
- **镜像要求**：`server` 镜像只含二进制（多阶段构建、非 root、内置 HEALTHCHECK）；`web` 镜像为 nginx + 静态产物 + `docker-entrypoint.d` 运行时注入脚本。两个镜像都不得硬编码内网地址或密钥。
- **安全红线**：工作流文件公开可见，**严禁硬编码 IP、密钥、内网域名**，一律 `${{ secrets.* }}` / `${{ vars.* }}` 引用；`GITHUB_TOKEN` 只申请必需的权限（`contents: read`、推送镜像时加 `packages: write`）。
