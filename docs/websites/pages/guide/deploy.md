# 部署

本系统由两部分组成，可以部署在同一台机器，也可以分开：

- **服务端** —— 提供接口，保存文件和账号数据。
- **网页端** —— 用户打开的那个页面。

如果只在**公司内网**使用，两部分放同一台服务器即可，最简单。

::: tip 如果还要在外网打开页面
网页端可以单独放到公网，服务端仍留在内网。这样员工在外网能打开页面，但接口访问不到时会提示「无法在此网络下使用，请更换网络再试！」。具体做法见 [前后端分离部署](./deploy-split)。
:::

## 准备工作

需要一台服务器，上面安装 **Docker** 与 **Docker Compose**。

如果服务器不能访问外网，请先在能上网的机器上执行 `docker pull` 把镜像拉下来，再导出到服务器：

```bash
# 能上网的机器
docker pull ghcr.io/sakana-1314/lan-drive:server
docker pull ghcr.io/sakana-1314/lan-drive:web
docker save ghcr.io/sakana-1314/lan-drive:server ghcr.io/sakana-1314/lan-drive:web -o landrive.tar

# 服务器
docker load -i landrive.tar
```

## 一键部署

下载部署文件：

```bash
git clone https://github.com/Sakana-1314/lan-drive.git
cd lan-drive
```

编辑 `docker-compose.full.yml`，**必须修改**下面这几项：

```yaml
services:
  mysql:
    environment:
      MYSQL_ROOT_PASSWORD: "换成你自己的密码"
      MYSQL_PASSWORD: "换成你自己的密码"

  api:
    environment:
      LANDRIVE_MYSQL_DSN: "lanfs:换成你自己的密码@tcp(mysql:3306)/lanfs"
      LANDRIVE_JWT_SECRET: "至少32位的随机字符串"
      LANDRIVE_ADMIN_PASSWORD: "管理员初始密码"
      LANDRIVE_CORS_ALLOW: "http://你的服务器地址"
```

各配置项的含义：

| 配置项 | 说明 |
| --- | --- |
| `MYSQL_PASSWORD` | 数据库密码，自己设定 |
| `LANDRIVE_JWT_SECRET` | 登录凭据的签名密钥，**至少 32 位**，可用 `openssl rand -hex 32` 生成 |
| `LANDRIVE_ADMIN_PASSWORD` | 管理员账号的初始密码，首次启动时使用 |
| `LANDRIVE_CORS_ALLOW` | 允许访问接口的网页地址，即用户浏览器里打开的地址 |

::: warning 关于 LANDRIVE_CORS_ALLOW
这一项填**用户实际访问的网址**（协议 + 域名 + 端口，不要带路径），例如 `http://192.168.1.100` 或 `https://files.example.com`。填错了浏览器会拦截请求，用户会看到「无法在此网络下使用，请更换网络再试！」。
:::

修改完成后启动：

```bash
docker compose -f docker-compose.full.yml up -d
docker compose -f docker-compose.full.yml ps
```

看到容器都是 `running` 或 `healthy` 就成功了。

## 首次登录

浏览器打开 `http://你的服务器地址/`，用管理员账号登录：

- 工号：`admin`（可用环境变量 `LANDRIVE_ADMIN_EMPLOYEE_NO` 修改）
- 密码：你设置的 `LANDRIVE_ADMIN_PASSWORD`

登录后请先到**用户管理**里给同事们创建账号。

## 确认是否正常

服务端提供了健康检查接口，在服务器上执行：

```bash
curl http://127.0.0.1:8080/api/health
```

返回 `"status":"ok"` 表示服务正常。

## 数据备份

需要备份两样东西，**必须一起备份**，否则文件与账号数据会对不上：

```bash
# 1. 数据库
docker compose -f docker-compose.full.yml exec mysql \
  mysqldump -ulanfs -p lanfs > backup-$(date +%F).sql

# 2. 文件
docker run --rm -v lan-drive_file-data:/data -v "$PWD:/backup" alpine \
  tar czf /backup/files-$(date +%F).tar.gz -C /data .
```

::: tip 迁移到新服务器
先装好同样的环境，把上面的 SQL 导入数据库、把文件压缩包解压回数据卷即可。
:::

## 常用维护命令

```bash
# 查看日志
docker compose -f docker-compose.full.yml logs -f api

# 重启
docker compose -f docker-compose.full.yml restart

# 更新到最新版本
docker compose -f docker-compose.full.yml pull
docker compose -f docker-compose.full.yml up -d

# 停止（不删除数据）
docker compose -f docker-compose.full.yml down

# 停止并删除全部数据（谨慎）
docker compose -f docker-compose.full.yml down -v
```

## 下一步

- [使用教程](../usage/login) —— 教同事怎么用
- [管理员指南](../admin/users) —— 用户与系统设置
- [常见问题](./faq) —— 遇到问题先看这里
