# Todo — 局域网文件助手

实施进度台账。规划与完成事件都追加到 `## Log`（最新在最上方）。

## Now

（全部完成 —— 代码、镜像、文档站均已发布；CI 全绿）

## Next

（无）

## Later

（无）

## Log

- **2026-09-25 P23 仓库隐私/中间文件复核清理 + 固化 `/.private/` 约定**

  要求：复核整个仓库，删掉与项目无关的文件（含中间文件、隐私文件），
  隐私文件应放在被 gitignore 的仓库目录。

  **复核结论（先查再动）**：仓库根目录**实际只有 12 个条目**（`.dockerignore`、
  `.env.example`、`.gitignore`、`AGENTS.md`、`Makefile`、`README.md`、
  `docker-compose.yml`、`todo.md` + `.github/`、`docs/`、`server/`、`web/`），
  与 GitHub 上一致，**没有**混进无关文件或构建产物 —— 上一轮的拖拽框、重复空状态
  等也无残留。所以这次的重点落在「隐私」与「本机私有留档」上。

  **发现并修复的真实隐私问题**：公开仓库的台账里写着**真实工号**
  （`todo.md` 两处、`storage_test.go` 测试夹具一处）。已全部改为 `<工号>`/
  `<新工号>`/无关数字占位。**没开 PR**（PR 的 diff 会把被删掉的明文展示在公开页面
  上，PR #18 的教训），直接 fast-forward 推到 `main`。

  **本机私有文件归入 `/.private/`**（已 gitignore，`git check-ignore` 验证）：
  之前散落在 `/root` 的 git bundle 全量备份、worktree 备份、分支保护备份、
  指向文件，全部移入 `.private/backups/` 并修正了指针文件；空目录 `bin/` 删除
  （`make` 会重建）；本项目的 `/tmp` scratch（demo 数据、旧克隆、截图等约 430MB）
  一并清理。**均未进 git**。

  **补 .gitignore 缺口**：根目录 `.env.*` 变体（staging/production 等）此前**未被忽略**，
  可能把真实地址带进公开仓库；已加 `/.env.*`（保留 `!/.env.example`）与 `web/.env.staging`。

  **固化规则**：AGENTS.md 安全红线补上「私有文件一律放 `/.private/`」
  「进仓库即视为公开」「删除敏感信息禁止开 PR，应强推覆盖并重写历史」。

  **清理本地 git 残留**：7 个已合并的陈旧本地分支（`chore/rename-to-landrive`、
  `feat/api-proxy` 等）仍可达**已从分支清除的真实内网域名**，已全部删除并
  `reflog expire` + `gc --prune=now`；本地 12 个含泄漏的 blob 命中归 **0**，
  `git fsck` 无异常。顺带 prune 了两个已在远端删除的 remote-tracking ref。

  **验证**：`typecheck` / `npm test` / `build` / `test:responsive`（5 视口）/
  `test:entrypoint`（含真实 nginx 与内网域名守卫）/ `vitepress build` 全绿；
  CI `测试通过`（后端 Go + 汇总）与 `构建并推送镜像` 成功。
  改动只碰 `.gitignore`、`AGENTS.md`、`todo.md`、一个 `_test.go` 夹具，
  **未触碰**工作区里另一会话的 logo/favicon 改动（原样保留）。

  **部署**：`server/**` 有变更（仅测试文件，`go build` 不含测试）触发 server 镜像重建，
  按流程只更新 `api`：digest `c1005616…` → `fff91c89…`（与构建日志一致），
  `--no-deps` 未动 web。回滚点 `/root/landrive-update-20260925-014821`。
  验收：两容器 healthy、`/api/health` 通（`schema_ver:3`）、登录与用户数 2 正常。
  说明：回收站计数从台账记的 1 变为 2，多出的文件在 **01:07Z** 被删除，
  **早于本次 01:48Z 的部署**，非本次操作所致。

