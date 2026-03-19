package config

import (
	"os"
	"strconv"
	"strings"
)

// Config 定义配置结构
type Config struct {
	APIKey            string // API密钥
	APIEndpoint       string // API端点
	Model             string // 模型名称
	Language          string // 语言
	SilentMode        bool   // 静默模式
	Provider          string // 模型提供商: openai, aliyun
	HistoryFile       string // 历史记录文件路径
	HistoryMax        int   // 历史记录最大容量
	RAGEnabled        bool   // RAG功能是否启用
	RAGTopK           int    // RAG TopK
	RAGMinLocalHit    int    // RAG最小本地命中分数
	RAGMinLocalCover  float64 // RAG最小本地覆盖度
	RAGFeedbackFile   string // RAG反馈文件路径
	RAGSemanticExpand bool   // RAG语义扩展是否启用
}

// Load 加载配置
func Load() (*Config, error) {
	return &Config{
		APIKey:            getEnv("API_KEY", getEnv("OPENAI_KEY", "")),
		APIEndpoint:       getEnv("API_ENDPOINT", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"),
		Model:             getEnv("MODEL", "gpt-4o-mini"),
		Language:          getEnv("LANGUAGE", "en"),
		SilentMode:        getEnv("SILENT_MODE", "false") == "true",
		Provider:          getEnv("PROVIDER", "openai"),
		HistoryFile:       getEnv("HISTORY_FILE", ""),
		HistoryMax:        getEnvAsInt("HISTORY_MAX_CAPACITY", 50),
		RAGEnabled:        getEnvAsBool("RAG_ENABLED", true),
		RAGTopK:           getEnvAsInt("RAG_TOP_K", 3),
		RAGMinLocalHit:    getEnvAsInt("RAG_MIN_LOCAL_HIT_SCORE", 4),
		RAGMinLocalCover:  getEnvAsFloat64("RAG_MIN_LOCAL_COVERAGE", 0.45),
		RAGFeedbackFile:   getEnv("RAG_FEEDBACK_FILE", ""),
		RAGSemanticExpand: getEnvAsBool("RAG_SEMANTIC_EXPAND", true),
	}, nil
}

// getEnv 获取环境变量，如果不存在则返回默认值
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}

	return parsed
}

func getEnvAsBool(key string, defaultValue bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return defaultValue
	}

	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultValue
	}
}

func getEnvAsFloat64(key string, defaultValue float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return defaultValue
	}

	if parsed < 0 {
		return 0
	}
	if parsed > 1 {
		return 1
	}

	return parsed
}
