# Todo — 局域网文件助手

实施进度台账。规划与完成事件都追加到 `## Log`（最新在最上方）。

## Now

（全部完成 —— 代码、镜像、文档站均已发布；CI 全绿）

## Next

（无）

## Later

（无）

## Log

- **2026-09-25 P36 支持把文件有效期设为永久（配额 100G 可配置，目录可递归）**

  要求：支持将文件有效期设成永久，但需要二次确认，并提示用户永久空间仅有 100G
  （可配置）；支持把整个文件夹（递归到文件）设成永久。

  **先确认三个会定死实现的点**（都问了需求方）：配额是**全站共享一个池**（不是每人 100G）、
  超限时**直接拒绝并告知差额**（不是只警告）、设永久后**支持改回有期限**（误操作可救）。

  **核心设计：用 NULL 表示永久**（`files.expires_at` 由 NOT NULL 改为可空，迁移 0004）。
  不用 9999 哨兵时间，三个理由：到期扫描是 `expires_at <= now`，哨兵会一直参与比较、
  真到那年整表一起到期；统计口径（7 天内到期、已过期未清理）每处都要记得排除它，
  漏一处就错；而 `shares` 表早就是 NULL=永久 的同一套语义，读起来一致。
  到期扫描天然不命中 NULL（`<=` 不会匹配 NULL）—— "永久"就是这么实现的，不需要额外分支。

  **最容易犯的错（已写进设计文档并用测试锁住）**：后端对永久文件给 `days_left=0`，
  前端若直接按 `days_left` 渲染，**「永久」会显示成「今天到期」** —— 与事实完全相反。
  因此列表先判 `permanent`，并用变异测试盯死（见下）。

  **顺带修正的排序问题**：MySQL 里 NULL 在 ASC 时排最前，于是按到期时间升序时
  「永久」会冒到一堆马上要到期的文件前面，看着像它也要到期了。改为显式
  `expires_at IS NULL` 固定排最后（不用 MySQL 专有的 `NULLS LAST`，两版兼容性不一致）。

  **配额口径**：全站 `active` 且 `expires_at IS NULL` 的 `size_bytes` 之和。
  **刻意不含回收站里的永久文件** —— 那些已设 purge_at、几天内必被清，算进来会让
  "删文件"这个唯一的自救手段不释放额度。代价是删掉到彻底清理之间额度不释放，
  放大倍数受 trash_days 约束，不会无限增长（已写进 design.md 备查）。
  配额 0 = **关闭永久功能**（不是"只允许 0 字节"）。

  **接口**：`GET /api/files/permanent`（二次确认展示"还剩多少"）、
  `PUT /api/files/:id/permanent`、`PUT /api/folders/:id/permanent`。
  超配额 **409**、功能关闭 **403**（提示带差额，用户能照做）。
  目录级用 `rel_path` 前缀匹配 -- 与"统计该目录有多少文件"同一套算法，口径天然一致；
  **目录没有有效期属性**，它只是一次操作的范围，因此只返回受影响文件数、不写 folders 表。

  **测试**（真库 MariaDB 11.8 跑）：
  - 单测：默认 100G、边界（恰好放行 / 超一字节拒）、已用超配额不放行（防减法溢出）、0=关闭。
  - 集成 `TestPermanentExpiryAndQuota`：设永久后 `permanent=true` 且 `expires_at` 为空；
    **到期扫描不碰永久文件**（把另一个文件设成过期来确认扫描仍在工作）；
    配额硬拒且文案含差额、拒绝后文件未被改动；**目录递归影响含子目录在内的 2 个文件**；
    改回有期限按当前保留天数重新计时；配额 0 时设永久 403 但"改回有期限"仍可用。
  - **变异验证 3 项**（都确认如期报错）：跳过配额校验 → 报"应返回 409 实际 200"；
    目录级改成只改当前层 → 报"应影响 2 个文件，实际 1"；
    到期扫描把 NULL 也算进去 → 报"永久文件不应被到期扫描影响，实际 status=trashed"。
  - 浏览器检查：`check-file-list.mjs` 新增一组 —— 永久文件显示「永久」且**不含倒计时文案**、
    有「改回有期限」入口、点「设为永久」必弹二次确认且含空间数字（默认 100G）、
    **取消后文件未被改动**。变异：忽略 permanent 按 days_left 渲染 → 报
    "应显示「永久」，实际「Excel|今天到期」"。
  - **一次自己的操作失误已记录**：变异测试时误用 `git checkout internal/store/files.go`
    把该文件**全部未提交改动**一起回滚了（包含 P35 的 orderBy 等）。发现后按对话记录
    完整重建并跑通全套测试 + 变异。教训：变异验证应该先 `cp` 备份（我确实备份了
    `/tmp/store_files.bak`，但多敲了一条 `git checkout`），**不要对未提交的文件用 git 回滚**。

  **全量回归**：`gofmt` / `go vet` / `go test -count=1 ./...` 全绿（含真库集成）；
  前端 `typecheck` / `npm test` / `build` 全绿；浏览器检查
  （file-list / sidebar / responsive / preview / upload-panel / folder-upload）通过。
  契约已同步 `docs/design.md`（settings 表、files 表、生命周期图、新增「文件有效期」一节、
  FileItem/PermanentStatus/Settings 类型）；用户文档补了
  `usage/files.md`（设为永久 / 改回有期限 / 整个文件夹设永久）与
  `admin/settings.md`（永久空间配额）。

