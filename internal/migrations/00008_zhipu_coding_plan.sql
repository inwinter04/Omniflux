-- +goose Up
-- 00008: 厂商目录补充智谱 GLM Coding Plan(国内站与国际站)两个套餐条目。
ALTER TABLE provider_accounts DROP CONSTRAINT provider_accounts_provider_check;
ALTER TABLE provider_accounts ADD CONSTRAINT provider_accounts_provider_check
CHECK (provider IN (
	'openai', 'azure_openai', 'anthropic', 'gemini', 'vertex', 'bedrock',
	'openai_compatible', 'kimi', 'kimi-global', 'kimi-for-coding', 'kimi-for-coding-global', 'packycode',
	'patewayai', 'modelflare', 'haoai', 'fennoai', 'runapi', 'openmodel',
	'grooroute-pro', 'buytoken', 'vapi', 'tokenfactory', 'aigocode', 'qiniu',
	'aicoding', 'subrouter', 'ospreyai', 'shengsuanyun', 'api88', 'code9527',
	'code0', 'teamorouter', 'ppio', 'claudecn', 'volcano-agent-plan', 'volcano-coding-plan',
	'byteplus', 'volcengine-doubao', 'siliconflow', 'siliconflow-en', 'a6api', 'compshare',
	'compshare-coding', 'ccsub', 'sssaicode', 'soleapi', 'micu', 'rightcode',
	'cubence', 'crazyrouter', 'dmxapi', 'xycai', 'alltokens', 'tuzi',
	'amux', 'deepseek', 'zhipu-glm', 'zhipu-glm-en', 'baidu-qianfan', 'baidu-qianfan-coding',
	'qwenai', 'qwenai-token-plan', 'qwencloud', 'qwencloud-coding', 'qwencloud-token-plan', 'tencent-hunyuan',
	'stepfun-api', 'stepfun-api-en', 'stepfun-plan', 'stepfun-plan-en', 'streamlake', 'streamlake-en',
	'longcat', 'minimax', 'minimax-en', 'astron-coding-plan', 'bailing', 'xiaomi-mimo',
	'xiaomi-mimo-token-plan', 'baseten', 'novita', 'xai', 'nvidia', 'ollama-cloud',
	'ollama', 'lm-studio', 'llama-cpp', 'vllm', 'deepinfra', 'nebius',
	'groq', 'sambanova', 'digitalocean', 'huggingface', 'fireworks', 'mistral',
	'cloudflare-ai-gateway', 'aihubmix', 'genesis', 'cherryin', 'relaxycode', 'openrouter',
	'therouter', 'jiekou', 'aicodewith', 'commandcode', 'moark', 'soruxgpt',
	'ikuncode', 'runtoken', 'botcf', 'yylx', 'nekocode', 'aicoming',
	'alltokens-si', 'modelscope', 'tokenflux', 'zhipu-glm-coding', 'zhipu-glm-en-coding'
));

-- +goose Down
ALTER TABLE provider_accounts DROP CONSTRAINT provider_accounts_provider_check;
ALTER TABLE provider_accounts ADD CONSTRAINT provider_accounts_provider_check
CHECK (provider IN (
	'openai', 'azure_openai', 'anthropic', 'gemini', 'vertex', 'bedrock',
	'openai_compatible', 'kimi', 'kimi-global', 'kimi-for-coding', 'kimi-for-coding-global', 'packycode',
	'patewayai', 'modelflare', 'haoai', 'fennoai', 'runapi', 'openmodel',
	'grooroute-pro', 'buytoken', 'vapi', 'tokenfactory', 'aigocode', 'qiniu',
	'aicoding', 'subrouter', 'ospreyai', 'shengsuanyun', 'api88', 'code9527',
	'code0', 'teamorouter', 'ppio', 'claudecn', 'volcano-agent-plan', 'volcano-coding-plan',
	'byteplus', 'volcengine-doubao', 'siliconflow', 'siliconflow-en', 'a6api', 'compshare',
	'compshare-coding', 'ccsub', 'sssaicode', 'soleapi', 'micu', 'rightcode',
	'cubence', 'crazyrouter', 'dmxapi', 'xycai', 'alltokens', 'tuzi',
	'amux', 'deepseek', 'zhipu-glm', 'zhipu-glm-en', 'baidu-qianfan', 'baidu-qianfan-coding',
	'qwenai', 'qwenai-token-plan', 'qwencloud', 'qwencloud-coding', 'qwencloud-token-plan', 'tencent-hunyuan',
	'stepfun-api', 'stepfun-api-en', 'stepfun-plan', 'stepfun-plan-en', 'streamlake', 'streamlake-en',
	'longcat', 'minimax', 'minimax-en', 'astron-coding-plan', 'bailing', 'xiaomi-mimo',
	'xiaomi-mimo-token-plan', 'baseten', 'novita', 'xai', 'nvidia', 'ollama-cloud',
	'ollama', 'lm-studio', 'llama-cpp', 'vllm', 'deepinfra', 'nebius',
	'groq', 'sambanova', 'digitalocean', 'huggingface', 'fireworks', 'mistral',
	'cloudflare-ai-gateway', 'aihubmix', 'genesis', 'cherryin', 'relaxycode', 'openrouter',
	'therouter', 'jiekou', 'aicodewith', 'commandcode', 'moark', 'soruxgpt',
	'ikuncode', 'runtoken', 'botcf', 'yylx', 'nekocode', 'aicoming',
	'alltokens-si', 'modelscope', 'tokenflux'
));
