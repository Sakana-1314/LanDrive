# 局域网文件助手 — 前端（部署在公网）

本目录是**前端静态站点**，构建产物 `dist/` 部署到公网；接口走公司内网的 API 服务
（`../server/`）。两者分离部署、跨域通信。

## 镜像部署（推荐）

CI 会推送固定 tag 到 ghcr：`ghcr.io/sakana-1314/lan-drive:web`。

镜像支持两种形态，**同一个镜像靠运行时环境变量切换**，不需要重新构建。

### 形态 A：`/api` 同源反代（推荐）

前端容器内的 nginx 把 `/api` 转发到后端容器。浏览器只访问一个域名：

- **不跨域** → 后端 `LANDRIVE_CORS_ALLOW` 可以留空；
- 后端地址**不出现在浏览器里**（前端走同源 `/api`）；
- 后端容器晚启动也没关系：请求时才解析上游，页面照常可用。

```bash
docker run -d -p 80:80 \
  -e LANDRIVE_API_PROXY=lan-drive-api:8080 \
  ghcr.io/sakana-1314/lan-drive:web
```

### 形态 B：跨域直连

`/api` 不反代，浏览器直接请求内网 API（需要后端配置 `LANDRIVE_CORS_ALLOW`）。

```bash
docker run -d -p 80:80 \
  -e LANDRIVE_API_BASE_URL='https://api.example.com/api' \
  ghcr.io/sakana-1314/lan-drive:web
```

不设置 `LANDRIVE_API_BASE_URL` 时回退到构建期 `--build-arg HOST` 注入的域名
（**官方发布的镜像不传 `HOST`**，因此回退到同源 `/api`）。

### 环境变量

| 环境变量 | 阶段 | 说明 |
| --- | --- | --- |
| `HOST` | 构建期 | 后端域名（仅 origin，不含 `/api`）；留空则走同源 `/api`。官方镜像**刻意留空** |
| `LANDRIVE_API_PROXY` | 运行时 | **反代目标**（`主机:端口`）。设置后启用形态 A，前端自动改用同源 `/api`。留空则关闭反代 |
| `LANDRIVE_API_BASE_URL` | 运行时 | 完整 API 地址（含 `/api`）。启用反代时留空即可；关闭反代时用于形态 B |
| `LANDRIVE_API_PROBE_TIMEOUT` | 运行时 | 连通性探测超时（毫秒），默认 6000 |
| `LANDRIVE_DNS_RESOLVER` | 运行时 | 反代解析上游用的 DNS；默认自动识别 Docker 内嵌 DNS |
| `LANDRIVE_API_PROXY_READ_TIMEOUT` | 运行时 | 反代读超时（秒），默认 600；大文件上传/下载慢时可调大 |

> 反代目标的取值会被校验（拒绝路径、空格、非法端口与换行注入）；
> 非法值会让容器**启动失败并打印原因**，而不是带着坏配置运行。

## 构建静态产物

```bash
cp .env.example .env.production
# 编辑 .env.production，填后端域名（仅 origin，不带 /api）：
#   HOST=https://api.example.com
npm install
npm run build        # 产物在 dist/

# 或直接在命令前传，不改文件：
HOST=https://api.example.com npm run build
```

把 `dist/` 的内容放到公网静态托管即可（Nginx / OSS / CDN 均可）。
静态托管时不方便改环境变量，可直接编辑部署后的 `config.js`——它优先于构建期变量。

### API 地址优先级

解析逻辑在 `src/api/base-url.ts`：

1. 运行时 `window.__LANDRIVE_CONFIG__.apiBaseUrl`（容器注入的 `/config.js`）。
   启用反代时这里就是 `/api` —— **优先级最高**，确保反代不会因为构建期注入而被绕过。
2. 构建期 `HOST`（vite `define` 注入的 `__API_HOST__`，拼成 `${HOST}/api`）
3. 同源 `/api`

> ⚠️ 跨域直连（形态 B）时，后端 `LANDRIVE_CORS_ALLOW` 必须包含前端被访问的域名，
> 否则浏览器会拦截请求，用户会看到「无法在此网络下使用，请更换网络再试！」。
> 用同源反代（形态 A）则不需要配这一项。

::: warning 不要在镜像里烘焙后端域名
镜像会推到**公开**的 ghcr，任何人都能 `docker save` 解包读取前端产物。
一旦在构建期把真实域名写进产物，内网地址就公开了（本项目曾因此泄露过一次）。
官方镜像因此不传 `HOST`，地址一律在**运行时**用环境变量注入。
`web/scripts/check-no-internal-host.sh` 会在构建前扫描并拦住这种事。
:::

### 反代相关的回归测试

```bash
npm run test:entrypoint   # 校验入口脚本与生成的 nginx 配置（含注入、端口校验）
```

装了 nginx 时会额外用 `nginx -t` 验证生成的配置真的能被解析。CI 会执行这一步。


## 开发

```bash
npm run dev          # :5173，已把 /api 代理到本机后端 :8080，无需配置 CORS/HOST
```

开发态用 `.env.development` 的 `VITE_DEV_API_TARGET` 指定代理目标（默认 `http://127.0.0.1:8080`）。

## 网络不可用的提示

`src/api/network.ts` 负责判定「请求未到达服务器」（超时 / 连接失败 / 被 CORS 拦截），
统一提示 **「无法在此网络下使用，请更换网络再试！」**。
该文案由需求指定，改动前请确认。

- 登录页：进入即探测 `GET /api/health`，不可达时在表单上方给出提示；
- 全局：任意接口不可达时跳转 `/network-blocked` 页面（`src/main.ts` 注册）。

## 目录

```
src/
├── api/           接口封装、类型定义、网络可用性判定
├── components/    FileTable 与预览组件（docx/xlsx/pptx/文本）
├── layouts/       主框架（顶栏 + 侧边菜单）
├── router/        路由与守卫（含管理员权限与网络探测）
├── stores/        轻量全局状态（用户、配置、上传策略）
├── utils/         格式化、主题、分片上传引擎
└── views/         页面（登录、文件、上传、预览、个人设置、管理端）
```
