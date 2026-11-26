# Provider Catalog & Balance

Omniflux includes native integrations for leading model providers, aggregation platforms, and private inference runtimes.

## Supported Providers

### 1. Chinese Model Providers
Preconfigured endpoints and protocol drivers:
- **DeepSeek**
- **Zhipu GLM** (BigModel)
- **Alibaba Qwen** (DashScope)
- **Moonshot Kimi**
- **ByteDance Volcano Engine Ark**
- **MiniMax**
- **StepFun**
- **01.AI**
- **Baidu Qianfan**

### 2. Global Model Providers
- **OpenAI** (GPT-4o, o1, embeddings)
- **Anthropic** (Claude 3.5 Sonnet / Haiku)
- **Google Gemini** (Gemini 1.5 Pro / Flash)
- **Mistral AI**, **Groq**, **Cohere**

### 3. Aggregation Services
- **SiliconFlow**
- **OpenRouter**
- **Novita AI**

### 4. Self-Hosted & Local Runtimes
- **Ollama**
- **vLLM**
- **LM Studio**
- **LocalAI** / **TGI**

## Coding Plan Variants

Dedicated catalog entries are available for specialized packages (e.g. Kimi For Coding, Volcano Engine Coding Plan, Baidu Qianfan Coding Plan). These map directly to specialized endpoints and models, enabling automated failover to standard pay-as-you-go accounts when monthly quotas are depleted.

## Automated Balance Monitoring

Query live account balances and quotas directly from the console for supported providers:
- **DeepSeek**: Cash account balance
- **Zhipu GLM**: Account balance and Coding Plan quotas
- **StepFun**: Account balance
- **SiliconFlow**: Real-time credits
- **OpenRouter**: Key credits and balance
- **Novita AI**: Account balance