- **2026-09-25 P35 侧栏母 tab「全部文件」改为不可点击（混合视图下线）**

  要求：全部文件这个母 tab 怎么能点击呢？修复。

  **先还原现状**：母 tab 是刻意的"双重控件"——点**文字**去看「全部人员」
  混合视图（`RouterLink` + `stopPropagation`），点**箭头**才展开/收起。
  代码与 `docs/design.md` 都记录了这个取舍。用真实浏览器量了一遍确认行为：
  点标签确实会跳到 `/files` 并高亮（`is-current`），点箭头则收起子项。

  **找根因**：同一个控件承担两个动作，"点下去的预期"是不确定的 ——
  想展开的人被带走、想进列表的人不知道该点箭头还是文字。
  原设计之所以这么拧巴，是因为**母 tab 同时被当成"看所有人的文件"的唯一入口**。

  **先确认再动手**（会改导航结构，问了需求方）：母 tab 只展开/收起后，
  「全部人员」混合视图怎么办？→ 选择**连该视图一起删掉**。

  **改法**：
  - 母 tab 换成纯 `span` 分组标题：内部**没有 `<a>`**，点整行只展开/收起
    （恢复 n-menu 默认行为，不再需要 `stopPropagation` 那套技巧）；
  - `/files` 保留但**只承载 `?owner=<id>`**：不带 owner 时用 `beforeEnter`
    重定向到 `/files/mine`（**不直接删路由** —— 它是登录默认落点、散落在
    书签与预览页返回逻辑里，删了会 404）；
  - 收尾清理：`activeKey` 不再把父级当选中项；`folderNavEnabled` 恒真
    故删掉这个开关；`show-owner` 置 false（整列都是同一个人，纯重复信息）；
    登录默认页 / 404 按钮 / 预览返回 / 管理员越权重定向都改到 `/files/mine`；
    删掉已成死代码的 `.all-files-link.is-current` 样式。

  **测试**：
  - `check-sidebar.mjs` 新增一组「母 tab 不可导航」断言：内部 `<a>` 数必须为 0、
    点它 URL 不能变、但子项数必须变（证明它**仍然**能展开/收起）、
    再点一次要展开回来（否则后续用例全废）。
  - **变异验证**：把母 tab 改回带链接的形态 → 报 4 项
    （内部有链接、点它导航了、子项数没变、连带「点⋯触发键不应导航」也报），
    确认这组断言真能挡住用户报的那个问题。
  - 顺带修正被这次改动带出的**测试脆弱点**：`check-preview.mjs` 原先用
    `/files`（混合视图）去找一份 owner=3 的 .csv，视图下线后它不在那页了 ——
    改为直接去 `/files?owner=3`；`check-sidebar.mjs` 的入口改用 `/files/mine`，
    不再依赖"重定向恰好也能看到侧栏"。

  **全量回归**：`typecheck` / `npm test` / `build` 全绿；6 个浏览器检查
  （responsive 5 视口 / file-list / upload-panel / folder-upload / preview / sidebar）
  全部通过。另用浏览器逐个确认入口：`/files`→重定向到 `/files/mine`、
  `/files?owner=2` 与 `?owner=2&folder=1` 照常、`/upload` 旧书签仍可达、
  预览独立页正常、无 JS 报错。纯前端改动，`server/**` 与接口契约未动。

- **2026-09-25 P34 新增可预览文件大小限制（默认 20MB）**

  要求：新增可预览文件大小限制，默认 20MB。

  **先确认口径**（两处会决定实现形状，先跟需求方对齐再动手）：
  做成**管理员可配置的设置项**（与单文件上限、分片大小同族，缺失时默认 20MB），
  且**所有预览类型都限**，包含图片与音视频。

  **关键设计：这是"预览"的闸，不是"上传"的闸。**
  预览要把整个文件读进浏览器（docx/xlsx/pptx 还要交给纯前端库解析），
  超大文件会把标签页拖死，所以单独设一道闸；**下载始终不受影响**。

  **为什么必须拦两处（本次最重要的一个判断）**：
  只在 `/preview` 的返回值里写 `kind=too-large` 是**不够的** —— 前端拿到的只是一个
  提示，用户完全可以自己扒出 `content_url`（或拿一条旧链接）直接请求 `/content`，
  浏览器照样去渲染那个超大文件，"限制"就只剩一句空话。
  因此登录态与免登录分享的**两个 `/content` 接口各自再判一次**并降级为附件
  （下载能力保留）。这条已写成集成测试的核心断言。

  **实现**：
  - `storage.PreviewKindFor(ext, sizeBytes, maxBytes)` 是**唯一**判定入口
    （放在 storage 而非 handler：登录预览与免登录分享必须共用一套，各写一份必走样）；
    **先判体积再判格式** —— 超限文件无论什么格式一律 `too-large`，
    对超大文件来说"请下载"就是结论，不必再劝用户换格式；
  - 新增 kind `too-large`（不复用 `unsupported`）：两者成因与出路都不同，
    混用会让用户去改格式白折腾；
  - 设置项 `preview_max_size_mb`（0–102400，**0 = 不限制**），进 settings 表、
    Seed 补齐、Patch/校验/缓存全套，管理页「可预览上限」+ 策略角标；
  - 前端 `PreviewView` 把 `too-large` 与"不支持"归入同一分支，**不请求内容**
    （这正是这道闸要防的事），只提示 + 给下载入口；分享页也补了原因说明。

  **测试**：
  - 单测：默认值恰为 20MB、边界（20MB 整可预览、+1 字节不行）、
    **0 表示不限制**（`TestPreviewLimitZeroMeansUnlimited`）、
    `PreviewKindFor` 的顺序与不限制语义（`internal/storage`）；
  - **集成测试**（`TestPreviewSizeLimitBlocksBothPaths`）：登录 `/preview` 与
    免登录分享都返回 `too-large`；两个 `/content` 都降级为附件；
    上限内照旧内联；`/download` 仍可用；上限改回 0 后恢复预览。
  - **变异验证**（3 项，都确认能如期报错）：
    去掉登录态 content 的体积判定 → 报「超限文件必须以附件下发」；
    去掉分享 content 的判定 → 报「免登录 content 必须以附件下发」；
    前端不认 `too-large` 照旧拉 blob → 浏览器检查报「不应请求 /content」。
    第三项尤其重要：它挡住的正是"限制只是个提示、大文件照样进浏览器"这种假实现。
  - **真库跑过**：本机装了 MariaDB 11.8（与生产同大版本），
    `LANDRIVE_TEST_MYSQL_DSN=... go test ./internal/store/ ./internal/maintain/
    ./internal/handler/ ./internal/shares/` 全绿。此前这些集成测试在本机是
    **静默 SKIP**（无 DSN），只跑单测会得到假绿 —— 这次特意补上真库验证。
  - 浏览器检查：`check-preview.mjs` 新增一组（超限页给"上限/下载"文案、
    不渲染舞台与 iframe、**未发起任何 /content 请求**、无 JS 错误）。

  **全量回归**：`gofmt` / `go vet` / `go test ./...` 全绿（含真库集成）；
  前端 `typecheck` / `npm test` / `build` 全绿；浏览器检查
  （preview / sidebar / responsive / file-list / upload-panel / folder-upload）通过。
  接口契约已同步 `docs/design.md`（settings 表、Settings 类型、预览矩阵、
  两个 content 接口说明）与 `web/src/api/types.ts`；使用文档补了
  `admin/settings.md` 与 `usage/preview.md`。

