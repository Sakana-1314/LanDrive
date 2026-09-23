# LanDrive · 局域网文件助手

[![测试](https://github.com/Sakana-1314/lan-drive/actions/workflows/test.yml/badge.svg)](https://github.com/Sakana-1314/lan-drive/actions/workflows/test.yml)
[![构建并推送镜像](https://github.com/Sakana-1314/lan-drive/actions/workflows/build-images.yml/badge.svg)](https://github.com/Sakana-1314/lan-drive/actions/workflows/build-images.yml)
[![镜像](https://img.shields.io/badge/ghcr.io-lan--drive-blue?logo=docker)](https://github.com/Sakana-1314/lan-drive/pkgs/container/lan-drive)

面向**公司内部**的轻量级文件共享网盘。每个员工拥有自己的独立目录，只能上传/修改/删除自己的文件，但可以浏览、预览、下载**所有人**的文件。

- 仓库：<https://github.com/Sakana-1314/lan-drive>
- 镜像：`ghcr.io/sakana-1314/lan-drive:server`（内网 API）、`ghcr.io/sakana-1314/lan-drive:web`（公网前端）

## 部署形态（重要）

本项目采用**前后端分离**，两部分部署在**不同网络位置**：

```
        公网                          公司内网
┌─────────────────────┐        ┌──────────────────────────┐
│  web/ 前端静态站点   │  跨域   │  server/ API 服务         │
│  (Vue 3 构建产物)    │ ─────► │  (Go + Gin，仅内网可达)    │
│  可放 CDN/静态托管   │  /api  │         │                │
└─────────────────────┘        │         ▼                │
                               │      MySQL + 文件存储     │
                               └──────────────────────────┘
```

- **`web/`** — 前端，构建成纯静态文件，部署在**公网**（Nginx、对象存储、CDN 均可）。
- **`server/`** — 后端 API，编译成单一二进制，部署在**内网**，**不对外暴露**。
- 员工在外网打开前端可以加载页面，但调用接口时若不在公司网络，会看到明确提示：
  **「无法在此网络下使用，请更换网络再试！」**

> **最容易出错的一步**：后端 `LANDRIVE_CORS_ALLOW` 必须填前端所在域名，否则浏览器会拦截跨域请求，同样会看到上面那句提示。

## 目录结构

```
.
├── server/                        内网 API（Go + Gin）
│   ├── cmd/landrive/main.go       启动、迁移、播种、维护任务调度
│   ├── internal/config/           环境变量配置与校验
│   ├── internal/model/            领域对象与枚举
│   ├── internal/store/            MySQL 访问 + 内嵌迁移（migrations/0001_init.sql）
│   ├── internal/auth/             bcrypt、JWT、登录限流、用户缓存
│   ├── internal/settings/         系统配置读写、校验与内存缓存
│   ├── internal/storage/          相对路径规范、安全拼接、落盘/合并/哈希/遍历
│   ├── internal/upload/           分片上传（init / 写分片 / 状态 / complete / cancel）
│   ├── internal/files/            文件查询、权限判定、改名、软删除、恢复、彻底删除
│   ├── internal/maintain/         到期标记、物理清理、僵尸会话、日志裁剪、孤儿扫描
│   ├── internal/handler/          HTTP 接口（auth / files / upload / admin）
│   ├── internal/router/           路由装配 + CORS
│   ├── Dockerfile                 仅含 API 的镜像
│   └── go.mod
├── web/                           公网前端（Vue 3 + TS + Vite + Naive UI）
│   ├── src/api/                   接口封装 + 网络可用性判定
│   ├── src/views/                 页面（登录、文件、上传、预览、管理端…）
│   ├── src/components/preview/    纯前端 Office/PDF 预览
│   └── .env.example               API 地址与探测超时配置模板
├── docker-compose.yml             内网：API + MySQL
├── docs/scripts/smoke.sh          部署后端到端冒烟（51 项）
├── docs/                        设计说明、接口契约与运维脚本
└── Makefile
```

## 功能一览

| 模块 | 能力 |
| --- | --- |
| 账号 | 姓名 + 工号（登录名，唯一）+ 密码（bcrypt）；角色分**管理员 / 普通用户** |
| 独立目录 | 每个用户一个目录 `users/<ID>`；只能上传/改名/删除**自己上传**的文件 |
| 全员可读 | 所有登录用户都能浏览、搜索、在线预览、下载任意人的文件 |
| 自动过期 | 上传后默认 **15 天**自动删除（对普通用户不可见），回收站再保留 **7 天**后彻底删除磁盘文件与数据库记录 |
| 体积与类型 | 管理员可配置**单文件最大体积**（默认 500 MB）与**允许的文件类型**（默认全部） |
| 大文件上传 | **分片并发 + 断点续传**：刷新页面、断网、服务重启后都能继续 |
| 在线预览 | Word / Excel / PPT / PDF / 图片 / 文本 / 音视频，全部浏览器内渲染 |
| 网络提示 | 当前网络访问不到内网接口时，明确提示「无法在此网络下使用，请更换网络再试！」 |
| 运维 | 审计日志、统计看板、存储一致性扫描、回收站恢复、上传总开关 |
| 存储一致性 | 数据库中只保存**相对数据根目录**的路径，与磁盘真实结构严格一一对应 |

---

## 部署步骤

### 镜像（CI 自动构建）

推送 `main` 分支后，GitHub Actions 会**自动构建并推送**两个镜像到 ghcr（固定 tag）：

| 镜像 | 用途 | 大小（压缩） |
| --- | --- | --- |
| `ghcr.io/sakana-1314/lan-drive:server` | 内网 API | ~37 MB |
| `ghcr.io/sakana-1314/lan-drive:web` | 公网前端 | ~26 MB |

- **固定 tag 不随版本变化**，部署脚本可长期引用；同时打时间戳 tag（如 `server-20260923-033115`）便于回滚。
- 镜像公开可匿名拉取，`docker pull ghcr.io/sakana-1314/lan-drive:server` 直接可用，无需登录。
- 按变更路径只构建改动过的镜像（改 `server/**` 只重建 server）；也可在 Actions 页面手动触发同时构建两个。

**回滚到历史版本**：

```bash
docker pull ghcr.io/sakana-1314/lan-drive:server-20260923-033115
docker tag  ghcr.io/sakana-1314/lan-drive:server-20260923-033115 ghcr.io/sakana-1314/lan-drive:server
```

### 第一步：部署内网 API

**方式 A：用 compose 拉取现成镜像（推荐）**

```bash
# 1. 修改 docker-compose.yml 中这几处（务必修改）：
#    MYSQL_PASSWORD / MYSQL_ROOT_PASSWORD
#    LANDRIVE_JWT_SECRET      ≥ 32 字符，可用 openssl rand -hex 32 生成
#    LANDRIVE_ADMIN_PASSWORD  初始管理员密码
#    LANDRIVE_CORS_ALLOW      ★ 前端域名，如 https://files.your-company.com

# 2. 拉取并启动（含 MySQL）
docker compose pull app
docker compose up -d
docker compose ps
docker compose logs -f app

# 3. 验证（在内网执行）
curl http://127.0.0.1:8080/api/health
```

**方式 B：从源码构建**

```bash
docker compose up -d --build     # 会用 server/Dockerfile 本地构建
```

**方式 C：直接跑二进制**

```bash
make server
export LANDRIVE_MYSQL_DSN='lanfs:pass@tcp(127.0.0.1:3306)/lanfs'
export LANDRIVE_JWT_SECRET="$(openssl rand -hex 32)"
export LANDRIVE_ADMIN_PASSWORD='change-me'
export LANDRIVE_CORS_ALLOW='https://files.your-company.com'
./bin/landrive
```

数据库迁移在应用启动时自动完成，无需手工导入 SQL。

> **安全**：`8080` 端口只应内网可达。生产环境建议把 `docker-compose.yml` 里的端口映射改成绑定具体内网网卡（如 `"192.168.1.100:8080:8080"`），或用防火墙限制来源网段。**不要**把它暴露到公网。

### 第二步：部署前端到公网

**方式 A：用镜像（推荐，地址无需重新构建）**

`web` 镜像内置运行时注入：启动时用环境变量告诉它内网 API 地址即可，**同一个镜像可部署到任意环境**。

```bash
# 直接用 CI 构建好的镜像（构建期已注入仓库变量 vars.API_HOST 指向的后端域名）
docker run -d --name lanfs-web -p 80:80 ghcr.io/sakana-1314/lan-drive:web

# 若镜像里的默认地址不对，用运行时变量覆盖（无需重新构建）
docker run -d --name lanfs-web -p 80:80 \
  -e LANDRIVE_API_BASE_URL='https://files.example.com/api' \
  ghcr.io/sakana-1314/lan-drive:web

# 自己构建镜像时用 --build-arg HOST 指定后端域名
docker build -f web/Dockerfile --build-arg HOST=https://files.example.com -t lan-drive-web web
```

或写进 compose：

```yaml
services:
  web:
    image: ghcr.io/sakana-1314/lan-drive:web
    restart: unless-stopped
    environment:
      # ★ 内网 API 地址，容器启动时注入，改完重启容器即生效
      LANDRIVE_API_BASE_URL: "http://192.168.1.100:8080/api"
    ports:
      - "80:80"
```

> 换环境（测试↔生产）只改 `LANDRIVE_API_BASE_URL` 并重启，**不用重新构建镜像**。

**方式 B：构建静态产物自行托管**

```bash
cd web
cp .env.example .env.production
# 编辑 .env.production，填后端域名（仅 origin，不带 /api）：
#   HOST=https://files.example.com
npm install && npm run build     # 产物在 web/dist/

# 也可以不改文件，直接在命令前传：
HOST=https://files.example.com npm run build

# 把 dist/ 内容上传到 Nginx / OSS / CDN
```

若公网站点用 Nginx：

```nginx
server {
    listen 443 ssl;
    server_name files.your-company.com;

    root /var/www/lan-drive;
    index index.html;

    # SPA 路由回退
    location / {
        try_files $uri $uri/ /index.html;
    }
}
```

### 第三步：确认跨域打通

前端部署好后，后端 `LANDRIVE_CORS_ALLOW` 必须包含**用户实际访问的域名**（协议+域名+端口，不含路径）：

```
LANDRIVE_CORS_ALLOW=https://files.your-company.com
```

多个域名用英文逗号分隔。改完重启容器：`docker compose up -d app`。

验证方式（在内网机器执行）：

```bash
curl -i -X OPTIONS http://127.0.0.1:8080/api/auth/login \
  -H 'Origin: https://files.your-company.com' \
  -H 'Access-Control-Request-Method: POST'
# 响应里应出现 Access-Control-Allow-Origin: https://files.your-company.com
```

### 网络访问说明

| 场景 | 结果 |
| --- | --- |
| 在公司网络打开前端 | 正常登录使用 |
| 在家/外网打开前端，未连 VPN | 页面能打开，但接口不通 → 提示**「无法在此网络下使用，请更换网络再试！」** |
| 已连公司 VPN | 正常使用（前提是 VPN 能访问内网 API 地址） |
| 前端域名没加进 `LANDRIVE_CORS_ALLOW` | 同样提示上面那句（浏览器拦截了跨域请求） |

---

## 本地开发

需要本机有 Go 1.24+、Node 20+ 与一个可用的 MySQL。

```bash
# 终端 1：内网 API（默认 :8080）
export LANDRIVE_MYSQL_DSN='lanfs:pass@tcp(127.0.0.1:3306)/lanfs'
export LANDRIVE_JWT_SECRET="$(openssl rand -hex 32)"
export LANDRIVE_ADMIN_PASSWORD='admin12345'
make run

# 终端 2：前端开发服务器（:5173，已把 /api 代理到 :8080，无需 CORS）
cd web && npm install && npm run dev
```

开发态走 vite 代理（`.env.development` 的 `VITE_DEV_API_TARGET`），因此既不需要配置 CORS，也不需要设置 `HOST`。

常用命令：

```bash
make server       # 编译内网 API → bin/landrive
make web          # 构建前端静态站点 → web/dist
make build        # 两者都构建
make test         # go test + 前端类型检查
make check        # 再加 gofmt / go vet
make up / down    # 启动 / 停止内网容器
make docker       # 构建 API 镜像
```

---

## 配置项

### 后端（内网 API）

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `LANDRIVE_LISTEN` | `:8080` | 监听地址 |
| `LANDRIVE_MYSQL_DSN` | **必填** | 如 `lanfs:pass@tcp(mysql:3306)/lanfs` |
| `LANDRIVE_AUTO_CREATE_DB` | `true` | 自动 `CREATE DATABASE IF NOT EXISTS` |
| `LANDRIVE_JWT_SECRET` | **必填** | ≥ 32 字符，登录令牌签名密钥 |
| `LANDRIVE_DATA_DIR` | `./data` | **数据根目录**（数据库中的相对路径都相对它） |
| `LANDRIVE_CORS_ALLOW` | 空 | ★ 允许跨域的前端域名，逗号分隔；**为空则不允许任何跨域** |
| `LANDRIVE_ADMIN_EMPLOYEE_NO` | `admin` | 首次启动创建的管理员工号 |
| `LANDRIVE_ADMIN_PASSWORD` | 空 | 首次启动创建管理员所需；`users` 表为空且未设置时启动失败 |
| `LANDRIVE_ADMIN_NAME` | `系统管理员` | 初始管理员姓名 |
| `LANDRIVE_TRUST_PROXY` | `false` | 是否采信 `X-Forwarded-For` |
| `LANDRIVE_LOG_KEEP_DAYS` | `90` | 审计日志保留天数 |
| `LANDRIVE_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |

### 前端（公网静态站点）

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `HOST` | 空 | **后端域名**（仅 origin，不含 `/api`），构建期注入；留空则走同源 `/api` |
| `VITE_API_PROBE_TIMEOUT` | `6000` | 连通性探测超时（毫秒），超时即判定当前网络不可用 |
| `VITE_DEV_API_TARGET` | `http://127.0.0.1:8080` | 仅开发态：vite 代理目标 |

前端构建示例：

```bash
HOST=https://files.example.com npm run build
# 或写入 web/.env.production：HOST=https://files.example.com
```

系统策略（体积、类型、天数、分片、上传开关）在**管理端「系统配置」页面**在线修改，保存后立即生效。

---

## 文件存储结构

数据库中只保存**相对数据根目录**的路径，且与磁盘真实结构严格对应：

```
<LANDRIVE_DATA_DIR>/                     默认 ./data，容器内 /data
├── users/
│   ├── 1/
│   │   ├── 1.pdf                        文件名 = 文件表主键 + 规范扩展名
│   │   └── 2.docx
│   └── 2/
│       └── 3.xlsx
└── tmp/
    ├── chunks/<上传会话ID>/<序号>.part    分片上传的临时分片
    └── orphans/<日期>/<原路径>            一致性扫描发现的孤儿文件（保留 7 天）
```

- 原始文件名**只存数据库**，磁盘文件名不可猜；下载时通过 `Content-Disposition` 还原原始文件名（支持中文）。
- 任何写盘都先算好相对路径再入库；合并失败会回滚已落盘文件，避免「磁盘有、数据库没有」的幽灵文件。
- 管理端「统计看板 → 存储一致性检查」可随时比对数据库与磁盘。

## 文件生命周期

```
上传完成 ──► active ──(上传满 15 天)──► trashed ──(再满 7 天)──► 彻底删除（磁盘文件 + 数据库记录）
               ▲                          │
               └────── 管理员恢复 ◄────────┘
```

- **已标记删除的文件对所有普通用户（含文件属主）完全不可见**，直链也无法访问。
- 只有管理员能在「文件与回收站」页面看到，并选择**恢复**或**立即彻底删除**。
- 清理任务：每 5 分钟标记到期、每 10 分钟物理清理、每小时清理僵尸上传会话、每天 03:30 归档孤儿文件并裁剪 90 天前日志。

## 在线预览支持范围

全部为**浏览器内渲染**，后端只负责把文件以流的方式发给前端，不做任何转换：

| 类型 | 实现 |
| --- | --- |
| `.docx` | `docx-preview` 渲染为 HTML |
| `.xlsx` | `exceljs` 解析后自建表格（超 2000 行 / 100 列会提示截断） |
| `.pptx` | `pptx-preview` 渲染幻灯片 |
| `.pdf` | 浏览器内置 PDF 阅读器 |
| 图片 | `png jpg jpeg gif webp bmp ico tiff` |
| 文本 | `txt md csv log json xml yml ini` 及常见源码后缀，自动识别 UTF-8 / GBK |
| 音视频 | `mp4 webm mov ogg` / `mp3 wav m4a flac aac`，支持拖动进度（后端支持 Range） |
| `.doc` `.xls` `.ppt` | 旧版二进制格式无法在浏览器解析，提示下载后用 Office 打开（另存为新格式即可预览） |
| 压缩包等 | 提示下载后查看 |

安全性说明：只有图片、PDF、音视频会被内联展示；`.svg`、`.html`、`.js` 等可能执行脚本的类型一律以附件形式下发，服务端也从不执行存储的内容。

## 接口速览

完整契约见 [`docs/design.md`](docs/design.md)。统一前缀 `/api`，错误响应恒为 `{"error":"中文提示"}`，登录后通过 `Authorization: Bearer <JWT>` 访问。

| 分类 | 主要接口 |
| --- | --- |
| 运维 | `GET /api/health`（无需登录，前端用它探测网络连通性） |
| 认证 | `POST /api/auth/login`、`GET /api/auth/me`、`POST /api/auth/password` |
| 文件 | `GET /api/files`、`GET /api/files/owners`、`GET /api/files/:id/preview`、`GET /api/files/:id/content`、`GET /api/files/:id/download`、`PATCH /api/files/:id`、`DELETE /api/files/:id` |
| 分片上传 | `POST /api/uploads/init`、`PUT /api/uploads/:id/chunks/:idx`、`GET /api/uploads/:id`、`POST /api/uploads/:id/complete`、`DELETE /api/uploads/:id` |
| 管理端 | `/api/admin/users`、`/api/admin/settings`、`/api/admin/files`（含回收站与恢复）、`/api/admin/logs`、`/api/admin/stats`、`/api/admin/storage/scan` |

---

## 验证

```bash
# 部署后针对内网 API 做端到端冒烟（51 项，含 CORS 校验）
BASE_URL=http://127.0.0.1:8080 \
ADMIN_PW='你的管理员密码' \
ORIGIN='https://files.your-company.com' \
bash docs/scripts/smoke.sh
```

单元测试（不依赖数据库）：

```bash
make test      # go test ./... + 前端类型检查
make check     # 再加 gofmt / go vet
```

数据库集成测试默认跳过，设置 DSN 后启用：

```bash
cd server
LANDRIVE_TEST_MYSQL_DSN='lanfs:pass@tcp(127.0.0.1:3306)/lanfs_test' go test ./internal/store/ ./internal/maintain/ -v
```

---

## 备份

需要备份两样东西，必须**成对备份**，否则数据库与磁盘会不一致：

```bash
# 1. 数据库
docker compose exec mysql mysqldump -ulanfs -p lanfs > backup-$(date +%F).sql

# 2. 文件（数据卷 file-data）
docker run --rm -v lan-file-data:/data -v "$PWD:/backup" alpine \
  tar czf /backup/files-$(date +%F).tar.gz -C /data .
```

恢复时把 SQL 导入数据库、把文件解包回数据卷即可（库里存的都是相对路径）。

---

## 常见问题

**前端页面能打开，但登录提示「无法在此网络下使用，请更换网络再试！」**
说明当前网络访问不到内网 API。依次检查：① 是否连着公司网络/VPN；② 前端构建时的 `HOST` 是否指向内网可达地址（改完必须重新 `npm run build`；用镜像则改 `LANDRIVE_API_BASE_URL` 并重启容器）；③ 后端 `LANDRIVE_CORS_ALLOW` 是否包含前端域名（改完 `docker compose up -d app` 重启）。

**确认在公司网络、地址也对，还是提示网络不可用**
多是跨域被拦截。用浏览器开发者工具的 Network 面板看预检请求；或按上文用 `curl -X OPTIONS` 验证 `Access-Control-Allow-Origin`。注意 `LANDRIVE_CORS_ALLOW` 要写完整来源（`https://` 开头，不带结尾斜杠和路径）。

**已登录状态下突然跳回登录页**
令牌有效期 12 小时，过期后需重新登录；管理员重置密码或停用账号也会使会话立即失效。

**上传提示"该文件类型不被允许上传"**
管理员在「系统配置」里限制了允许类型；清空即可恢复为允许全部。

**旧版 `.doc/.xls/.ppt` 无法预览**
纯前端方案只支持 OOXML 格式。用 Office 另存为 `.docx/.xlsx/.pptx` 后重新上传即可在线预览。

**视频无法拖动进度条**
经反向代理（Nginx）访问时需透传 `Range` 头，Nginx 默认支持。
