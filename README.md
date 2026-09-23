# LanDrive · 局域网文件助手

[![测试](https://github.com/Sakana-1314/LanDrive/actions/workflows/test.yml/badge.svg)](https://github.com/Sakana-1314/LanDrive/actions/workflows/test.yml)
[![构建并推送镜像](https://github.com/Sakana-1314/LanDrive/actions/workflows/build-images.yml/badge.svg)](https://github.com/Sakana-1314/LanDrive/actions/workflows/build-images.yml)
[![镜像](https://img.shields.io/badge/ghcr.io-lan--drive-blue?logo=docker)](https://github.com/Sakana-1314/LanDrive/pkgs/container/lan-drive)

一个可在局域网内部署的文件共享网盘。

## 功能

- **账号登录** —— 按工号登录，区分管理员与普通用户
- **独立目录** —— 每人一个自己的目录，只能修改自己的文件
- **全员可读** —— 可以浏览、搜索、下载所有人共享的文件
- **在线预览** —— 文档、表格、演示、PDF、图片、文本、音视频直接在浏览器中打开
- **大文件上传** —— 支持分片与断点续传
- **自动清理** —— 文件到期自动删除，回收站可恢复
- **管理后台** —— 用户管理、容量与类型限制、存储一致性检查

## 文档

使用教程与部署指南：**<https://sakana-1314.github.io/LanDrive/>**

## 镜像

```
ghcr.io/sakana-1314/lan-drive:server
ghcr.io/sakana-1314/lan-drive:web
```

## 目录

| 目录 | 说明 |
| --- | --- |
| `server/` | 服务端 |
| `web/` | 网页端 |
| `docs/` | 文档与设计说明 |
