package completion

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"

	"github/xiuivfbc/NaturalCmd/internal/config"
	"github/xiuivfbc/NaturalCmd/internal/utils"
)

// 全局 i18n bundle
var bundle *i18n.Bundle

// 初始化 i18n bundle
func init() {
	bundle = i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)

	// 加载翻译文件
	_, err := bundle.LoadMessageFile("locales/en.json")
	if err != nil {
		fmt.Printf("Error loading en.json: %v\n", err)
	}
	_, err = bundle.LoadMessageFile("locales/zh.json")
	if err != nil {
		fmt.Printf("Error loading zh.json: %v\n", err)
	}
}

// GenerateScript 生成shell脚本
func GenerateScript(prompt string, cfg *config.Config) (string, error) {
	// 模拟模式：直接返回预设命令
	if cfg.APIKey == "mock" {
		fmt.Println("```ls -la```")
		return "ls -la", nil
	}

	fullPrompt := buildFullPrompt(prompt, cfg)

	// ================= [Eino 改造] =================
	// 1. 初始化 Eino ChatModel，这里的 NewChatModel 我们写在了 client.go 里
	cm, err := NewChatModel(context.Background(), cfg)
	if err != nil {
		return "", fmt.Errorf("failed to init eino chat model: %v", err)
	}

	// 2. 组装输入 Message
	// 使用 Eino 提供的 schema.UserMessage 快速组装用户问题
	msgs := []*schema.Message{
		schema.UserMessage(fullPrompt),
	}

	// 3. 调用 Generate 进行非流式请求
	// Eino 会在底层帮我们处理 http request/response 解析，我们直接拿到提取好文本的 resp
	resp, err := cm.Generate(context.Background(), msgs)
	if err != nil {
		return "", fmt.Errorf("model generation error: %v", err)
	}
	// ===============================================

	// 提取命令，移除代码块标记 (这里仍然使用 resp.Content 而不是原始的 result)
	script := utils.ExtractScript(resp.Content)
	return script, nil
}

// GenerateExplanation 生成脚本解释
func GenerateExplanation(script string, cfg *config.Config) (string, error) {
	// 模拟模式
	if cfg.APIKey == "mock" {
		// 创建本地化器
		localizer := i18n.NewLocalizer(bundle, cfg.Language)

		// 使用翻译
		explanation := localizer.MustLocalize(&i18n.LocalizeConfig{
			MessageID: "mockExplanation",
		})
		fmt.Println(explanation)
		return explanation, nil
	}

	// 创建本地化器
	localizer := i18n.NewLocalizer(bundle, cfg.Language)

	// 使用翻译
	prompt := localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID: "generateExplanationPrompt",
		TemplateData: map[string]interface{}{
			"Script": script,
		},
	})

	// ================= [Eino 改造] =================
	cm, err := NewChatModel(context.Background(), cfg)
	if err != nil {
		return "", err
	}

	msgs := []*schema.Message{
		schema.UserMessage(prompt),
	}

	// 1. 调用 Stream 返回一个 StreamReader
	streamResp, err := cm.Stream(context.Background(), msgs)
	if err != nil {
		return "", err
	}
	defer streamResp.Close()

	// 2. 利用 Eino StreamReader 提供的迭代器 Recv() 处理流数据的核心循环
	var builder strings.Builder
	for {
		chunk, err := streamResp.Recv()
		if err != nil {
			if err == io.EOF {
				// EOF 代表正常读取到末尾
				break
			}
			// 中途出现的网络异常、解析异常
			return builder.String(), err
		}

		// 输出当前流收到的最新分段
		fmt.Print(chunk.Content)
		builder.WriteString(chunk.Content)
	}

	fmt.Println()
	return builder.String(), nil
}

// GenerateQueryExpansion 生成用于检索增强的语义扩展词（逗号分隔）。
func GenerateQueryExpansion(query string, cfg *config.Config) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil
	}

	prompt := fmt.Sprintf("Expand this user intent into concise retrieval keywords for command-line tasks. Return one single line as comma-separated keywords only, no explanation, no markdown: %s", query)

	// ================= [Eino 改造] =================
	cm, err := NewChatModel(context.Background(), cfg)
	if err != nil {
		return "", err
	}

	msgs := []*schema.Message{
		schema.UserMessage(prompt),
	}

	// 调用生成，同时保留原版中要求输出随机性低 (Temperature 0.2) 行为的配置
	// Eino 支持通过 WithTemperature 等 Option 直接传参覆写配置中的默认值
	resp, err := cm.Generate(context.Background(), msgs, model.WithTemperature(0.2), model.WithTopP(0.8))
	if err != nil {
		return "", err
	}
	// ===============================================

	return normalizeExpansionTerms(resp.Content), nil
}

func normalizeExpansionTerms(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "`")
	raw = strings.ReplaceAll(raw, "\n", ",")
	raw = strings.ReplaceAll(raw, "；", ",")
	raw = strings.ReplaceAll(raw, ";", ",")

	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		term := strings.TrimSpace(strings.Trim(part, "\"'"))
		if len(term) <= 1 {
			continue
		}
		key := strings.ToLower(term)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, term)
		if len(result) >= 8 {
			break
		}
	}

	return strings.Join(result, ", ")
}

// buildFullPrompt 构建完整的提示
func buildFullPrompt(prompt string, cfg *config.Config) string {
	shell := utils.GetShell()
	os := utils.GetOS()

	// 获取当前环境信息
	envInfo := "\n\nCurrent environment:\n"

	// 获取当前工作目录
	currentDir, err := utils.GetCurrentDir()
	if err == nil {
		envInfo += fmt.Sprintf("- Current directory: %s\n", currentDir)
	}

	// 列出当前目录中的文件和目录
	files, err := utils.ListFiles(".")
	if err == nil && len(files) > 0 {
		envInfo += "- Files and directories in current directory:\n"
		for _, file := range files {
			envInfo += fmt.Sprintf("  - %s\n", file)
		}
	}

	// 创建本地化器
	localizer := i18n.NewLocalizer(bundle, cfg.Language)

	// 使用翻译
	return localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID: "generateScriptPrompt",
		TemplateData: map[string]interface{}{
			"Shell":  shell,
			"OS":     os,
			"Prompt": prompt + envInfo,
		},
	})
}
