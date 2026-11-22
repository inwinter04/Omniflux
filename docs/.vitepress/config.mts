import { defineConfig } from 'vitepress'

// GitHub Pages 项目站点部署在 /omniflux/ 子路径下;
// 改用自定义域名或用户主站点时,把 base 改为 '/'。
export default defineConfig({
  base: '/Omniflux/',
  lang: 'zh-CN',
  title: 'Omniflux',
  description: 'All intelligence, one current. 企业 AI 服务控制平面:治理、路由、计量、审计与可观测。',
  head: [['link', { rel: 'icon', type: 'image/svg+xml', href: '/Omniflux/favicon.svg' }]],
  // docs/architecture/ 下的设计文档被 .gitignore 显式排除,属于内部资料,
  // 站点构建时同样要排除,避免被发布。
  srcExclude: ['architecture/**'],
  themeConfig: {
    logo: '/favicon.svg',
    siteTitle: 'Omniflux',
    nav: [
      { text: '指南', link: '/guide/quick-start' },
      { text: 'API 参考', link: '/api/dataplane' },
      { text: 'GitHub', link: 'https://github.com/inwinter04/Omniflux' }
    ],
    sidebar: [
      {
        text: '指南',
        items: [
          { text: '快速开始', link: '/guide/quick-start' },
          { text: '开发', link: '/guide/development' }
        ]
      },
      {
        text: 'API 参考',
        items: [
          { text: '数据面 API', link: '/api/dataplane' },
          { text: '管理面 API', link: '/api/admin' }
        ]
      },
      {
        text: '设计',
        items: [
            ]
      }
    ],
    search: {
      provider: 'local',
      options: {
        translations: {
          button: { buttonText: '搜索文档', buttonAriaLabel: '搜索文档' },
          modal: {
            noResultsText: '没有找到结果',
            resetButtonTitle: '清空条件',
            footer: { selectText: '选择', navigateText: '切换', closeText: '关闭' }
          }
        }
      }
    },
    socialLinks: [{ icon: 'github', link: 'https://github.com/inwinter04/Omniflux' }],
    outline: [2, 4],
    docFooter: { prev: '上一页', next: '下一页' }
  }
})