- **2026-09-25 P22 修复「我的文件」出现两遍空状态提示（PR #19）+ 部署**

  现象：空目录下页面同时渲染「这里还没有文件」与「暂无文件」两条空提示。

  根因：**PR #13 在 `FilesView` 里加了一条页面级 `n-empty`**（条件为根目录且
  无文件无文件夹），而 `FileTable` 自己已经负责空状态 —— 它用 `#empty`
  渲染桌面表格的空态、另有移动端卡片前的一条，两者按 CSS 断点互斥。
  两个条件同时成立，于是同屏出现两遍。桌面与移动端都可复现
  （修复前实测 `可见空提示 = ["这里还没有文件","暂无文件"]`）。

  修法：删掉页面级那条（连带不再使用的 `NEmpty` 导入），空状态统一归
  `FileTable`。

  防复发：`check-ui-consistency.mjs` 增加规则 —— **用了 `FileTable` 又自己写
  `<n-empty>` 直接报错**，已接入 `npm test`。做了变异验证：把该 `n-empty`
  加回去，脚本确实以退出码 1 报出（不是假绿）。

  验证：真实浏览器 × 桌面(1280)/移动(390) × 3 种空场景（我的文件根目录、
  空子目录、全部文件）共 12 项断言全过，均「恰好一条」；typecheck / npm test /
  build / test:responsive 全绿。

  部署：镜像按 `web/**` 变更重建，新 digest `sha256:faed6ac7…`；穿透目标机
  只更新 web（`--no-deps web`，api 未重建、23 小时 uptime 不变）；
  生产站点真实浏览器复验「我的文件 / 全部文件」各只有一条空提示且无 JS 报错；
  数据未动（用户 2 / 回收站 1 / 磁盘 1）。回滚点
  `/root/landrive-update-20260925-001807`。

- **2026-09-24 P21 穿透目标机复核（确认已是最新，无需更新）**

  用户要求「把最新线上版更新到内网穿透的服务器」。先摸清拓扑再动手，
  结果与最初的假设不同 —— **对外那个公网 IP 是 `FrpServer` 中继，不是部署机**：
  它的 frps 管理接口显示多个客户端与 TCP 隧道，其中一条 SSH 隧道
  转发到内网机 22 端口。也就是说 `ssh -p <穿透端口>` 落到的是内网部署机，
  公网 IP 只是中转。（具体地址与端口见部署机，**不写进本公开仓库**。）

  两台机器 **hostname 都叫 `Host`**（容易混），靠这些特征区分：
  - 公网中继：Debian 13 trixie，容器 `MaterialsManager-API / FrpServer /
    MySQL / Nginx / XRay / SubConverter`，**完全没有 LanDrive**（无容器、
    无镜像、无 compose、无 1Panel 站点、全盘无前端特征文件）。
  - 穿透目标：Debian forky/sid，容器
    `LanDrive-Web / LanDrive-API / FrpClient / IPIP-API / IPIP-Web / Nginx / MySQL`。

  经与用户确认，目标是穿透指向的那台内网机（即 P20 已更新过的那台）。
  于是把更新流程**幂等地重跑一遍**做确证，而不是"看着像是最新"就结束：
  - `docker compose pull web api` → 两个镜像均 `Pulled`；
  - `docker compose up -d web api` → 均为 `Container ... Running`（未重建）；
  - 更新前后 digest `diff` **完全一致** → 确实无需更新。

  版本确证（与 ghcr 上 `web`/`server` 的 `docker-content-digest` 逐一相等）：
  `web = sha256:97823fa3…6305`、`server = sha256:c1005616…09a5`。

  **生产验收**（对着穿透后的真实站点，不是本地）：
  - 容器 healthy、`/api/health` 通、同源 `/api` 反代通、SPA 深链 200；
  - 容器内产物含本次新增文案、旧 `n-upload-dragger` 已消失、`section-head__main`
    与 `popoverColor` 两档均在；首页加载的正是本次发布的 `index-B9p6YPQV.js`；
  - 真实浏览器：整页拖放提示生效、头部控件统一 34px、下拉菜单明暗两档底色
    `rgb(255,255,255)` / `rgb(28,34,43)` 且左对齐、各管理页无 JS 报错；
  - 数据未动：用户 2、回收站 1、`data/users` 文件 1。回滚点
    `/root/landrive-update-20260924-131600`（compose + 更新前镜像快照）。

  **环境提示**：这条穿透隧道仍然不稳（期间断了 3 次）。
  两点经验：远程写操作照旧 `setsid nohup … &` 交远端后台执行，但**脚本要先
  `cat >` 落到远端再后台跑** —— 否则 detached 与 stdin 管道冲突，脚本根本收不到；
  本地浏览器验收需把域名经 `--host-resolver-rules` 映射到隧道端口，
  且**不能用 `page.request`**（它不走浏览器解析器，会解析到内网地址而超时），
  改用页面内 `fetch`。

