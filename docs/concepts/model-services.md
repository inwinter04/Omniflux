# 模型服务与 Provider

在传统的开发模式中，业务代码直接调用特定厂商的模型（例如 `client.chat.completions.create(model="deepseek-chat")`）。当供应商发生故障、价格变动或需要技术升级时，企业必须大面积重构业务代码。

Omniflux 引入了 **逻辑模型服务（Logical Model Service）** 这一层核心抽象，彻底解耦业务诉求与物理模型实现。

## 核心设计概念

![Omniflux 逻辑模型服务与智能路由](/assets/model-services.zh-CN.png)

### 1. 供应商账号 (Provider Account)
- 代表一个外部大模型平台或私有部署集群的接入凭据。
- **凭据高强度加密**：所有 API Key、Base URL 均使用 AES-256-GCM 算法和由平台管理员保管的 Master Key 加密落库。
- **多账号负载支持**：针对同一个厂商（如 DeepSeek），允许配置多个独立的账号凭据，天然支持多 Key 轮询与并发额度扩展。

### 2. 逻辑模型服务 (Logical Model Service)
- 面向业务端暴露的虚拟模型名称（例如 `general-pro-chat`、`code-assistant`、`fast-embedding`）。
- 业务系统在调用 `/v1/chat/completions` 时，只需指定该逻辑服务名。
- 逻辑模型服务具备独立的状态控制（启用/停用）、结算币种声明（如 USD / CNY）以及组织级/项目级的调用权限隔离。

### 3. 候选模型列表 (Candidates)
- 一个逻辑模型服务可以挂载多个具体的候选实例。
- 每个候选包含：
  - **绑定的 Provider 账号**：指明凭证与调用协议。
  - **真实上游模型名 (Upstream Model)**：供应商实际接受的模型标识（如 `deepseek-chat`、`qwen-plus-0919`、`gpt-4o-2024-08-06`）。
  - **优先级 (Priority)**：数值越小优先级越高。
  - **组内权重 (Weight)**：同一优先级内参与加权路由的相对概率比值。
  - **质量先验分 (Quality Score)**：0.0 ~ 1.0 的质量基准评分，参与路由算法计算。

## 模型接入与 Base URL 规范

Omniflux 内置了丰富的供应商驱动目录（覆盖主流国内外公共云、聚合网关及自建推理框架）。

::: warning Base URL 填写的常见误区
- **使用系统内置目录时**：官方公有云供应商的接口地址由 Omniflux 自动填充，**无需手工填写 Base URL**。
- **使用「自定义 OpenAI 兼容」或本地推理引擎时**（如 Ollama、vLLM、LM Studio、LocalAI）：
  - 填写的地址**严禁以 `/v1` 结尾**！
  - 例如本地 Ollama 默认监听 `http://localhost:11434`，填写 `http://127.0.0.1:11434` 即可；
  - 网关在调用时，会自动在末尾拼接 `/v1/chat/completions` 等标准路径。如果填成 `http://127.0.0.1:11434/v1`，将导致请求拼接为 `/v1/v1/chat/completions` 导致 404 错误。
:::

## 候选生命周期与实时画像

每个候选模型在运行时都会被实时采集指标，并形成多维画像：
- **近 7 天统计画像**：持续累积的平均延迟与历史可靠性指标，供评分引擎打分。
- **近 24 小时健康度画像**：高频滑窗统计成功率。当某个候选短时间内连续报错或超时时，路由系统会自动对其降权；当上游完全不可用时触发熔断冷却，避免无效流量重试加剧网络拥塞。
