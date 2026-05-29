package providers

// knownProviders maps provider name to its default base URL.
// An empty string means the URL is tenant-specific and must be supplied explicitly.
var knownProviders = map[string]string{
	"together_ai":   "https://api.together.xyz/v1",
	"fireworks_ai":  "https://api.fireworks.ai/inference/v1",
	"perplexity":    "https://api.perplexity.ai",
	"xai":           "https://api.x.ai/v1",
	"groq":          "https://api.groq.com/openai/v1",
	"mistral":       "https://api.mistral.ai/v1",
	"deepseek":      "https://api.deepseek.com/v1",
	"moonshot":      "https://api.moonshot.cn/v1",
	"zhipu":         "https://open.bigmodel.cn/api/paas/v4",
	"yi":            "https://api.01.ai/v1",
	"qwen":          "https://dashscope.aliyuncs.com/compatible-mode/v1",
	"nvidia_nim":    "https://integrate.api.nvidia.com/v1",
	"octoai":        "https://text.octoai.run/v1",
	"anyscale":      "https://api.endpoints.anyscale.com/v1",
	"deepinfra":     "https://api.deepinfra.com/v1/openai",
	"lepton":        "https://api.lepton.ai/api/v1",
	"novita":        "https://api.novita.ai/v3/openai",
	"openrouter":    "https://openrouter.ai/api/v1",
	"cerebras":      "https://api.cerebras.ai/v1",
	"sambanova":     "https://api.sambanova.ai/v1",
	"lambda":        "https://api.lambdalabs.com/v1",
	"hyperbolic":    "https://api.hyperbolic.xyz/v1",
	"nscale":        "https://inference.nscale.com/v1",
	"azure_ai":      "", // tenant-specific URL required: https://{deployment}.{region}.inference.ai.azure.com/v1
	"ollama":        "http://localhost:11434/v1",
	"lmstudio":      "http://localhost:1234/v1",
	"vllm":          "http://localhost:8000/v1",
	"sglang":        "http://localhost:30000/v1",
	"llamacpp":      "http://localhost:8080/v1",
	"oobabooga":     "http://localhost:5001/v1",
	"litellm_proxy": "http://localhost:4000/v1",
}