- **2026-09-24 P20 整页拖放上传 + 页面头部/用户下拉菜单统一（PR #15）+ 内网部署**

  需求两项：上传不要单独的拖入区域（整页任意位置可拖）；UI 不统一
  （分段切换高度与左右位置不一致）、用户下拉菜单显示异常。

  **一、整页拖放上传**：删掉虚线拖拽框，改为 document 捕获阶段的页面级拖放
  （`PageDropZone`）—— 列表、文件夹卡片、面包屑、空白处任意位置松手即传，
  拖入时浮出整页提示。入口两处共用一份队列：整页拖放 + 工具条「上传文件」按钮
  （移动端没有拖拽操作，必须留按钮）。`UploadPanel` 拆成 `useUploadQueue`（逻辑）
  + `UploadQueue`（纯队列视图）。目标目录在**入队那一刻**同步，拖入后切目录不会传错。

  顺带修掉一个真实缺陷：`/files` 与 `/files/mine` 复用同一组件实例（原代码正是为此
  watch `route.fullPath`），只在 `onMounted` 拉上传策略的话，从「全部文件」点进
  「我的文件」时策略永远拉不到 ——「上传已暂停」静默失效、按钮不禁用。
  改为路由变化也调用幂等的 `prepare()`。

  **二、UI 统一**（先量再改，不凭观感）：改前实测分享管理分段切换靠左、
  文件与回收站**靠右**；头部混用 28px 与 34px；用户管理卡片自写 `border-radius:8px`。
  现固定为**筛选靠左、按钮靠右**：`.section-head` = 左区 `.section-head__main`
  + 右区 `.section-head__actions`；头部控件统一 Naive medium（34px）；
  卡片圆角统一 `--radius-card`。

  **三、用户下拉菜单两个真实缺陷**：
  1. 深色档弹层底色是 Naive 内置灰 `rgb(72,72,78)`。根因：Dropdown/Select 弹层用
     `common.popoverColor` 而**不是** `cardColor`，`theme.ts` 从未覆盖它。已补两档。
  2. 用量块与选项文字错开：实测「姓名」在 `x=0`、而「外观」在 `x=36`
     （渲染型选项不参与 Naive 前缀缩进）。已按 `--n-option-icon-prefix-width` 对齐。

  **防复发**（均接入 `npm test`，且做了变异验证 —— 去掉修复会真的失败）：
  `theme.spec.ts`（明暗两套覆盖结构必须一致、`popoverColor` 必须显式给出）、
  `check-ui-consistency.mjs`（头部左右结构、控件尺寸、卡片圆角）。

  **发布与部署**：
  - PR #15 合并 `c7c4c67`；`build-images.yml` 只重建 web（按变更目录跳过 server）；
    文档站自动发布到 gh-pages。
  - web 镜像 digest `sha256:97823fa3…6305`（固定 tag + 时间戳 tag `web-20260924-103734`），
    与构建日志一致。
  - 内网部署机按「只更新改动过的服务」更新 web：`docker compose up -d --no-deps web`，
    **API 未重建**（`api` 容器 0 重启、StartedAt 不变，确认没白跑迁移）。
  - 验收对着生产做（经自愈隧道，真实域名解析到内网、开发机不可直达）：
    容器 healthy、`/api/health` 通、同源 `/api` 反代通、容器内产物含本次新增文案
    且旧 `n-upload-dragger` 已消失；真实浏览器在生产页确认整页拖放提示、
    头部控件统一 34px、下拉菜单明暗两档底色为 `#ffffff` / `#1c222b` 且左对齐 36=36、
    各管理页无 JS 报错。
  - **数据未动**：users=2、回收站=1、active=0、`data/users` 文件数=1，与更新前一致。
    回滚点 `/root/landrive-update-20260924-111954`（compose + 更新前镜像 digest）。

  **环境提示**：隧道不稳，曾把 `docker compose up` 打断在中间 —— 远程写操作一律
  `setsid nohup … &` 交远端后台执行；本地浏览器验收用自愈隧道（断了自动重连）。

