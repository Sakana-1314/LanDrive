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

**部署拓扑是硬约束**：前端在公网、API 在内网。任何改动都不得破坏这条边界，
也不得让后端承担托管页面的职责（`server` 不内嵌前端产物）。

两者之间的连接有两种方式，前端的 API 地址解析必须同时兼容：

- **同源反代**：前端镜像内的 nginx 把 `/api` 转发到后端（`LANDRIVE_API_PROXY`），
  浏览器视角同源，不跨域、不需要 CORS。**推荐**，也是 compose 的默认形态。
- **跨域直连**：浏览器直接请求后端绝对地址，需要后端配 `LANDRIVE_CORS_ALLOW`。

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
- **API 地址**：优先级为「运行时 `/config.js` → 构建期 `HOST`（`__API_HOST__`，拼成 `${HOST}/api`）→ 同源 `/api`」，解析逻辑只在 `src/api/base-url.ts`（纯函数，便于在 Node 下单测；`index.ts` 只负责读取运行时配置并调用它）；健康探测的判定在 `src/api/health.ts`。`HOST` 是**构建期**环境变量（仅 origin，不含 `/api`），与同组织其它前端项目保持一致；容器部署还可用运行时 `LANDRIVE_API_BASE_URL` 覆盖，**不要为了换地址重新构建镜像**。
- **网络判定**：区分「请求未到达服务器」（超时/连接失败/被 CORS 拦截 → 提示更换网络）与「有响应但业务失败」（按业务提示）。判定逻辑只在 `src/api/network.ts`。**绝不能把网络不可达显示成"密码错误"。**
- **`/api` 同源反代**：前端镜像内置 nginx，可用 `LANDRIVE_API_PROXY=主机:端口` 把同源 `/api` 转发到后端容器（不跨域、免 CORS）。实现要点，改动时别踩：
  - 入口脚本会同时（a）生成 `/etc/nginx/proxy.d/api.conf`，（b）把运行时 `apiBaseUrl` 写成 `/api`。**两者必须一起改**，只改一个会出现「配了反代但前端仍跨域直连」。
  - 反代地址必须**校验后再拼进配置**：`grep` 按行匹配，含换行的值会绕过校验并注入 nginx 指令 —— 用 `case` 做整串匹配。非法值应让容器启动失败并打印原因。
  - `proxy_pass` 用变量 + `resolver`（而非直接写主机名），否则后端容器未就绪时 nginx 会 `[emerg] host not found in upstream` 导致**整个前端起不来**。
  - `proxy_pass` 用变量时不能带 URI；我们的需求正是保留 `/api` 前缀（后端路由就是 `/api/**`）。
  - 未启用反代时 `/api` 必须返回 JSON 404，**不能落到 SPA 回退返回 200 HTML**，否则健康探测会把"未连通"误判为"已连通"。
  - 回归测试：`npm run test:entrypoint`（`web/scripts/test-entrypoint.sh`），装了 nginx 时会用 `nginx -t` 校验生成的配置。