- **2026-09-25 P33 子 tab 的「更多」菜单靠右、改成实心三点**

  要求：全部文件里，子 tab 的菜单按钮要靠右，而且要选用更明显的三个点作为
  icon，让人理解。

  **先量再改**（不靠肉眼猜）：用真实浏览器量了触发键的几何。原状是
  `EllipsisHorizontalOutline`（细线三点、15px）+ 行内样式；**位置根本没靠右** ——
  208px 侧栏里触发键落在 x≈64~83，而标题单元格一直铺到 x=190，只差一点点就要
  贴着姓名（`margin-left` 全靠姓名长度决定），看着像"粘在名字上"。

  **根因**：`n-menu` 把 `extra` 放在**标题单元格内部**，行是 grid
  `auto 1fr auto`，「1fr」是标题列 —— extra 只是那个格子里的**行内流**，
  自然只会紧跟姓名。选项层没有"把 extra 顶到行尾"的能力。

  **改法**：
  - 位置交给 CSS：给子 tab 的行挂 `owner-tab`（`:node-props` 按选项上的
    `ownerTab` 标记下发），`styles.css` 里把标题单元格改成 flex、
    `extra` 用 `margin-left: auto` 吃掉剩余空间 → 触发键贴到行右缘（x=162~190，
    与其它行文字右缘对齐）。桌面侧栏与移动端抽屉共用同一条规则；
  - 图标换成**实心** `EllipsisHorizontal`（三个实心圆点、18px）：细线版在
    这个尺寸下几乎看不清，认不出是"更多"；实心三点是这类菜单的通用形状；
  - 顺手把内联魔法样式换成 `button.owner-menu-trigger`：28px 触控目标、
    圆角走 `--radius-control`、颜色走设计令牌，hover/focus 画出底色与描边
    （之前只是一个裸图标，看不出可以点）。行高仍是 42px，没被撑高。

  **测试**（`scripts/check-sidebar.mjs` 扩了 2 组断言，桌面 + 移动端抽屉各一遍）：
  触发键右缘距行右缘 ≤2px、且必须离姓名右缘 >20px（防退化成"紧跟姓名"）；
  图标必须是 3 个 `fill:currentColor`、r=48 的圆；触控目标 ≥24px；
  抽屉里不得出现横向溢出。
  **变异验证**（关键，否则等于没测）：
  - 去掉 `margin-left: auto` → 报 12 项（桌面 92px、抽屉 132px 都没靠右）；
  - 图标换回 Outline → 报 6 项（3 个圆全是 `fill:none`、r=32）。
  两条都能如期报错。

  **顺带写进文档的一条既有取舍**（这次量出来的，容易被下一个人误判）：
  侧栏折叠成 64px 图标栏后，子项走的是 **`n-dropdown` 弹层**，
  而该弹层**只列姓名链接、连「⋯」触发键都不渲染** —— 也就是说折叠态没有
  置顶入口。这与 `extra` 被 `n-dropdown` 忽略是同一个机制，属既有行为、
  非本次回归，已在 `docs/design.md` 写明。

  **全量回归**：`typecheck` / `npm test` / `build` 全绿；6 个浏览器检查
  （responsive 5 视口 / file-list / upload-panel / folder-upload / preview / sidebar）
  全部通过。其中 `upload-panel` 在 5 个套件**并发**跑时曾报 3 项失败
  （"浮窗收不起/展不开"），单独重跑即通过 —— 确认是并发争抢（同一 dev server
  与浏览器资源）导致的抖动，与本次改动无关，已按单跑结果为准。纯前端改动，
  `server/**` 与接口契约未动。

- **2026-09-25 P32 预览统一骨架的收尾与部署（生产验收通过）**

  P31 的统一骨架合并后按流程部署：PR #28（统一观感）→ 镜像 `sha256:696e8971…`；
  生产验收时又发现**空态/错误态漏了统一**（`.xls` 旧格式提示态仍是自己那套
  内边距，没走统一令牌），遂补 PR #29 → 镜像 `sha256:7663732f…`。

  **合并**：#28、#29 的必需检查 `测试通过` 全绿，且都从 CI 日志确认新增的
  「预览舞台统一」静态检查与「预览弹层检查」**真的执行**并打印通过。

  **部署**（两次都只动 web、`--no-deps`）：
  - 回滚点 `/root/landrive-update-20260925-114657`；
  - 两次都**核对 digest** 与 CI 日志里 push 的完全一致；
  - api 两次都未动（始终 Up 10 hours、digest `fff91c89…` 不变）。

  **生产验收**（经自愈隧道对着真实站点）：
  - 容器 healthy、`/api/health` 200、同源 `/api` 反代通；
  - 新产物进了容器（`preview-stage` 标志落在 `PreviewView-*.js`）；
  - 用**真实文件**逐个量舞台：`.txt` / `.xlsx` / `.pdf` 的舞台底色与内边距
    **完全一致**（`rgb(233,237,243)` + `40px 12px 12px`），深色档 `rgb(28,34,43)`；
  - `.xls` 旧格式提示态补完后也走统一令牌；
  - 数据未动（`/data/users` 仍为 3）。

