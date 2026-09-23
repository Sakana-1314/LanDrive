# 局域网文件助手 — 前端（部署在公网）

本目录是**前端静态站点**，构建产物 `dist/` 部署到公网；接口走公司内网的 API 服务
（`../server/`）。两者分离部署、跨域通信。

## 构建

```bash
cp .env.example .env.production
# 编辑 .env.production：VITE_API_BASE_URL 指向内网 API 完整地址（含 /api）
npm install
npm run build        # 产物在 dist/
```

把 `dist/` 的内容放到公网静态托管即可（Nginx / OSS / CDN 均可）。

> ⚠️ 后端 `LANDRIVE_CORS_ALLOW` 必须包含本前端被访问的域名，否则浏览器会拦截跨域请求，
> 用户会看到「无法在此网络下使用，请更换网络再试！」。改完前端地址后需要**重新构建**，
> 因为 `VITE_API_BASE_URL` 是构建期注入的。

## 开发

```bash
npm run dev          # :5173，已把 /api 代理到本机后端 :8080，无需配置 CORS
```

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
