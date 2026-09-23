# 局域网文件助手 — 前端（部署在公网）

本目录是**前端静态站点**，构建产物 `dist/` 部署到公网；接口走公司内网的 API 服务
（`../server/`）。两者分离部署、跨域通信。

## 镜像部署（推荐）

CI 会推送固定 tag 到 ghcr：`ghcr.io/sakana-1314/lan-drive:web`。

后端域名在**构建期**由 `HOST` 注入（CI 取仓库变量 `vars.API_HOST`）；
镜像同时内置**运行时注入**：启动时若设置了 `LANDRIVE_API_BASE_URL` 就用它生成
`/config.js` 覆盖，因此同一个镜像也能部署到任意环境，无需重新构建。

```bash
# 用镜像里构建期注入的后端域名
docker run -d -p 80:80 ghcr.io/sakana-1314/lan-drive:web

# 运行时覆盖（无需重新构建镜像）
docker run -d -p 80:80 \
  -e LANDRIVE_API_BASE_URL='https://files.example.com/api' \
  ghcr.io/sakana-1314/lan-drive:web
```

| 环境变量 | 阶段 | 说明 |
| --- | --- | --- |
| `HOST` | 构建期 | 后端域名（仅 origin，不含 `/api`）；留空则走同源 `/api` |
| `LANDRIVE_API_BASE_URL` | 运行时 | 完整 API 地址（含 `/api`），覆盖构建期值；结尾斜杠自动去掉 |
| `LANDRIVE_API_PROBE_TIMEOUT` | 运行时 | 连通性探测超时（毫秒），默认 6000 |

## 构建静态产物

```bash
cp .env.example .env.production
# 编辑 .env.production，填后端域名（仅 origin，不带 /api）：
#   HOST=https://files.example.com
npm install
npm run build        # 产物在 dist/

# 或直接在命令前传，不改文件：
HOST=https://files.example.com npm run build
```

把 `dist/` 的内容放到公网静态托管即可（Nginx / OSS / CDN 均可）。
静态托管时不方便改环境变量，可直接编辑部署后的 `config.js`——它优先于构建期变量。

### API 地址优先级

解析逻辑在 `src/api/index.ts` 的 `resolveApiBaseUrl`：

1. 运行时 `window.__LANDRIVE_CONFIG__.apiBaseUrl`（容器注入的 `/config.js`）
2. 构建期 `HOST`（vite `define` 注入的 `__API_HOST__`，拼成 `${HOST}/api`）
3. 同源 `/api`（适用于用反向代理把 `/api` 转发到内网的场景）

> ⚠️ 后端 `LANDRIVE_CORS_ALLOW` 必须包含本前端被访问的域名，否则浏览器会拦截跨域请求，
> 用户会看到「无法在此网络下使用，请更换网络再试！」。改完前端地址后需要**重新构建**，
> 因为 `HOST` 是构建期注入的（容器部署可用运行时 `LANDRIVE_API_BASE_URL` 免重构建覆盖）。

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
