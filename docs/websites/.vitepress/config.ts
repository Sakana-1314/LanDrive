import { defineConfig } from 'vitepress'
import { withMermaid } from 'vitepress-plugin-mermaid'

/**
 * 站点源码在 pages/（srcDir），构建产物在 .vitepress/dist/。
 * base 必须与 GitHub Pages 的仓库路径**大小写完全一致**：
 *   https://sakana-1314.github.io/LanDrive/
 * GitHub Pages 的路径区分大小写，写成小写会导致线上资源全部 404。
 *
 * withMermaid 让 ```mermaid 代码块渲染成图（部署拓扑、文件生命周期都用到）。
 */
export default withMermaid(
  defineConfig({
    base: '/LanDrive/',
    srcDir: 'pages',
    lang: 'zh-CN',
    title: '局域网文件助手',
    // 导航左侧已显示站点名，页面标题不必再带一遍。
    titleTemplate: ':title',
    description: '局域网文件助手的部署与使用文档。',
    cleanUrls: true,
    head: [['meta', { name: 'theme-color', content: '#1f6feb' }]],
    lastUpdated: true,
    themeConfig: {
      siteTitle: '局域网文件助手',
      outline: { level: [2, 3], label: '本页目录' },
      nav: [
        { text: '指南', link: '/guide/what-is' },
        { text: '部署', link: '/guide/deploy' },
        { text: '使用', link: '/usage/files' },
        { text: '管理', link: '/admin/users' },
      ],
      sidebar: [
        {
          text: '入门',
          items: [
            { text: '这是什么', link: '/guide/what-is' },
            { text: '功能一览', link: '/guide/features' },
            { text: '部署', link: '/guide/deploy' },
            { text: '前后端分离部署', link: '/guide/deploy-split' },
            { text: '常见问题', link: '/guide/faq' },
          ],
        },
        {
          text: '使用教程',
          items: [
            { text: '登录', link: '/usage/login' },
            { text: '浏览与下载', link: '/usage/files' },
            { text: '上传文件', link: '/usage/upload' },
            { text: '在线预览', link: '/usage/preview' },
            { text: '修改密码', link: '/usage/profile' },
          ],
        },
        {
          text: '管理员',
          items: [
            { text: '统计看板', link: '/admin/dashboard' },
            { text: '用户管理', link: '/admin/users' },
            { text: '系统设置', link: '/admin/settings' },
            { text: '文件与回收站', link: '/admin/files' },
            { text: '操作日志', link: '/admin/logs' },
          ],
        },
      ],
      docFooter: { prev: '上一篇', next: '下一篇' },
      darkModeSwitchLabel: '主题',
      returnToTopLabel: '回到顶部',
      sidebarMenuLabel: '目录',
      lastUpdatedText: '最后更新',
      search: {
        provider: 'local',
        options: {
          translations: {
            button: { buttonText: '搜索文档', buttonAriaLabel: '搜索文档' },
            modal: {
              noResultsText: '未找到相关结果',
              resetButtonTitle: '清除查询条件',
              footer: { selectText: '选择', navigateText: '切换', closeText: '关闭' },
            },
          },
        },
      },
      footer: {
        message: '本站为项目文档',
        copyright: '版权归 Sakana-1314 所有',
      },
      socialLinks: [{ icon: 'github', link: 'https://github.com/Sakana-1314/LanDrive' }],
    },
  }),
)
