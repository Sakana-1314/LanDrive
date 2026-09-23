// 静态托管（非容器）部署时的占位配置。
//
// 容器部署时，该文件会被 nginx 启动脚本覆盖为运行时注入的配置
// （见 web/docker-entrypoint.d/40-lan-drive-config.sh）。
// 需要在不重新构建的情况下改 API 地址时，直接改这个文件即可。
window.__LANDRIVE_CONFIG__ = {
  apiBaseUrl: "",
  probeTimeout: 6000
};
