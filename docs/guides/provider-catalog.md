# Provider 目录与余额查询

Omniflux 提供了极其丰富的大模型供应商开箱即用支持，涵盖官方公有云、开源自建框架以及第三方聚合平台。

## 支持的厂商分类

### 1. 国内主流官方平台
内置端点与官方 API 协议规范适配，无需手工配置复杂的 Base URL：
- **DeepSeek**（深度求索）
- **智谱 GLM**（智谱 BigModel 开放平台）
- **通义千问**（阿里云百炼平台 DashScope）
- **Kimi**（月之暗面 Moonshot AI）
- **火山方舟**（字节跳动火山引擎 Ark）
- **MiniMax**（稀宇科技开放平台）
- **阶跃星辰**（StepFun）
- **零一万物**（01.AI）
- **百度千帆**（Baidu Qianfan）

### 2. 海外主流官方平台
支持标准及专用协议对接：
- **OpenAI**（GPT-4o、GPT-4o-mini、o1、Embeddings 等）
- **Anthropic**（Claude 3.5 Sonnet、Claude 3 Haiku 等）
- **Google Gemini**（Gemini 1.5 Pro / Flash 系列）
- **Mistral AI**、**Groq**、**Cohere** 等

### 3. 聚合网关与第三方平台
适合需要多模型统一接入或高性价比算力的团队：
- **SiliconFlow**（硅基流动）
- **OpenRouter**
- **Novita AI**

### 4. 私有部署与本地推理引擎
全面支持企业内网搭建的开源大模型运行环境：
- **Ollama**
- **vLLM**
- **LM Studio**
- **LocalAI** / **TGI**

## Coding Plan / Token Plan 变体支持

针对当前各大厂商推出的特定开发者套餐（如 Kimi For Coding、火山引擎 Coding Plan、百度千帆 Coding Plan 等），Omniflux 在供应商目录中专门提供了对应的预设选项。

- 能够自动匹配套餐专属的接入地址和模型名称；
- 方便企业在同一厂商下区分“按量计费账号”与“开发者套餐账号”，配合多账号轮询，在套餐额度用完时无缝平滑回退至按量计费。

## 余额与配额自动监控

为了避免因供应商账户欠费或套餐耗尽造成线上事故，Omniflux 支持在管理控制台中一键查询部分厂商的实时余额与配额：

### 当前支持自动查询的供应商：
- **DeepSeek**：账户可用现金余额
- **智谱 GLM**：通用账户余额与 Coding Plan 套餐剩余额度
- **StepFun**（阶跃星辰）：可用账户余额
- **SiliconFlow**（硅基流动）：账户实时余额
- **OpenRouter**：Key 额度与账户可用资金
- **Novita AI**：账户信用额度

### 使用方式：
在管理台进入「模型接入 → 供应商账号」，在目标账号操作列点击「查询余额」，系统将即时调取官方接口回显实时数据，并在余额触警时提供可视化提示。
