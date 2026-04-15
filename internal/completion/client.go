package completion

import (
	"context"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"

	"github/xiuivfbc/NaturalCmd/internal/config"
)

// NewChatModel 根据配置创建一个 Eino 的 ChatModel 实例
func NewChatModel(ctx context.Context, cfg *config.Config) (model.ChatModel, error) {
	// 1. 设置 BaseURL
	// 您原来代码中的 APIEndpoint 默认是 "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"
	// Eino 使用的底层统一规范 API 通常只需要 BaseURL，也就是不需要包含具体的 "/chat/completions" 路径。
	baseURL := cfg.APIEndpoint
	if strings.HasSuffix(baseURL, "/chat/completions") {
		baseURL = strings.TrimSuffix(baseURL, "/chat/completions")
	} else if strings.HasSuffix(baseURL, "/v1/chat/completions") {
		baseURL = strings.TrimSuffix(baseURL, "/v1/chat/completions") + "/v1"
	}

	// 2. 组装 Eino OpenAI 客户端配置
	// 无论是由于填写了 OpenAI 还是 Aliyun (DashScope 兼容 OpenAI 格式)，
	// 我们都统一使用 openai 的组件，只要填入对应的模型名、BaseURL 和 鉴权 APIKey 即可实现完美调用。
	chatCfg := &openai.ChatModelConfig{
		APIKey:  cfg.APIKey,
		BaseURL: baseURL,
		Model:   cfg.Model,
	}

	// 你可以根据原先代码在请求里设置的默认参数添加一些配置：
	// 例如原来代码里阿里请求默认加了 Temperature: 0.7, TopP: 0.95
	temperature := float32(0.7)
	topP := float32(0.95)
	chatCfg.Temperature = &temperature
	chatCfg.TopP = &topP

	// 3. 调用 Eino 的方法生成客户端实例
	return openai.NewChatModel(ctx, chatCfg)
}