- **2026-09-25 P31 预览观感统一（各类型共用一套骨架）**

  要求：各种文件的预览效果不统一，改进它。

  **先量再改**：写了一个审计脚本，用同一套 fixture（python-docx / python-pptx /
  exceljs 生成 + 真实 PDF/PNG/文本）通过真实 PreviewView 逐个渲染，量「舞台底色 /
  内边距 / 圆角 / 谁来滚动」四件事。结果确实是四分五裂：

  | 类型 | 改动前的舞台底色 | 问题 |
  |---|---|---|
  | docx | 自绘浅灰 `#e9edf3` | 与 xlsx/text 的全白不一致 |
  | pptx | 自绘深灰 `#3d4148` | **浅色档下是一块深灰**，最刺眼 |
  | xlsx / text | 自绘全白 | 与 docx/pptx 不一致 |
  | pdf | 无舞台 | 唯一没有统一骨架的类型 |
  | image / 音视频 | 又一套（媒体还用了第三块深灰底） | 各写各的 |

  滚动也各写一套：docx 靠 wrap 滚、pptx 库内部滚、xlsx/text 用
  `calc(100dvh - 220px)` / `calc(100dvh - 190px)` 这种**魔数**限制高度。

  **根因**：每个预览器各自决定舞台、留白、滚动，没有统一契约。单看任何一个都合理，
  摆在一起才割裂 —— 这种漂移类型检查和单测都发现不了。

  **改法**：在 PreviewView 里立一个 `.preview-stage` 统一骨架，各预览只画「内容面」：
  - 底色唯一 `--color-preview-stage`；留白唯一 `--preview-gutter/--preview-pad`；
  - 滚动归各类型自己的内容面**内部**（外层舞台不滚，否则纸张/卡片会跟着内容滑走）；
  - 禁用 `calc(100dvh - 魔数)`；
  - 盖掉库的硬编码底：`pptx-preview` 把 wrapper 写成 `background:#000`，
    不覆盖就是「双重舞台」（我们的舞台外再套一圈黑底）。

  改完复测：**7 个可渲染类型的舞台底色与内边距完全一致**
  （浅色 `rgb(233,237,243)`、深色 `rgb(28,34,43)`，padding 都是 `40px 12px 12px`）。

  **过程中发现并修掉两个真缺陷**：
  1. **docx 长文档滚动时「框会跑」**：统一骨架后 docx 一度在外层舞台滚，
     于是白色纸张与两侧留白跟着内容一起滑出视口、文字贴到屏幕边缘。已改为在
     内容面内部滚（与其它类型一致），滚动时卡片框固定。
  2. **pptx 在窄屏被裁**：库的渲染宽度由我们传入，原代码写的是
     `Math.max(640, …)` —— **给宽度设了下限**。390px 手机上幻灯片被硬渲染成
     640px 宽（比容器宽 250px），右侧内容直接被裁掉。宽度必须跟着容器走、
     只设上限。已改为 `Math.max(1, Math.min(clientWidth, 1280))`，
     实测 390px 下滑块宽度 366px、零溢出。

  **测试**（4 项变异验证）：
  - `scripts/check-ui-consistency.mjs`（静态，进 `npm test`）新增两条：
    预览组件不许自带 `--color-preview-stage`、不许出现 `calc(100dvh - …)`，
    且 PreviewView 必须存在用统一令牌的 `.preview-stage`。
    检查前会**剥掉 CSS 注释** —— 否则「不再用 calc(100dvh - 魔数)」这种说明
    文字会被自己误判。
  - `scripts/check-preview.mjs`（真实浏览器）新增：
    ① 把 docx/xlsx/png/pdf/text **真的渲染一遍**（mock 对这些扩展名改回**真实字节**，
    否则 office 类型走不到渲染分支、检查是假绿），断言各类型舞台底色与留白完全一致、
    内容面被撑满；② 390px 下 pptx 不得被裁。
  - 变异验证：pptx 自带舞台底色 → 报错；text 加回魔数高度 → 报错；
    让 xlsx 的舞台 padding 不同 → 报 4 项「留白不统一」；把 640px 下限改回去
    → 报「390px 下幻灯片被裁：幻灯片 640px 宽，舞台只有 390px」。全部如期报错。
  - 新增 `web/scripts/fixtures/`（4 个最小样例，共约 8KB）供 mock 回真实字节。

  **全量回归**：`typecheck` / `npm test` / `build` 全绿；6 个浏览器检查全部通过。

- **2026-09-25 P30 合并 P28/P29 并部署到内网部署机（生产验收通过）**

  要求：把 P28（预览弹层 iframe + 深色）与 P29（侧栏 tab 与置顶下拉）合并后部署。

  **合并**：按仓库惯例拆成两个 PR（一提交一 PR）。
  - PR #25 P28 预览弹层：必需检查 `测试通过` 全绿，且从 CI 日志确认新增的
    「预览弹层检查」步骤**真的执行**并打印通过（不是被跳过）；
  - PR #26 P29 侧栏：「⋯」下拉与 tab 文案。P28 合并后 PR #26 与 `main` 冲突
    （两边都改了 `package.json` / `Makefile` / `test.yml` / `todo.md`）——
    冲突都是「各自新增一段」的加性冲突，rebase 到新 `main` 后按「两边都保留」解决，
    并在本地把 `typecheck` / `test` / 两个浏览器检查 / `build` 全跑一遍才 force-push。
    **注意 `MainLayout.vue` 是自动合并的**，所以专门确认了 P28 的 `PreviewModal`
    与 P29 的 `OwnerMenu` 两处改动都还在。

  **镜像**：`web/**` 变更触发 `build-images.yml`，`构建 server 镜像 = skipped`
  （只重建 web，符合预期）。web digest `sha256:675c951a…`。

  **部署**（只动 web，`--no-deps`，避免 api 跑迁移）：
  - 回滚点 `/root/landrive-update-20260925-104504`（compose 副本 + 更新前镜像清单）；
  - `docker compose pull web` → `up -d --no-deps web`；
  - **核对 digest**：部署机上 `sha256:675c951a…` 与 CI 日志里 push 的**完全一致**
    （只看 CI 绿灯不算验证）；
  - 更新与未变动的对照：web `df6d1461…` → `675c951a…`，api 保持 `fff91c89…` 未动。

  **生产验收**（经自愈隧道对着真实站点，不是对着本地）：
  - 容器 healthy、`/api/health` 200、同源 `/api` 反代通（`LanDrive-Web` 健康、
    `LanDrive-API` 仍显示 Up 9 hours、digest 未变）；
  - 新产物确实进了容器：`MainLayout` 指纹变了（`-DLMqpgOE` → `-BCvKe_89`），
    且该产物里同时含 P28 的 `preview-shell` 与 P29 的「更多/置顶」；
  - 真实浏览器（真实 admin token）验收：侧栏子 tab **只有姓名、无计数**，
    右侧触发键 title 是「更多」，点开下拉只有「置顶」一项；
    点文件「预览」**新开标签数 = 0**，弹层出现、`iframe src=/preview/27?embed=1`、
    `legacyBar(旧顶部栏) = false`、有悬浮关闭键；深色档下 iframe 内
    `data-theme=dark`、预览面底色 `rgb(28,34,43)`；
  - 真实文件逐个渲染：`c.txt` 正文出来了、xlsx 表格渲染正常（`sheet` + `n-tabs`）、
    PDF 走 blob + 内层 iframe（526660 字节）。
  - **数据未动**：`/data/users` 目录数仍为 3，用户文件未被触碰（只读验收，
    除登录与预览外未产生任何写操作）。

  **踩到的坑（值得记）**：
  1. **隧道会把慢当坏**：PDF（526KB）经隧道要 **10 秒**才取完，初次只等 12 秒，
     页面还在「正在加载文件内容…」，看着像预览坏了。放长等待后确认
     blob 字节数完全正确、内层 iframe 1325×828 正常。教训与 P27 一致：
     **穿透场景下的异常先排除「链路慢/断了」，再怀疑代码**。隧道这次也断了 3 次
     （`ERR_CONNECTION_REFUSED` / `ERR_EMPTY_RESPONSE`），靠自愈重连循环恢复。
  2. **headless Chromium 不绘制内置 PDF 查看器**，截图里 PDF 区域是全白的；
     要判定 PDF 是否真的加载，得量 blob 大小与内层 iframe 尺寸，不能看截图。

