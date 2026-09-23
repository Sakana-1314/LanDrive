# 前后端分离部署

如果希望员工在**外网也能打开页面**，可以把网页端放到公网，服务端留在内网。

```mermaid
flowchart LR
    subgraph 公网
        W[网页端<br/>静态站点]
    end
    subgraph 公司内网
        A[服务端接口]
        D[(数据与文件)]
    end
    W -- "跨域请求" --> A
    A --> D
```

这样做的好处是：员工在家里打开页面能正常加载，但只有连上公司网络（或 VPN）才能看到文件内容。不在公司网络时，页面会明确提示「无法在此网络下使用，请更换网络再试！」，而不是让人误以为是密码错误。

## 第一步：内网部署服务端

按[部署](./deploy)走完流程，区别只有一处：**把 `docker-compose.yml` 里的 `web` 一段删掉**，只保留 `api`。

```bash
docker compose up -d
```

数据库同样用 1Panel 应用商店安装，不需要单独部署。

::: tip 只跑服务端时，CORS 要填公网网页端的地址
`.env` 里的 `LANDRIVE_CORS_ALLOW` 填**用户在浏览器里打开的网页地址**（即下面第二步里公网站点的域名），而不是内网地址。
:::

## 第二步：公网部署网页端

网页端镜像支持两种连后端的方式，**同一个镜像靠环境变量切换**。

### 方式一：跨域直连（推荐用于分离部署）

浏览器直接请求内网接口的对外地址，需要后端配置 `LANDRIVE_CORS_ALLOW`。

```bash
docker run -d --name lan-drive-web -p 80:80 \
  -e LANDRIVE_API_BASE_URL='https://api.example.com/api' \
  ghcr.io/sakana-1314/lan-drive:web
```

其中 `LANDRIVE_API_BASE_URL` 填**内网服务端的对外地址**（要带 `/api`）。

### 方式二：网页端反代

网页端容器自己把 `/api` 转发到你指定的地址。适合网页端与某台可达服务器之间网络通畅的情形：

```bash
docker run -d --name lan-drive-web -p 80:80 \
  -e LANDRIVE_API_PROXY=api.example.com:8080 \
  ghcr.io/sakana-1314/lan-drive:web
```

两者选一个即可：
- 方式一：浏览器直连接口，**后端必须配 CORS**；
- 方式二：浏览器只访问网页端域名，**不跨域、不用配 CORS**。

::: tip 怎么选
内网接口有独立的对外域名（如 `api.example.com`）→ 用方式一，链路最短。

只想暴露一个域名、或不想维护 CORS 白名单 → 用方式二。
:::

## 第三步：让两边能通

需要满足两个条件：

1. **公网能访问到内网接口** —— 通常通过 VPN、内网穿透或网关转发实现。
2. **服务端放行网页端地址** —— 即 `LANDRIVE_CORS_ALLOW` 必须等于用户浏览器里打开的网页地址。

验证跨域是否配置正确（在内网机器上执行）：

```bash
curl -i -X OPTIONS http://127.0.0.1:8080/api/auth/login \
  -H 'Origin: https://files.example.com' \
  -H 'Access-Control-Request-Method: POST'
```

响应里应该出现：

```
Access-Control-Allow-Origin: https://files.example.com
```

如果没有这一行，说明地址没配对，用户会一直看到「无法在此网络下使用」。

::: tip 常见疑问：为什么在内网能打开页面，却提示换网络？
因为网页端在公网、接口在内网。在内网时两者都通；一旦离开内网，页面还能打开（缓存在浏览器里），但接口请求发不出去，于是提示更换网络。
:::

## 改为同机部署

如果不需要外网访问，按[部署](./deploy)把两个容器放同一台机器上更省事。
