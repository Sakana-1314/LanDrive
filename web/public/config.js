// 静态托管（非容器）部署时的占位配置。
//
// 容器部署时，该文件会被 nginx 启动脚本覆盖（见
// web/docker-entrypoint.d/40-lan-drive-config.sh）。
// apiBaseUrl 留空时，前端回退到构建期 HOST 注入的后端域名（__API_HOST__），
// 再回退到同源 /api。需要在不重新构建的前提下改地址时，直接改这个文件即可。
window.__LANDRIVE_CONFIG__ = {
  apiBaseUrl: "",
  probeTimeout: 6000
};