- **2026-09-25 P28 预览改为列表页内弹层 + iframe 内嵌，并适配深色**

  要求：预览改成 **iframe 内部预览**、**去掉顶部的名称与下载按钮**，并**适配深色模式**。

  **原状**：点「预览」是 `window.open` 新开一个标签页，页面顶部有一条卡片栏
  （文件名 + 类型/大小标签 + 下载按钮）；预览页与四个预览组件的样式里散着
  `#fff` / `#f5f7fa` / `#525659` / `#f7f8fa` 等写死的浅色，深色档下是一块刺眼的白。

  **改法**：预览入口全部改成「把文件 id 写进全局单例 `stores/preview.ts`」，
  由挂在 `MainLayout` 上的 `PreviewModal.vue` 弹出铺满视口的弹层，弹层内是
  `src="/preview/<id>?embed=1"` 的 iframe；预览页据此不再渲染返回键等自身 chrome，
  也没有任何标题栏 —— 唯一的退出方式是两个悬浮关闭键之一（弹层右上角 / Esc）。

  **两个刻意的技术取舍**（都做了真实浏览器验证，不是拍脑袋）：
  - **iframe 必须用同源真实文档，不能用 `srcdoc` + Vue `Teleport`。**
    先按 srcdoc 方案写完并跑通了：内容能渲染、事件也能点。但实测深色档下
    Teleport 出去的内容**拿不到 Naive 的 CSS 变量作用域** —— `.n-alert` 的文字
    仍是 `rgb(0,0,0)`，而同一页面外层的 Naive 已经跟着主题变了。根因是 Naive
    把每个组件的 CSS 变量挂在**自己所在文档**的样式表上，跨文档 Teleport 后
    作用域对不上。换成同源真实路由后，iframe 自己跑一遍 SPA 启动，
    Naive 与明暗主题天然生效（实测 iframe 内 `data-theme="dark"`、
    预览面底色 `#1c222b`）。
  - **「纸张是内容，舞台是 UI」**：docx/pptx 库渲染出的白纸保持白色 ——
    那是文档自身的样式（Word 页面本来就白底黑字），跟着界面变深反而失真；
    但纸张**外面**的舞台底色、滚动区、边框必须走令牌。为此在 `styles.css`
    新增了一组预览专用令牌（`--color-preview-stage/paper/backdrop/code-bg/
    code-border/slide-backdrop`），明暗两档都覆盖。

  **顺带修掉两个真缺陷**：
  1. `DocxPreview.vue` 覆盖纸张样式的选择器写的是 `.docx-wrapper`，
     而 `renderAsync` 传的 `className: 'docx-preview-root'` 会让实际类名变成
     `.docx-preview-root-wrapper` / `section.docx-preview-root` —— **这段覆盖从来
     没生效过**（一直是库内置的 `background: gray` 在生效）。已改成按实际类名匹配。
  2. 悬浮关闭键压住文本预览右上角的「复制全部」：深色档下量出来只差 8px，
     浅色档出现滚动条后布局左移、两者**真的叠上**，那个按钮就点不动了
     （截图时发现）。已在嵌入态给右上角留 44px 安全区。

  **测试**（4 项变异验证，不是假绿）：
  - 新增 `scripts/check-preview.mjs`（真实浏览器，接入 `npm run test:preview`
    与 CI）：点预览**不新开标签**（`page.on('popup')` 计数）、弹层内**恰好一个
    iframe** 且 src 指向 `/preview/<id>?embed=1`、顶部栏 `.preview-bar` 归零、
    弹层内不出现「下载」、**iframe 里真的渲染出文件正文**（挑一个必定能渲染的
    `.csv` 断言正文文本 —— 只断言「有字」会被错误提示蒙混过去）、嵌入态不渲染
    返回键、关闭键与内容右上角控件不重叠且点它不会误关弹层、深色档 iframe 内
    也是深色且预览面底色不过亮、无 JS 报错。
  - 变异验证：把 `openPreview` 改回 `window.open` → 报「无弹层」；
    把旧顶部栏加回来 → 报「iframe 内还残留顶部栏」；把预览面底色写死 `#ffffff`
    → 报「深色档下预览面底色过亮」；把内容接口改成返回空正文 → 报「没有渲染出
    文件正文」；去掉 44px 安全区 → 报「关闭键与右上角控件重叠」。5 项全部如期报错。
  - `mock-api.mjs` 补了 `/api/files/:id/preview` 与 `/api/files/:id/content`
    两个接口（kind 判定口径与 `server/internal/storage.PreviewKind` 对齐），
    否则弹层打不开、检查测不到真实路径。

  **全量回归**：`typecheck` / `npm test` / `build` 全绿；5 个浏览器检查
  （responsive 5 视口 / file-list / upload-panel / folder-upload / preview）全部通过。
  纯前端改动，`server/**` 与接口契约未动。
