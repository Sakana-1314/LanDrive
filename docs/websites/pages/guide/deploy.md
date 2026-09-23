# 部署

本系统由两部分组成：

- **服务端** —— 提供接口，保存文件和账号数据。
- **网页端** —— 用户打开的那个页面。

推荐用 **1Panel** 部署：在面板里装好 MySQL，再用一份编排文件把两个容器跑起来。

编排里网页端默认把 `/api` **反代**给服务端容器，因此：

- 浏览器只访问一个域名，**不跨域**，`LANDRIVE_CORS_ALLOW` 可以留空；
- 后端地址不会出现在浏览器里。

::: tip 只在公司内网使用？
页面和服务端放同一台服务器即可，看这一页就够了。

如果员工还要在**外网**打开页面，网页端需要单独放到公网，见[前后端分离部署](./deploy-split)。
:::

## 准备工作

1. 服务器上已安装 **1Panel**，并能正常使用它的「容器」与「应用商店」。
2. 在 1Panel 的**应用商店**里安装 **MySQL**（本系统的编排文件里不含数据库）。
3. 确认 1Panel 的网络已就绪。本系统会加入 1Panel 自带的 `1panel-network`，这样它才能和
   面板里安装的 MySQL 互相访问。

需要确认网络存在时，在服务器上执行：

```bash
docker network ls | grep 1panel-network
```

如果没有任何输出，在 1Panel 里安装任意一个应用就会自动创建，也可以手动创建：

```bash
docker network create 1panel-network
```

::: warning 服务器不能上外网怎么办
先在能上网的机器上把镜像拉下来，再传到服务器：

```bash
# 能上网的机器
docker pull ghcr.io/sakana-1314/lan-drive:server
docker pull ghcr.io/sakana-1314/lan-drive:web
docker save ghcr.io/sakana-1314/lan-drive:server ghcr.io/sakana-1314/lan-drive:web -o landrive.tar

# 服务器
docker load -i landrive.tar
```
:::

## 第一步：拿到编排文件

```bash
git clone https://github.com/Sakana-1314/LanDrive.git
cd LanDrive
```

只需要 `docker-compose.yml` 与 `.env.example` 这两个文件，其余都是源码。

## 第二步：填写配置

复制一份配置模板，然后按注释填写：

```bash
cp .env.example .env
vi .env
```

**带 ★ 的是必填项**，没填的话启动时会直接报错并告诉你缺哪一项：

```ini
# MySQL 连接串。主机名填 1Panel 里 MySQL 的容器名（同一网络内可直接互通）
LANDRIVE_MYSQL_DSN=lanfs:你的密码@tcp(mysql:3306)/lanfs

# 登录凭据签名密钥，至少 32 位。用 openssl rand -hex 32 生成
LANDRIVE_JWT_SECRET=把这里换成随机字符串

# 管理员初始密码（仅首次启动时使用）
LANDRIVE_ADMIN_PASSWORD=换成一个安全密码

# 网页端把 /api 反代到服务端容器（默认值即可，浏览器因此不跨域）
LANDRIVE_API_PROXY=api:8080
```

`LANDRIVE_CORS_ALLOW` 用上面的默认反代时**可以留空**。

::: warning 只有跨域直连时才需要填 LANDRIVE_CORS_ALLOW
如果关掉了反代（把 `LANDRIVE_API_PROXY` 留空）并让浏览器直接访问接口地址，
就必须把 `LANDRIVE_CORS_ALLOW` 填成**用户实际访问的网址**（协议 + 域名 + 端口，不要带路径），
例如 `http://192.168.1.100` 或 `https://files.example.com`。

填错的后果是浏览器拦截请求，用户会看到「无法在此网络下使用，请更换网络再试！」。
:::

::: tip MySQL 不在同一台机器
把 `LANDRIVE_MYSQL_DSN` 里的主机名换成实际地址即可，例如
`lanfs:你的密码@tcp(10.0.0.5:3306)/lanfs`。
:::

## 第三步：启动

```bash
docker compose up -d
docker compose ps
```

也可以直接用现成的命令，它会先帮你检查网络和 `.env`：

```bash
make up
```

看到两个容器都是 `running` 或 `healthy` 就成功了。数据库表会在首次启动时自动创建，
不需要手动导入 SQL。

## 第四步：在 1Panel 里配置访问入口

进入 1Panel 的**网站**，添加一个反向代理网站，把域名指向网页端：

| 域名 | 反代到 |
| --- | --- |
| `files.example.com` | `http://127.0.0.1:80`（网页端，**只需这一条**） |

接口由网页端容器自己转发给服务端容器（即 `LANDRIVE_API_PROXY`），
所以**不需要再给接口单独配一个域名**。

::: tip 为什么只配一个域名
浏览器访问 `https://files.example.com/api/...` 时，网页端容器的 nginx 会把 `/api` 转发给
服务端容器。从浏览器视角看，页面和接口同源，既不跨域、也看不到后端地址。
:::

如果端口被占用，改 `.env` 里的 `WEB_PORT` / `API_PORT` 即可。

::: tip 只想在内网用
不做反向代理也可以，直接用 `http://服务器IP` 访问。
:::

## 首次登录

浏览器打开你的地址，用管理员账号登录：

- 工号：`admin`（可用 `LANDRIVE_ADMIN_EMPLOYEE_NO` 修改）
- 密码：你填的 `LANDRIVE_ADMIN_PASSWORD`

登录后先到**用户管理**里给同事创建账号。

## 确认服务是否正常

```bash
curl http://127.0.0.1:8080/api/health
```

返回 `"status":"ok"` 表示正常。

## 数据备份

需要备份两样东西，**必须一起备份**，否则文件与账号数据会对不上：

```bash
# 1. 文件（本编排的数据卷）
docker run --rm -v lan-drive_file-data:/data -v "$PWD:/backup" alpine \
  tar czf /backup/files-$(date +%F).tar.gz -C /data .

# 2. 数据库（用 1Panel 的数据库备份功能，或命令行导出）
#    在 1Panel「数据库」页面点备份最省事
```

::: tip 迁移到新服务器
先装好 1Panel 与 MySQL，把数据库导入、把文件压缩包解压回数据卷即可。
因为数据库里存的是相对路径，只要两边目录结构一致就能直接对上。
:::

## 常用维护命令

```bash
# 查看状态
docker compose ps
make status

# 查看日志
docker compose logs -f api

# 重启
docker compose restart

# 更新到最新版本
docker compose pull && docker compose up -d

# 停止（数据保留）
docker compose down

# 停止并删除文件数据（谨慎）
docker compose down -v
```

## 下一步

- [使用教程](../usage/login) —— 教同事怎么用
- [管理员指南](../admin/users) —— 用户与系统管理
- [常见问题](./faq) —— 遇到问题先看这里