- **2026-09-23 P19 工号目录 / 菜单整改 / 删除账号自动清文件（PR #12）+ 部署**

  **需求实现**
  1. 磁盘目录 `users/<id>` → `users/<工号>`：落盘后一眼看出是谁的文件。
     代价是**工号成为不可变标识**（改了会让已有文件变孤儿），因此把约束固化：
     新增 `storage.UserDirName()` 按路径段严格校验工号（拒空串、`.`、`..`、
     分隔符、空格），创建账号时也用它，让错误提前到参数校验阶段。
     `FileRel` 改为接目录相对路径而非 owner id（沿用会话里的 `dir_rel`），
     **存量数据无需迁移**也能继续写对自己的目录。
  2. 删除账号语义：软删全部文件 → 终止上传会话 → 物理清除文件与记录 →
     删账号 → 清磁盘目录。去掉 `?purge_files=1` 与界面开关（语义已固定）。
     目录必须清：按工号命名，同工号重建账号会复用同名目录、继承前任文件。
  3. 菜单：删「全部人员」子项（父级本身就是看全部）；**0 文件账号不列出**；
     一个都没有文件时父级退化成普通链接。

  **顺带修掉一个连带问题**：`n-menu` 默认把父级点击当成展开/收起，
  于是选过某用户后点「全部文件」永远回不到全部列表。改为 label 只负责导航
  （stopPropagation 拦冒泡）、展开收起交给右侧箭头，两者互不干扰
  （用真实鼠标点击验证，之前用 JS 派发到容器点错了目标，误判过一次）。

  **迁移带出的真实风险（已修）**：目录改工号后，**存量数据仍在 `users/<id>`**，
  新建工号 `1`/`5` 会与旧目录**完全同名**，两账号共用一个磁盘目录、
  互相看到对方的文件；`users.dir_rel` 无唯一键，数据库不拦。
  在**线上数据上确认冲突真实存在**（`users/1` 命中 1、`users/5` 命中 1、
  `users/1001` 为 0），新增 `store.DirRelInUse` 在建账号前拦下并返回 409。

  **部署**（备份 `/root/landrive-update-20260923-234448`）
  - 部署前后逐项比对：`dir_rel`（admin→users/1、<真实工号>→users/5）与
    `files.rel_path`（users/1/1.bin）**完全不变**，既有数据未受影响；
  - 线上验收 11 项全通过：工号无法经接口修改；`a/b`、`a b`、`..`、`a:b` 均 400；
    工号 `1`/`5` 因旧目录冲突 409（提示清晰）；新工号创建后目录 = `users/<工号>`；
  - 全流程：新账号上传文件落在 `users/<新工号>/4.txt`（工号路径生效），
    删账号返回 `soft_deleted:1, purged:1`，磁盘目录已清除、数据库无残留，
    最终只剩原有 2 账号 + 1 文件；
  - 浏览器确认生产构建：菜单无「全部人员」、无空账号子 tab，
    而列表标题仍显示「全部人员」（保留，是当页归属说明）。

  **未做（按你的选择）**：线上旧数据不迁移（仍是 `users/1`、`users/5`）。
  代码已兼容：存量目录继续正常读写，新账号走工号目录。
  将来若要迁，需把文件移到 `users/<工号>` 并同步改写 `files.rel_path`。