- **2026-09-25 P29 侧栏用户 tab 不再显示文件数，置顶改为下拉勾选**

  要求：tab 不展示用户名下的文件数量；置顶按钮改成菜单下拉勾选。

  **原状**：子 tab 文案是 `姓名 · 文件数`；右侧是一个行内**图钉按钮**，
  点一下切换置顶，当前状态只能靠图标形状（实心/空心）与悬停提示区分。

  **改法**：文案改成只显示姓名；右侧换成「⋯」触发键 + `n-dropdown`，
  菜单里是「置顶」项，已置顶时显示对勾，触发键本身也点亮（不展开菜单也能看出状态）。
  「名下没有文件的账号不列出」这条既有过滤保持不变。

  **踩到的坑（真问题，值一提）**：第一版把勾选态写在菜单项的 `extra` 字段上，
  因为顶栏「外观」子菜单就是这么写的、看着像现成范式。跑起来**不报错、
  类型检查也通过，但勾根本不显示** —— `extra` 是 **`n-menu` 专有**字段，
  `n-dropdown` 的 option body 只渲染 `prefix/label/suffix`，压根不读 `extra`。
  只有真浏览器点开菜单才发现"点了置顶却没勾"。已改为把勾画在 `label` 里。

  **顺带发现**：顶栏「外观」子菜单的勾选态用的是**同一个写法**，因此它的
  「当前档位对勾」其实也**从来没渲染过**（一直是靠点击后的主题变化体现）。
  本次未动它（不在需求范围内），但已在 `docs/design.md` 里把这条约定写清楚，
  避免下一个人照着错的范式再抄一遍。

  **测试**（2 项变异验证）：
  - 新增 `scripts/check-sidebar.mjs`（真实浏览器，接入 `npm run test:sidebar`
    与 CI）：子 tab 文案不含计数、不含数字；右侧触发键 title 是「更多」（不再是图钉）；
    点「⋯」能展开菜单且**不触发导航**；未置顶无对勾 → 点置顶后顺序真的前移 →
    菜单里出现对勾 → 再点一次取消后顺序回到原样。
  - 变异验证：把 `姓名 · 文件数` 加回去 → 3 项报错；把勾选态改回 `extra`
    → 报「已置顶时应有对勾（n-dropdown 不读 extra）」。两项都如期报错。

  **全量回归**：`typecheck` / `npm test` / `build` 全绿；6 个浏览器检查
  （responsive / file-list / upload-panel / folder-upload / preview / sidebar）
  全部通过 —— 其中 `check-upload-panel.mjs` 是靠**点侧栏链接做 SPA 导航**的，
  改动侧栏后确认它仍通过（导航没被破坏）。纯前端改动，接口契约未动。

- **2026-09-25 P27 文件夹与文件同列展示，对齐 Windows 资源管理器（PR #24）+ 部署**

  要求：**文件夹不要单独放上面**，要像 Windows 文件系统那样在同一个列表里列出来。

  **原状**：`FilesView` 里子文件夹是表格**上方单独的一坨卡片**（`.folder-grid`），
  文件是另一个列表 —— 同一个目录的两类东西被拆在两处，用户得上下扫两遍才知道
  这一层有什么，而且两块的视觉语言完全不像同一个东西。

  **改法**：删掉那坨卡片区，行模型抽成纯函数 `web/src/utils/fileRows.ts`，
  由 `FileTable` 把「当前目录的子文件夹」与「当前页文件」合成**一个列表**渲染
  （桌面表格与移动端卡片吃同一份行）。三个刻意的取舍：
  - 文件夹**只在第 1 页**列出：文件由服务端分页、文件夹不是（`listFolders` 一次
    返回该层全部），每页都渲染会出现"翻页后内容重复"的错觉。
  - 文件夹**按名称本地排序**，不跟服务端文件排序器走（`sort`/`order` 只是文件查询
    参数，跟着变会与服务端结果对不上、表头箭头会骗人）。排序口径显式分桶：
    数字/字母在前、中文在后，档内 `numeric`（报表2 < 报表10）。直接
    `localeCompare('zh-Hans-CN')` 会把 `AB` 排到「文档」后面，与资源管理器观感不符
    —— 这条是写单测时才发现的（第一版断言按 localeCompare 写，跑出来是错的）。
  - 搜索时按**文件夹名**本地匹配（服务端 `q` 只作用于文件）：全藏会让用户搜「报表」
    时进不去那个叫「报表」的目录，全留又会掺入无关目录。

  界面细节：文件夹行有底色、名称加重、大小列显示「N 项」而不是体积、到期列 `—`；
  操作列 打开/分享/重命名/删除（文件行仍是 预览/下载/重命名/删除）；
  移动端文件夹卡片加了进入箭头（没有静态「打开」按钮，光靠可点击看不出来）；
  分页汇总改为「共 N 个文件 · M 个文件夹」（分页只对文件生效）。

  **顺带修掉一个自己引入的契约回归**：改「名称」列时把 key 从 `original_name`
  改成了 `name`，而排序键会原样发给服务端（`internal/store/files.go` 的
  `sortColumn`），服务端认不出就**静默掉回 `created_at`** —— 表面能排、实际排错，
  类型检查与单测都发现不了。已改回 `original_name`，并把「按名称排序时请求里
  必须带 `sort=original_name`」写进浏览器测试钉住。

  **测试**（5 项变异验证，不是假绿）：
  - `fileRows.spec.ts`（纯函数，接入 `npm test`）：文件夹在前、只第 1 页、名称排序
    （数字/字母在前）、numeric 比较、行 key 唯一（文件夹与文件 id 是两个自增序列，
    可能相同）、搜索按名称匹配且忽略大小写。
  - `check-file-list.mjs`（真实浏览器，接入 `npm run test:file-list` 与 CI）：
    同一个表格里文件夹行在文件行**之前**、`.folder-grid` 残留为 **0**、文件夹显示项数、
    操作列齐备、分页汇总区分文件与文件夹、**点文件夹行真的进目录**（URL 带 `folder=`、
    面包屑更新）、搜索「报表」保留该目录且滤掉「文档」、移动端同样混排且带箭头、
    390px 不横向溢出、排序键仍是 `original_name`、无 JS 报错。
  - 变异验证：文件夹排到文件之后报 4 项；每页都渲染文件夹 → 单测失败；
    搜索不按名称过滤报 1 项；名称列 key 改名报 1 项；文件夹隐藏不渲染报多项。

  **发布与部署**：PR #24 的必需检查 `测试通过` 全绿，且已从 CI 日志确认新增的
  「文件列表形态检查」步骤**真的执行**并打印通过。`web/**` 变更触发镜像重建，
  新 web digest `sha256:df6d1461…`（与构建日志一致），按流程只更新 web
  （`--no-deps web`）：`e8c973b3…` → `df6d1461…`，api 未动。
  回滚点 `/root/landrive-update-20260925-062406`。

  **生产验收**（经隧道，对着真实站点）：容器 healthy、容器内含新文案
  （`共 ` 落在 `FileTable-*.js`）。真实浏览器（真实 token 登录）在「我的文件」看到
  **一个列表**：文件夹行 `001.备件申报` 显示「3 项」+ 打开/分享/重命名/删除，
  分页显示「共 0 个文件 · 1 个文件夹」；点这一行进入 `?folder=10`、面包屑变成
  `我的文件 / 001.备件申报`，目录内三个子文件夹同样以列表行呈现；移动端同一形态、
  带进入箭头、无横向溢出、无 JS 运行时错误。**未产生任何测试数据**（只读验收）。

  **踩到的坑**：隧道这次断了三次（`ERR_CONNECTION_REFUSED`）。其中一次断在
  页面导航中途，playwright 报的是 `Unable to preload CSS for
  /assets/NetworkBlockedView-*.css` —— 看起来像产物缺文件，实际那个 CSS 就在容器里
  （`ls` 确认存在），是隧道断连造成的假象。教训：**穿透场景下的报错要先排除"链路断了"，
  再怀疑代码/产物**，否则会去修一个不存在的问题。