- **上传**：走 `src/utils/upload.ts` 的 `UploadManager`（分片并发、断点续传、重试）；分片大小必须服从服务端 `init` 返回的 `chunk_size`，前端不得自行决定。
- **预览**：预览库一律 `defineAsyncComponent` + 动态 `import()`，**主包不得引入 docx-preview / exceljs / pptx-preview**；新增预览类型时同步 `previewKind`（server）与预览矩阵表（`docs/design.md`）。
- **样式**：用 Naive UI 组件与 `n-space`/`n-card` 布局，避免自写大段 CSS；设计令牌集中在 `src/styles.css`，Naive UI 的主题覆盖值在 `src/utils/theme.ts`（详见下方 UI 约定）。
- **代码质量**：`npm run typecheck`（vue-tsc）、`npm test`（网络判定单测）、`npm run build` 全绿。
- **UI 约定（参照同组织 Electrical-Manager）**：
  - **设计令牌**集中在 `src/styles.css`（`--color-*` / `--radius-*` / `--shadow-*`），明暗两套只覆盖颜色类令牌。
    **不要写内联魔法色值**（`#1f6feb`、`#f2f3f5` 之类），用变量。
  - **明暗外观**在 `src/utils/themeState.ts`（`reactive` 单例，三档 auto/light/dark），
    Naive UI 覆盖值在 `src/utils/theme.ts` 的 `createThemeOverrides(palette)`；
    两套是一份结构两个调色板，**不要只改一套**（另一套会掉回内置值）。
    **浮层类组件必须覆盖 `common.popoverColor`**：Dropdown / Select / Popover 的弹层底色
    用它而不是 `cardColor`，漏掉时深色档会显示 Naive 内置的 `rgb(72,72,78)` 灰
    （已发生，用户下拉菜单那块不协调的底色）。由 `src/utils/theme.spec.ts` 守卫。
  - **页面头部统一样式**：`.section-head` = 左区 `.section-head__main`（标题、计数、
    分段切换、筛选器）+ 右区 `.section-head__actions`（只放按钮）。
    **筛选一律靠左、按钮一律靠右**（曾出现同类分段切换一处靠左一处靠右，翻页找不到控件）。
    头部内控件用 Naive 默认尺寸 medium（34px），**不要传 `size="small"`**（28px 与 34px
    并排会参差不齐）；`small` 只用于表格行内。卡片圆角统一 `--radius-card`，不要内联
    `border-radius`。由 `scripts/check-ui-consistency.mjs` 守卫（已接入 `npm test`）。
  - **响应式**：`useIsMobile()`（≤768px）判断形态；视图切换优先用 CSS 媒体查询而非 JS 断点（首帧即正确）。
    桌面表格 / 移动卡片两套视图必须共用同一份操作逻辑（见 `composables/useFileActions.ts`），不要各写一份。
  - **布局陷阱**（都踩过）：全局 `box-sizing: border-box` 不可去掉；
    grid 容器与页面根节点用 `grid-template-columns: minmax(0, 1fr)`（默认 `min-width:auto` 会被长内容撑宽、
    内容被 `n-scrollbar` 裁掉）；全屏页面用 `100dvh` 而不是 `100%`。
  - **文案**：不要用整段小字解释功能 —— 功能应显而易见（策略值用角标、权限差异用标签、
    流程用箭头表示）。**异常态例外**：网络不可达等必须说清原因与做法。
  - **回归测试**：`npm run test:responsive`（真实浏览器 × 5 视口查横向溢出，
    自带 mock 后端与 dev server）。CI 设 `REQUIRE_PLAYWRIGHT=1`，未装浏览器时**失败而非跳过**
    —— 否则会出现"什么都没检查却报绿"的假通过（已发生三次）。
    **判断"元素是否被裁剪"不能只看有没有滚动祖先**：`n-scrollbar-container` 是
    `overflowX:scroll`，旧逻辑一见它就跳过，于是被 `overflow:hidden` 祖先**裁掉**的元素
    （390px 下工具条第三个按钮只剩一半）能一路报绿。现在的口径是先遇到的
    `auto/scroll` → 可滚动（跳过）；先遇到的 `hidden/clip` 且超出其右边界 → 真的被裁（报错）。
  - **上传文件夹**：拖入文件夹时 `dataTransfer.files` 里**只有那个文件夹本身**
    （一个 0 字节 File），只读 `files` 就会「只上传一个与文件夹同名的空文件」。
    目录内容必须经 `webkitGetAsEntry()` 递归读出（`<input webkitdirectory>` 则读
    `webkitRelativePath`），再按相对层级在服务端逐级建目录（同名复用）后分别入队。
    展平逻辑集中在 `src/utils/uploadEntries.ts`；每个任务的 `folderId` 必须**自带**
    （同一批文件分属不同层级，不能用共享字段）。由 `uploadEntries.spec.ts` 与
    `npm run test:folder-upload`（真实浏览器，含建目录/落点断言）共同守卫。
  - **上传进度浮窗**：队列状态是**全局单例**（`src/stores/upload.ts` 的
    `uploadQueueState`），浮窗组件 `components/UploadPanel.vue` 挂在 `MainLayout` 上，
    右下角常驻。**不要把队列状态放回页面组件**：组件一卸载队列就从界面上消失，
    用户切页后看不到进度、也没法取消（这是被修掉的老问题）。
    约定：空队列不渲染；全部传完自动收起，**有失败时保持展开**（失败不能被折叠藏住）；
    汇总口径（按字节算总进度、速度只汇总进行中的任务、标题优先级）集中在
    `src/utils/uploadSummary.ts`，由 `uploadSummary.spec.ts` 与
    `npm run test:upload-panel`（真实浏览器：逐条进度条真的在涨、切页后仍在动、
    失败不被收起）共同守卫。