- **2026-09-23 P18 内网部署更新 + 发现并修复登录按钮失效**

  **部署**（1Panel 主机，容器 LanDrive-API / LanDrive-Web；主机地址见部署机，
  **不写进本公开仓库**）：
  - 部署前备份：`/root/landrive-update-20260923-211104`（compose + mysqldump 16KB，含 36 条 op_logs）。
  - 先起 web（新镜像），再起 api（api 启动时执行迁移 0002）。
  - 结果：`schema_migrations` = 1,2；`op_logs` 已 DROP；`user_pins` 已建；
    users=2、files=1（trashed）数据完好；`/api/health` 报 `schema_ver: 2`。
  - 已下线的 `/api/admin/logs`、`/api/admin/logs/actions` 均返回 404。
  - 从 compose 移除已失效的 `LANDRIVE_LOG_KEEP_DAYS`。
  - 经真实域名验证：首页/SPA 深链/静态资源/`/api` 全部正常。
    主机本地 13 项功能校验全通过（含使用量口径、置顶往返与边界）。

  **部署验收时发现一个从初始版本就存在的真实缺陷：点「登录」按钮毫无反应。**
  - 现象：不报错、不发请求、页面不动；只有回车能登录。
  - 根因：`<n-form>` 是**未解析的自定义元素** —— `LoginView.vue` 从未导入 `NForm`。
    本项目按需导入 Naive UI，漏导入时 Vue 不报错、标签照进 DOM 但无行为，
    于是 `attr-type="submit"` 的按钮所在"表单"根本不是 `<form>`。
  - `git log -S 'attr-type="submit"'` 追到初始提交 `d5c7ff1` —— 从第一天起就点不动。
  - 同一规则扫全站又发现 `UsersView` 的 `<n-empty>` / `<n-pagination>` 也未导入。
  - 修复（PR #11）+ 新增 `web/scripts/check-component-imports.mjs` 并接入 `npm test`：
    类型检查与单测都发现不了这类问题（vue-tsc 不管未解析自定义元素）。

  **为什么之前的验证没抓到**：我在本地 mock 环境的验收一直用回车路径，
  只有这次对"生产产物 + 生产 API"逐项点查才暴露 —— 说明验收要多走真实交互路径。

  验证后再次更新 web 镜像（commit 2a7aaf6）。生产构建上确认：按钮已在 `<form>` 内，
  点击发出 `POST /api/auth/login` 并跳转 `/files`；侧栏用户子 tab、
  `/files?owner=5` 过滤标题、工作台指标、系统管理含存储一致性、
  我的文件的拖拽条均正常。

  **环境提示**：内网穿透隧道不稳，且**会截断大文件**（841KB 的 naive chunk 被截到
  152KB，导致浏览器报 ERR_INCOMPLETE_CHUNKED_ENCODING）。本次改为在远端打包
  `tar` 并校验 sha256 后本地托管，绕开隧道传输大文件；隧道本身用循环重连保活。

- **2026-09-23 P17 导航重构：用户子 tab 可置顶、上传并入我的文件、下线审计日志**
  详见 PR #9 与 #10。

  1. **「全部文件」改为可展开父级 tab**：展开出按人的子 tab（带文件数），
     找谁直接点，不必先进列表再筛选。子项图钉即置顶开关，用 stopPropagation
     拦住 n-menu 的选中行为（已验证点击置顶不触发导航）。
     新增 `user_pins` 表，按 (owner, target) 建表而非全局标记位 ——
     置顶是"谁的界面"这件事，不同人看到的顺序可以不同。
     双向 ON DELETE CASCADE，删账号自动清引用，不留幽灵项。
  2. **顶栏「0 个文件 · 0 B」移入悬浮菜单**，外观改为二级悬浮菜单。
     使用量样式必须放全局 CSS：n-dropdown 内容会 teleport，scoped 覆盖不到。
  3. **上传并入「我的文件」**：抽出 `UploadPanel`，紧凑拖拽条 + 队列。
     删掉「最大 500 MB」「不限类型」角标（超限会直接报错，不必占位）。
  4. **审计日志整体下线**：页面/路由/菜单/接口/写入/裁剪/表全部移除，
     迁移 0002 直接 DROP op_logs —— 留一张永远不写的表只会让人以为功能还在。
     删除 `ListLogs`/`LogActions`/`audit()`/`PruneLogs`/`LogKeepDays` 等；
     `clientIP()` 保留（登录限流仍在用）。
  5. **统计看板 → 工作台**（删「最近操作」）；**系统配置 → 系统管理**
     并迁入存储一致性检查；删左上角「保存后立即生效」与「仅我可修改/均可下载」tag。
  6. **修复多处 0 边距**：根因是 `.section-head` 的 margin-bottom 为 0，
     标题与内容贴在一起；各页另用 18/16/12px 散值拼凑。新增
     `--space-xs/sm/md/lg` 四档令牌统一。浏览器审计 7 个页面 0 间距问题全消。

  **过程中被测试抓出的两个真实缺陷**（都是自己写的代码）：
  - 顶栏恒显「0 个文件 · 0 B」：`users` 表没有聚合列，`/auth/me` 直接外发
    查询结果恒为 0。修在**副本**上赋值（`u` 被 15s TTL 缓存持有，原地改会污染缓存）。
  - 上述修复本身又错了一次：新加的 `TestOwnerUsageAggregates` 立刻发现我用了
    不过滤 status 的 `CountFilesByOwner`（那是给"删账号前提示还有 N 个文件"用的），
    会把回收站文件计入展示口径。改为新增 `ActiveUsageByOwner` 一次查出个数与占用
    —— 两个数字必须同口径同时刻，分两次查会出现"3 个文件 · 只统计了 2 个的大小"。
    已做变异验证：去掉 status 过滤后测试确实失败。
  - `check-responsive.mjs` 会留下孤儿 vite：用 npx 启 vite，vite 是孙进程，
    只 kill(npx) 会让 vite 被 init 收养并继续占端口。危害是下一轮检查**误连旧服务**、
    测的是旧代码却报告通过（PR #10）。改为 detached + 按进程组 kill。

  验证：gofmt / vet / go test / make check 全绿；**CI 在真实 MySQL 8.0 上**
  跑通 0001+0002 迁移与新增的 `TestOwnerPins`、`TestOwnerUsageAggregates`；
  本地另用 go-mysql-server 内存引擎验证置顶排序、按账号独立、幂等、
  双向 CASCADE 清理；真实浏览器确认菜单展开/置顶点击/悬浮菜单二级/拖拽条；
  响应式检查 5 视口通过且确认真的执行（非跳过）；make docs 构建通过。