- **2026-09-25 P26 右下角常驻上传进度浮窗，逐文件实时看进度（PR #23）+ 部署**

  需求：**目前没法看到上传文件的进度**，要在右下角做浮窗实时看每个文件的进度。

  **根因（先查再改）**：不只是"没做进度条"—— 队列状态原本由「我的文件」页面组件
  （`useUploadQueue`）持有，组件一卸载队列就从界面上消失。也就是说用户切到别的页面后
  **既看不到进度，也没有取消入口**（上传本身在后台还在跑）。所以真正的改动是把队列的
  归属从页面搬到全局。

  **改法**：
  - 队列状态抽到全局单例 `web/src/stores/upload.ts`（`reactive` 单例，不引入 Pinia）；
    `UploadManager` 的完成/失败提示也改由浮窗统一发出 —— 只有浮窗常驻，
    提示挂在页面里会随组件卸载一起消失（两处都发则会重复提示）。
  - 新增 `web/src/components/UploadPanel.vue`，挂在 `MainLayout` 上：右下角常驻，
    **逐文件**显示文件名（含相对目录）、状态、已传/总字节、进度条；顶部一条按**字节**
    折算的总体进度（按任务数算会在传大文件时虚高）。
  - 汇总口径抽成纯函数 `web/src/utils/uploadSummary.ts`（总进度、汇总速度、剩余时间、
    标题优先级），由单测钉住；`UploadQueue` 的状态文案也改为复用它，不再两处各写一套。
  - `FilesView` 不再渲染队列，只保留两个上传入口。
  - 交互：空队列不渲染；全部传完自动收起为一行；**有失败时保持展开**（失败不能被
    自动折叠藏起来）；折叠状态记 localStorage；又有新一批开始传时自动展开。
  - 只有**切账号**才清队列（`bindUploadUser` 按工号识别），同一账号在目录间切换不清 ——
    否则传着文件切个子目录，进度就没了（这是本改动最容易踩的坑，已进回归测试）。

  **测试**（4 项变异验证，不是假绿）：
  - `uploadSummary.spec.ts`（纯函数，接入 `npm test`）：按字节折算（大文件不能因任务数
    过半就报 100%）、速度只汇总进行中的任务、标题优先级（失败 > 进行中 > 已完成）、
    取消算"已了结"而不是失败、空队列不产生 NaN。
  - `check-upload-panel.mjs`（真实浏览器，接入 `npm run test:upload-panel` 与 CI）：
    逐条进度条的 `max-width` **真的在涨**、SPA 切页后**仍在传**、回到「我的文件」队列
    **不被清空**、传完自动收起、**失败不被收起**、空队列不渲染、390px 不横向溢出。
    变异验证：把浮窗从布局删掉报 9 项失败；停止刷新任务列表报 9 项失败；
    总进度按任务数算报 8 项失败；有失败也自动收起报 3 项失败。
    （第一版断言只读标题文字，把总进度条写死成 0 也能报绿 —— 已改为读进度条本体。）
  - `check-folder-upload.mjs` 随队列位置同步（全部传完会自动收起，必须先展开再读条目），
    并去掉一处失真注入（真实浏览器拖入文件夹时 `dataTransfer.files` 是**空的**，
    塞一个条目进去等于测一个真实不存在的输入）。
  - `mock-api.mjs` 补分片上传接口（分片带延时，否则进度来不及被观察）；
    文件名含「失败」的会话固定返 500，用来覆盖失败态。

  **发布与部署**：PR #23 的必需检查 `测试通过` 全绿，且已从 CI 日志确认新增的
  「上传进度浮窗检查」步骤**真的执行**并打印通过（不是跳过）。`web/**` 变更触发镜像
  重建，新 web digest `sha256:e8c973b3…`（与构建日志一致），按流程只更新 web
  （`--no-deps web`）：`69449193…` → `e8c973b3…`，api 未动。
  回滚点 `/root/landrive-update-20260925-035341`。

  **生产验收**（经隧道，对着真实站点，不是本地）：容器 healthy、`/api/health` 通、
  同源 `/api` 反代通；容器内产物含新文案（`正在上传`/`个文件上传失败` 落在
  `MainLayout-*.js`）。真实浏览器（真实 token 登录）传一个 8MB 文件：浮窗出现在
  右下角且在视口内、列出该文件、进度从 0% 涨到 50%（995 KB/s · 剩余 4 秒）、
  传完变「已完成 1 个上传」并自动收起、切到「分享管理」后展开仍能看到该任务，
  **无 JS 运行时错误**。验收产生的文件已彻底删除，线上数据回到改动前
  （用户 2 / active 12 / 回收站 2）。

  **踩到的坑**：内网穿透隧道在验收中途断了一次（`ERR_CONNECTION_REFUSED`）；
  自愈隧道脚本恢复后重跑即通过 —— 与 P25 记录一致，远程/穿透场景要按"会断"来设计。

