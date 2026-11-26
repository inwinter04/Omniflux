import { defineConfig, type DefaultTheme } from 'vitepress'

const githubUrl = 'https://github.com/inwinter04/Omniflux'

// 侧边栏与导航按语言生成，prefix 为 '' (中文) 或 '/en' (英文)。
function sidebar(prefix: string, t: Record<string, string>): DefaultTheme.Sidebar {
  return [
    {
      text: t.intro,
      items: [
        { text: t.whatIs, link: `${prefix}/introduction/what-is-omniflux` },
        { text: t.quickStart, link: `${prefix}/introduction/quick-start` }
      ]
    },
    {
      text: t.concepts,
      items: [
        { text: t.orgModel, link: `${prefix}/concepts/organization-model` },
        { text: t.modelServices, link: `${prefix}/concepts/model-services` },
        { text: t.routing, link: `${prefix}/concepts/routing` },
        { text: t.metering, link: `${prefix}/concepts/metering-budget` }
      ]
    },
    {
      text: t.guides,
      items: [
        { text: t.providerCatalog, link: `${prefix}/guides/provider-catalog` },
        { text: t.promptTemplates, link: `${prefix}/guides/prompt-templates` },
        { text: t.agents, link: `${prefix}/guides/agents` }
      ]
    },
    {
      text: t.deployment,
      items: [
        { text: t.requirements, link: `${prefix}/deployment/requirements` },
        { text: t.configuration, link: `${prefix}/deployment/configuration` },
        { text: t.docker, link: `${prefix}/deployment/docker` },
        { text: t.fromSource, link: `${prefix}/deployment/from-source` },
        { text: t.operations, link: `${prefix}/deployment/operations` }
      ]
    },
    {
      text: t.reference,
      items: [
        { text: t.dataplaneApi, link: `${prefix}/reference/dataplane-api` },
        { text: t.adminApi, link: `${prefix}/reference/admin-api` }
      ]
    },
    {
      text: t.contributing,
      items: [
        { text: t.development, link: `${prefix}/contributing/development` },
        { text: t.openSource, link: `${prefix}/contributing/open-source` }
      ]
    }
  ]
}

function nav(prefix: string, t: Record<string, string>): DefaultTheme.NavItem[] {
  return [
    { text: t.intro, link: `${prefix}/introduction/what-is-omniflux`, activeMatch: `${prefix}/introduction/` },
    { text: t.concepts, link: `${prefix}/concepts/organization-model`, activeMatch: `${prefix}/concepts/` },
    { text: t.guides, link: `${prefix}/guides/provider-catalog`, activeMatch: `${prefix}/guides/` },
    { text: t.deployment, link: `${prefix}/deployment/requirements`, activeMatch: `${prefix}/deployment/` },
    { text: t.reference, link: `${prefix}/reference/dataplane-api`, activeMatch: `${prefix}/reference/` },
    { text: t.contributing, link: `${prefix}/contributing/development`, activeMatch: `${prefix}/contributing/` }
  ]
}

const zh = {
  intro: '入门',
  whatIs: 'Omniflux 是什么',
  quickStart: '快速开始',
  concepts: '核心概念',
  orgModel: '多租户与组织模型',
  modelServices: '模型服务与 Provider',
  routing: '动态路由与决策留痕',
  metering: '计量计费、预算与限流',
  guides: '实践指南',
  providerCatalog: 'Provider 目录与余额查询',
  promptTemplates: '提示词模板 (PromptTemplate)',
  agents: '智能体运行时 (Agent)',
  deployment: '部署运维',
  requirements: '环境要求与架构规划',
  configuration: '环境变量配置指南',
  docker: 'Docker 镜像部署',
  fromSource: '源码构建与本地运行',
  operations: '用户初始化与运维保障',
  reference: '接口参考',
  dataplaneApi: '数据面 API (OpenAI 兼容)',
  adminApi: '管理面 API',
  contributing: '开源生态',
  development: '本地开发与代码规范',
  openSource: '开源依赖与致谢'
}

const en = {
  intro: 'Introduction',
  whatIs: 'What is Omniflux',
  quickStart: 'Quick Start',
  concepts: 'Core Concepts',
  orgModel: 'Multi-Tenancy & Org Model',
  modelServices: 'Model Services & Providers',
  routing: 'Dynamic Routing & Auditing',
  metering: 'Metering, Budgets & Rate Limits',
  guides: 'Guides',
  providerCatalog: 'Provider Catalog & Balance',
  promptTemplates: 'Prompt Templates',
  agents: 'Agent Runtime',
  deployment: 'Deployment',
  requirements: 'Requirements & Architecture',
  configuration: 'Environment Configuration',
  docker: 'Docker Deployment',
  fromSource: 'Building from Source',
  operations: 'Bootstrap & Operations',
  reference: 'API Reference',
  dataplaneApi: 'Data Plane API (OpenAI Compatible)',
  adminApi: 'Admin Plane API',
  contributing: 'Ecosystem',
  development: 'Development & Contributing',
  openSource: 'Open Source Credits'
}

export default defineConfig({
  // GitHub Pages 项目站点部署在 /Omniflux/ 子路径下;
  // 改用自定义域名或用户主站点时,把 base 改为 '/'。
  base: '/Omniflux/',
  title: 'Omniflux',
  head: [['link', { rel: 'icon', type: 'image/svg+xml', href: '/Omniflux/favicon.svg' }]],
  // docs/architecture/ 下的设计文档被 .gitignore 显式排除,属于内部资料,
  // 站点构建时同样要排除,避免被发布。
  srcExclude: ['architecture/**'],
  markdown: {
    math: true
  },
  locales: {
    root: {
      label: '简体中文',
      lang: 'zh-CN',
      description: 'All intelligence, one current. 企业 AI 服务控制平面:治理、路由、计量、审计与可观测。',
      themeConfig: {
        nav: [...nav('', zh), { text: 'GitHub', link: githubUrl }],
        sidebar: sidebar('', zh),
        docFooter: { prev: '上一页', next: '下一页' },
        outlineTitle: '本页目录',
        langMenuLabel: '切换语言',
        sidebarMenuLabel: '菜单',
        returnToTopLabel: '回到顶部',
        darkModeSwitchLabel: '主题',
        lightModeSwitchTitle: '切换到浅色模式',
        darkModeSwitchTitle: '切换到深色模式'
      }
    },
    en: {
      label: 'English',
      lang: 'en-US',
      link: '/en/',
      description:
        'All intelligence, one current. Enterprise AI service control plane: governance, routing, metering, audit and observability.',
      themeConfig: {
        nav: [...nav('/en', en), { text: 'GitHub', link: githubUrl }],
        sidebar: sidebar('/en', en)
      }
    }
  },
  themeConfig: {
    logo: '/favicon.svg',
    siteTitle: 'Omniflux',
    search: {
      provider: 'local',
      options: {
        locales: {
          root: {
            translations: {
              button: { buttonText: '搜索文档', buttonAriaLabel: '搜索文档' },
              modal: {
                noResultsText: '没有找到结果',
                resetButtonTitle: '清空条件',
                footer: { selectText: '选择', navigateText: '切换', closeText: '关闭' }
              }
            }
          }
        }
      }
    },
    socialLinks: [{ icon: 'github', link: githubUrl }],
    outline: [2, 4]
  }
})