- **2026-09-23 P16 UI 改版：响应式、明暗外观、去说明性小字**
  参照同组织 Electrical-Manager 重做前端界面。详见 PR #8。

  1. **设计令牌**（新增 `src/styles.css`）：颜色/字号/圆角此前散落在内联 style 里，
     统一为 CSS 变量，明暗两套只覆盖颜色类。顺带修掉缺全局 `box-sizing: border-box`
     导致登录卡在 320px 屏溢出 5px 的问题。
  2. **明暗外观**：三档（跟随系统/浅色/深色），`createThemeOverrides(palette)` 生成两套覆盖，
     改一处同步生效；`index.html` 预置脚本避免深色用户闪白。
  3. **响应式**：桌面侧栏 + 移动抽屉；文件列表桌面表格 / 移动卡片（共用 `useFileActions`）；
     管理端五个页面同样两套。切换用 CSS 媒体查询，首帧即正确。
  4. **去小字**：删掉整段功能解释，改为角标/标签/箭头自解释；异常态（网络不可达）仍保留说明。
  5. **响应式回归测试**（`scripts/check-responsive.mjs` + `mock-api.mjs`）：
     真实浏览器 × 5 视口查横向溢出，一条命令自带 mock 后端与 dev server。

  **这个测试自身修过三次「假通过」**，都值得记住：
  - 起初用无后端的 preview → 内页全被路由守卫重定向到登录页，只测到登录页却报绿
    （现在强制校验 `/api/health` 可达，并断言未落到 `/login`）；
  - 没有服务在跑时页面空白，「无横向溢出」恒成立（现在加了可达性预检，无服务直接失败）；
  - CI 里用 `npm i -g playwright` 装到全局，`import('playwright')` 解析不到 →
    脚本走「跳过」分支却 `exit 0`，**什么都没检查却报绿**。
    改为本地 `--no-save` 安装 + `REQUIRE_PLAYWRIGHT=1`（缺浏览器时失败而非跳过）。

  顺带修掉：FileTable 内建刷新与各视图 slot 刷新按钮重复、卡片标题与顶栏标题重复、
  grid 子项 `min-width:auto` 撑宽导致内容被裁、登录页 `min-height:100%` 未居中。

  验证：typecheck / 单测 / 构建 / make check 全绿；1440、1100、900、390、320 五种视口
  无横向溢出；明暗两套均已用真实浏览器逐页截图核对。CI 日志确认五种视口均实际执行。

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