- **2026-09-25 P25 修复「上传文件夹只传了个同名空文件」（PR #22）+ 部署**

  现象：从系统拖入一个文件夹，只上传了一个与文件夹同名的 0 字节文件，里面的文件全丢。

  **根因（先复现再改）**：从系统拖入文件夹时，`dataTransfer.files` 里**只有那个文件夹
  本身**（一个 0 字节的 `File`）。旧代码直接读 `files` 就入队，所以"文件夹"被当成
  空文件上传。真实浏览器复现抓到现场：
  `POST /uploads/init {"file_name":"报表","file_size":0,"folder_id":0}`。
  目录内容只能经 `DataTransferItem.webkitGetAsEntry()` 递归读取 —— 该接口浏览器支持
  （实测 `hasGetAsEntry: true`），但代码从未调用；`<input webkitdirectory>` 那条
  路径也没读 `webkitRelativePath`。

  **改法**：
  - 新增 `web/src/utils/uploadEntries.ts`：把拖放/选择到的条目展平成「文件 + 相对目录」。
    目录用 `webkitGetAsEntry` 递归（`readEntries` 每次最多 100 条，必须循环读到空），
    选目录读 `webkitRelativePath`；非法目录名跳过而不是让整棵树失败。
  - `PageDropZone` 的 drop 改走该函数（异步递归）。
  - `useUploadQueue`：按层级分组，逐级**解析/创建**目录（同名复用，避开 409），
    再把文件按各自目标目录入队。
  - `upload.ts`：目标目录改成**每个任务自带**（`folderId`/`dirs`）—— 同一批拖入的
    文件分属不同层级，用 manager 上的共享字段会被后一个覆盖。
  - 新增「上传文件夹」按钮 + `webkitdirectory` 输入（移动端没有拖拽也能传整个目录）。
  - `UploadQueue` 显示「相对目录/文件名」，同名文件不再分不清。

  **顺带修掉一个回归测试的假通过**（这次工具条裁剪就是被它漏掉的）：
  `check-responsive` 判断"元素是否越界"时一见 `overflowX:scroll` 的祖先就跳过，
  于是被 `overflow:hidden` 祖先**裁掉**的元素一路报绿
  （390px 下工具条第三个按钮只剩一半，而 `document.scrollWidth == innerWidth`）。
  改为区分「可滚动祖先」（跳过）与「裁剪祖先」（超出右边界即报错）。
  新工具条也因此允许换行（三个按钮共 368px，窄屏折行后完整可见）。

  **测试**（两条都做了变异验证，不是假绿）：
  - `uploadEntries.spec.ts`（纯函数）已接入 `npm test`：递归展开、相对层级、
    目录名校验、条目上限、空目录不入队、不支持 entry 时的回退。
  - `check-folder-upload.mjs`（真实浏览器）已接入 `npm test:folder-upload` 与 CI：
    断言队列是 3 个真实文件、**没有"同名文件夹条目"**、按层级建出目录
    （`parent_id` 正确）、文件各落其目录。还原旧行为会报 **11 项**失败；
    响应式守卫还原 `flex:none` 会报出"元素被裁剪祖先裁掉"。

  **发布与部署**：PR #22 合并 `4173f36`（必需检查与新增的上传文件夹检查均通过，
  已从 CI 日志确认该步骤真的执行）；web 镜像 digest `sha256:69449193…`
  （与构建日志一致），按流程只更新 web（`--no-deps web`）：
  `a1ce57d8…` → `69449193…`，api 未动。回滚点
  `/root/landrive-update-20260925-025833`。

  **生产验收**（经隧道，真实站点）：容器内产物含「上传文件夹」与 `webkitGetAsEntry`；
  登录后用真实 token 拖入 `验收<时间戳>/子层/{a,b}.txt` + `c.txt`，结果：
  建出 `验收…`(id 8) → `子层`(id 9, parent 8)，`a/b.txt` → folder 9、`c.txt` → folder 8，
  队列 3 个文件、无报错。**验收产生的测试目录已删除**，线上数据回到
  根目录 0 个 / 用户 2 / 回收站 2（与改动前一致）。

- **2026-09-25 P24 换用新 logo + 补齐站点图标（PR #21）+ 部署**

  要求：把 logo 换上，并先对图片做预处理（加圆角）。

  **logo**：侧栏/抽屉、登录页、分享页三处品牌位原先是 Naive 图标
  （`folder-open-outline`/`cloud-outline`）套一个渐变圆角色块，现在统一换成 `logo.png`。

  **图片预处理**：用 SVG 圆角矩形做 `dest-in` 遮罩，把圆角**切进像素**得到透明圆角
  （实测四角 `alpha=0`）。关键点：**在原始 1254px 上先切圆角、再缩放** —— 反过来
  先缩放会得到毛边圆角。因此同步**删掉**了原来给色块用的 `border-radius` 与
  `--gradient-brand` 底（留着等于给图片再套一层遮罩，会切出双圆角）。生成脚本与源图
  放在 `.private/logo/`（不提交）。

  **补齐站点图标**（此前完全缺失）：`favicon-16.png` / `favicon-32.png` /
  `favicon.ico`（16/32/48 三个尺寸，内嵌 PNG 负载）/ `apple-touch-icon.png`(180)，
  并在 `index.html` 按「png 优先、ico 兜底」声明。

  **验证**：`typecheck` / `npm test` / `build` / `test:responsive`(5 视口) /
  `test:entrypoint` 全绿；PR #21 合并（必需检查 `测试通过` 通过）。

  **部署**：`web/**` 变更触发镜像重建，新 web digest `sha256:a1ce57d8…`（与构建日志
  一致），按流程只更新 web（`--no-deps web`）：`faed6ac7…` → `a1ce57d8…`；
  api 未动。回滚点 `/root/landrive-update-20260925-021425`。

  **生产验收**（经隧道，不是本地）：五个图标资源在线均 200；容器内 `logo.png`
  md5 与仓库**逐字节一致**（`06f00a6c…`）；真实浏览器（登录后用真实 token）确认
  桌面侧栏(1280)与移动抽屉(390) 在明暗两档下 logo 均正确渲染
  （`naturalWidth=256`、四角 alpha=0 即圆角生效、无 JS 报错）。

  **踩到的坑**：验收脚本第一次用固定端口 5199 起 dev server，而该端口被同工作区
  另一个项目（`备件管理系统`）的 vite 占着，测出来的登录页是「电气无忧」——
  **不是本项目**。改为动态选端口并断言页面标题含「局域网文件助手」后才拿到真实结果。
  教训：跨会话共用开发机时，固定端口不可靠，**必须断言测的是哪个应用**。

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