## 6. 部署编排（docker-compose.yml）

- **只面向 1Panel 场景**：编排文件不创建网络，所有服务加入 1Panel 已有的 `1panel-network`
  （`networks: 1panel-network: external: true`），以便与面板里安装的其它应用互通。
- **MySQL 不在编排里**：数据库用 1Panel 应用商店安装或指向已有实例，通过 `.env` 的
  `LANDRIVE_MYSQL_DSN` 连接。**不要**往 compose 里加 `mysql` 服务或 `mysql-data` 卷。
- **网页端默认走 `/api` 同源反代**：`LANDRIVE_API_PROXY` 默认 `api:8080`，因此
  1Panel 只需为网页端配一个域名，`LANDRIVE_CORS_ALLOW` 可留空。
- **必填项用 `${VAR:?提示}`**：缺失时 compose 直接报错并给出可操作提示，不允许带空值启动。
  可选值用 `${VAR:-默认值}`。新增变量必须同步 `.env.example`。
- **`.env` 不提交**（已在 `.gitignore`）；`.env.example` 只放占位与说明，不得出现真实域名、IP、密码。
- 改编排后请校验：`docker compose config` 能解析、不含 `mysql` 服务、`1panel-network` 为 external。
  本地无 docker 时至少确认 YAML 可解析且 `${VAR:?}` 覆盖了必填项。

## 7. docs（文档站）

- **技术栈**：VitePress（源码 `docs/websites/pages/`，构建产物 `docs/websites/.vitepress/dist/`）。
- **面向使用者**：文档站只讲**怎么部署、怎么用**，不写实现细节（那些放 `docs/design.md`）。语言要口语化、面向非开发同事。
- **`base` 必须与仓库路径大小写完全一致**（当前为 `/LanDrive/`）。GitHub Pages 的路径**区分大小写**，仓库改名（尤其大小写）会让线上资源全部 404。**改名时必须同步这四处**：
  1. `docs/websites/.vitepress/config.ts` 的 `base` 与 `socialLinks`
  2. `.github/workflows/website.yml` 的站点地址与链接校验前缀
  3. `README.md` 的徽章与仓库链接、文档站页面里的 `git clone` 地址
  4. `AGENTS.md` 本条说明
