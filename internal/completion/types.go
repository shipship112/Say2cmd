package completion

// Message 定义消息结构
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenAIRequest 定义OpenAI API请求结构
type OpenAIRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

// OpenAIResponse 定义OpenAI API响应结构
type OpenAIResponse struct {
	Choices []Choice `json:"choices"`
}

// Choice 定义选择结构
type Choice struct {
	Delta Delta `json:"delta"`
}

// Delta 定义增量结构
type Delta struct {
	Content string `json:"content"`
}

// AliyunRequest 定义阿里云大模型API请求结构
type AliyunRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	Temperature float64   `json:"temperature,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
}

// AliyunResponse 定义阿里云大模型API响应结构
type AliyunResponse struct {
	Output Output `json:"output"`
}

// Output 定义阿里云响应输出结构
type Output struct {
	Text string `json:"text"`
}

// AliyunStreamResponse 定义阿里云大模型API流式响应结构
type AliyunStreamResponse struct {
	Output       Output `json:"output"`
	FinishReason string `json:"finish_reason,omitempty"`
}