- **反之，以下名称必须保持小写，不要跟随仓库名改成 `LanDrive`**：ghcr 镜像路径（`ghcr.io/sakana-1314/lan-drive`）、`docker-compose.yml` 的 `name:` 与 `container_name:`、本地构建的镜像 tag。Docker 与 ghcr 均不接受大写。
- **图用 Mermaid 写**（```` ```mermaid ```` 代码块，已接入 `vitepress-plugin-mermaid`），状态机用 `stateDiagram-v2`、流程用 `flowchart`；不要贴图片。
- **部署文档要与镜像行为一致**：网页端默认用 `/api` 同源反代，文档不能再说「必须配 CORS」；只有跨域直连（`LANDRIVE_API_PROXY` 留空）时 `LANDRIVE_CORS_ALLOW` 才是必填。
- **站内互引用相对路径**（如 `./deploy`、`../usage/login`）；VitePress 对死链只警告不报错，因此 `website.yml` 里有一次硬校验，改链接后请本地 `make docs` 确认。
- **不要把 `node_modules` / `.vitepress/dist` 提交**（已在 `.gitignore`）。
- 本地预览：`make docsdev`；构建：`make docs`。注意本仓库开发环境的 `NODE_ENV=production` 会让 npm 跳过 devDependencies，**安装时必须带 `--include=dev`**，否则 VitePress 装不上。

## 8. CI/CD

- **`test.yml`（测试）**：push 任意分支与 PR 触发。`detect` 按变更目录过滤（`server/**` / `web/**`）；后端起 MySQL 8.0 service 跑单元 + 集成测试，前端跑 typecheck / 单测 / 构建并校验产物（含固定文案存在性）。汇总 job `测试通过` 是分支保护的必需检查，路径跳过按成功处理。
- **⚠️ 必需检查工作流禁止在 `on:` 上写 `paths` 过滤**：`测试通过` 是分支保护的必需检查，若在触发层就按路径过滤，只改 `docs/`、`README.md`、`todo.md` 的 PR 不会触发工作流，必需检查永不回报，PR 会永久卡在 `BLOCKED` 无法合并。正确做法是「**总是触发 + 在 job 内按变更目录跳过**」：`on` 不写 `paths`，由 `detect` 判断、子任务 `if` 跳过，汇总 job 始终回报状态。`build-images.yml` 不是必需检查，可以保留路径过滤以省额度。
- **`build-images.yml`（构建并推送镜像）**：push `main` 与手动触发。推送到 **ghcr.io**，**固定 tag**：
  - `ghcr.io/sakana-1314/lan-drive:server`（内网 API）
  - `ghcr.io/sakana-1314/lan-drive:web`（公网前端）
  
  同时打时间戳 tag（`server-YYYYMMDD-HHMMSS`）便于回滚。按变更路径只构建改动过的镜像；手动触发时两个都构建。**web 镜像故意不传 `HOST`**，让前端走**同源 `/api`**（与网页端自己的域名一致）：
  - 浏览器只访问一个域名，不跨域、不需要 CORS；
  - 镜像与部署环境解耦，换地址只改运行时环境变量（`LANDRIVE_API_PROXY` 或 `LANDRIVE_API_BASE_URL`）；
  - **公开镜像绝不能烘焙真实后端域名**：镜像推到公开 ghcr 后任何人都能解包读取，
    曾因 `--build-arg HOST=<内网域名>` 把内网地址泄进公开镜像。
    因此工作流里不得出现 `vars.API_HOST` 之类的注入；确有需要时由使用者自建镜像传 `--build-arg HOST=`。
  - 守卫：`web/scripts/check-no-internal-host.sh`（构建前扫描 `.env*` / `vite.config.ts` / `src`，
    排除测试文件），已接入 `npm run test:entrypoint` 与 `build-images.yml`。
- **`website.yml`（构建并发布文档站）**：push `main` 与 PR（`docs/**` 变更）+ 手动触发。PR 只构建校验、不发布；push `main` 时构建后强推 `gh-pages` 分支，由 GitHub Pages 发布到 <https://sakana-1314.github.io/LanDrive/>。发布前会硬校验站内链接与资源是否都存在。
- **镜像要求**：`server` 镜像只含二进制（多阶段构建、非 root、内置 HEALTHCHECK）；`web` 镜像为 nginx + 静态产物 + `docker-entrypoint.d` 运行时注入脚本。两个镜像都不得硬编码内网地址或密钥。
- **安全红线**：工作流文件公开可见，**严禁硬编码 IP、密钥、内网域名**；
  **仓库源码与文档同样公开，示例地址一律用 `example.com` / `localhost` 占位**（真实域名只存在于部署机的 `.env` 与仓库变量/密钥中），一律 `${{ secrets.* }}` / `${{ vars.* }}` 引用；`GITHUB_TOKEN` 只申请必需的权限（`contents: read`、推送镜像时加 `packages: write`）。
- **隐私/私有文件一律放 `/.private/`**（已在 `.gitignore`，整体不提交）：凭据、部署备份、
  私人笔记、临时导出等**本机要留档**的私有内容都归到它下面，**不要**放在仓库其它位置
  —— 否则一次 `git add -A` 就会把它们带进这个公开仓库。放进仓库的**任何**文件都视为
  已公开：真实工号、姓名、内网地址、域名一律用 `<工号>`、`example.com` 之类占位。
  `.env` 及其变体同样不提交（模板见 `.env.example`）。
  **删除已提交的敏感信息时不要开 PR** —— PR 的 diff 会把被删掉的明文完整展示在
  公开页面上；应直接 `git push --force` 覆盖分支并重写历史。

## 9. 发布流程（合并到 main 之后必做）

**合并 PR 只是把镜像推上 ghcr，线上还是旧版本 —— 内网部署机必须再手动更新一次。**
这一步最容易漏（漏了就是"CI 全绿但用户看到的还是旧界面"），所以固定成流程：

1. **合并到 main** → `build-images.yml` 按变更目录重建镜像（只改 `web/**` 就只重建 web），
   `website.yml` 在 `docs/**` 变更时自动发布文档站（这一步是自动的，不用管）。
2. **更新内网部署机**（`ssh` 别名与私钥只存在于开发机 `~/.ssh/config`，仓库里不写地址）：
   ```bash
   ssh landrive                       # 见 ~/.ssh/config，非仓库内容
   cd /opt/1panel/docker/compose/lan-drive
   BK=/root/landrive-update-$(date -u +%Y%m%d-%H%M%S); mkdir -p "$BK"
   cp docker-compose.yml "$BK/" && docker inspect LanDrive-Web LanDrive-API \
     --format '{{.Name}} {{.Image}}' > "$BK/images-before.txt"   # 回滚点
   docker compose pull <service>       # 只拉改动过的：web / api
   docker compose up -d --no-deps <service>
   ```
   **只更新改动过的服务**：`--no-deps` + 指定服务名，避免无谓重建另一个容器
   （server 没改就别动 `api`，它会执行迁移）。
3. **核对镜像 digest**：`docker images --digests | grep lan-drive` 的 digest 必须等于
   `build-images.yml` 日志里 push 的那个 `sha256:...`。**只看到 CI 绿灯不算验证。**
   `docker compose pull` 是判断"要不要更新"的可靠依据：两个镜像都 `Pulled`、随后
   `up -d` 只报 `Container ... Running`（未 Recreate）、更新前后 digest `diff` 为空
   —— 说明本来就已经是最新，这是**正常结论**，不要为了"必须做点什么"而强行重建容器。
4. **验收（对着生产，不是对着本地）**：容器 healthy、`/api/health` 通、
   同源 `/api` 反代通、新产物指纹确实进了容器
   （`docker exec LanDrive-Web grep -l <本次新增文案> /usr/share/nginx/html/assets/*.js`）、
   数据未动（用户数 / 回收站数 / `data/users` 文件数）。
5. 把结果追加到 `todo.md` 的 Log（这是台账）。

**拓扑（别把中继当部署机）**：对外那个公网 IP 是 **FrpServer 中继**，不是部署机。
`ssh -p <穿透端口>` 落到的是**内网的部署机**，中继只是转发。
识别办法：中继上只有 frps/其它项目、**完全没有 LanDrive**（无容器/镜像/compose/站点）；
部署机上有 `LanDrive-Web` + `LanDrive-API`。两台机器 **hostname 可能都叫 `Host`**，
所以要用「容器清单 + 内网 IP」区分，不要靠 hostname。动手前先跑
`curl -u <frps面板账号> http://127.0.0.1:<面板端口>/api/proxy/tcp` 看隧道指向，
确认目标机器再操作（曾差点误以为要在中继上部署）。

**踩过的坑**：
- 内网穿透隧道不稳、且会截断大文件。**远程命令一律 `setsid nohup ... &` 交给远端后台执行**，
  否则隧道一断命令就半途中止（曾把 `docker compose up` 打断在中间）。
  ⚠️ 但**脚本要先 `cat > 远端脚本 && chmod +x`，再 `setsid nohup bash 远端脚本 &`**：
  直接 `ssh 'setsid ... &' < local.sh` 会让 detached 与 stdin 管道冲突，脚本收不到内容、静默不执行。
- 需要本地浏览器验收生产页时，用自愈隧道
  （`while true; do ssh ... -N -L <本地端口>:127.0.0.1:<网页端端口> landrive; sleep 2; done`），
  并注意 `page.addInitScript` 必须在 `goto` 之前注册。
- 真实域名解析到**内网地址**，在开发机上不可直达，只能经隧道访问。浏览器验收要开
  `--host-resolver-rules=MAP <域名> 127.0.0.1` + `--ignore-certificate-errors`；
  且**不要用 `page.request`**（它不走浏览器的解析器，仍会解析到内网地址而超时），
  改用页面内 `fetch` 拿 token 再写 `localStorage`。
- 生产的 compose / `.env` 里含 JWT 密钥与管理员密码 —— 只存在于部署机，
  **绝不可复制进仓库、日志或提交信息**；`todo.md` 又是公开文件，写台账时不要重复贴地址与凭据。

