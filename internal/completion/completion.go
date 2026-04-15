package completion

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"

	"github/shipship112/Say2cmd/internal/config"
	"github/shipship112/Say2cmd/internal/utils"
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

	messages := []Message{
		{Role: "user", Content: fullPrompt},
	}

	var result string
	var err error

	// 根据提供商选择不同的API调用
	switch cfg.Provider {
	case "aliyun":
		request := AliyunRequest{
			Model:       cfg.Model,
			Messages:    messages,
			Stream:      false,
			Temperature: 0.7,
			TopP:        0.95,
		}
		result, err = callAliyun(request, cfg)
	default: // openai
		request := OpenAIRequest{
			Model:    cfg.Model,
			Messages: messages,
			Stream:   false,
		}
		result, err = callOpenAI(request, cfg)
	}

	if err != nil {
		return "", err
	}

	// 提取命令，移除代码块标记
	script := utils.ExtractScript(result)
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

	messages := []Message{
		{Role: "user", Content: prompt},
	}

	var result string
	var err error

	// 根据提供商选择不同的API调用
	switch cfg.Provider {
	case "aliyun":
		request := AliyunRequest{
			Model:       cfg.Model,
			Messages:    messages,
			Stream:      true,
			Temperature: 0.7,
			TopP:        0.95,
		}
		result, err = callAliyun(request, cfg)
	default: // openai
		request := OpenAIRequest{
			Model:    cfg.Model,
			Messages: messages,
			Stream:   true,
		}
		result, err = callOpenAI(request, cfg)
	}

	return result, err
}

// GenerateQueryExpansion 生成用于检索增强的语义扩展词（逗号分隔）。
func GenerateQueryExpansion(query string, cfg *config.Config) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil
	}

	prompt := fmt.Sprintf("Expand this user intent into concise retrieval keywords for command-line tasks. Return one single line as comma-separated keywords only, no explanation, no markdown: %s", query)
	messages := []Message{{Role: "user", Content: prompt}}

	var result string
	var err error

	switch cfg.Provider {
	case "aliyun":
		request := AliyunRequest{
			Model:       cfg.Model,
			Messages:    messages,
			Stream:      false,
			Temperature: 0.2,
			TopP:        0.8,
		}
		result, err = callAliyun(request, cfg)
	default:
		request := OpenAIRequest{
			Model:    cfg.Model,
			Messages: messages,
			Stream:   false,
		}
		result, err = callOpenAI(request, cfg)
	}

	if err != nil {
		return "", err
	}

	return normalizeExpansionTerms(result), nil
}

// callOpenAI 调用OpenAI API
func callOpenAI(request OpenAIRequest, cfg *config.Config) (string, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", cfg.APIEndpoint, bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.APIKey))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("OpenAI API error: %s", string(body))
	}

	if !request.Stream {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}

		var response struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(data, &response); err != nil {
			return "", err
		}

		var builder strings.Builder
		for _, choice := range response.Choices {
			builder.WriteString(choice.Message.Content)
		}

		return builder.String(), nil
	}

	// 处理流式响应
	var result string
	reader := resp.Body

	buffer := make([]byte, 1024)
	for {
		n, err := reader.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}

		data := string(buffer[:n])
		// 解析SSE格式的响应
		lines := bytes.Split([]byte(data), []byte("\n"))
		for _, line := range lines {
			line = bytes.TrimSpace(line)
			if len(line) == 0 || bytes.HasPrefix(line, []byte(":")) {
				continue
			}

			if bytes.HasPrefix(line, []byte("data: ")) {
				line = bytes.TrimPrefix(line, []byte("data: "))
				if string(line) == "[DONE]" {
					goto done
				}

				var response OpenAIResponse
				if err := json.Unmarshal(line, &response); err != nil {
					continue
				}

				for _, choice := range response.Choices {
					content := choice.Delta.Content
					result += content
					fmt.Print(content)
				}
			}
		}
	}

done:
	fmt.Println()
	return result, nil
}

// callAliyun 调用阿里云大模型API
func callAliyun(request AliyunRequest, cfg *config.Config) (string, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", cfg.APIEndpoint, bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.APIKey))

	// 设置超时
	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Aliyun API error: %s", string(body))
	}

	if request.Stream {
		return streamAliyunResponse(resp.Body)
	}

	// 处理响应
	var result string
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// 尝试解析为OpenAI兼容响应
	var openAIResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if content, ok := parseSSEBody(body); ok {
		return content, nil
	}

	if err := json.Unmarshal(body, &openAIResponse); err == nil {
		// 处理OpenAI兼容响应
		for _, choice := range openAIResponse.Choices {
			result += choice.Message.Content
		}
	} else {
		// 直接返回原始响应
		result = string(body)
	}

	return result, nil
}

func streamAliyunResponse(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var builder strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}

		content := extractChunkContent(payload)
		if content == "" {
			continue
		}

		builder.WriteString(content)
		fmt.Print(content)
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	fmt.Println()
	return builder.String(), nil
}

// parseSSEBody 解析 SSE 格式响应，提取 data 行中的 delta/content 内容。
func parseSSEBody(body []byte) (string, bool) {
	lines := bytes.Split(body, []byte("\n"))
	var builder strings.Builder
	foundSSE := false

	for _, raw := range lines {
		line := bytes.TrimSpace(raw)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}

		foundSSE = true
		payload := strings.TrimSpace(string(bytes.TrimPrefix(line, []byte("data:"))))
		if payload == "" || payload == "[DONE]" {
			continue
		}

		content := extractChunkContent(payload)
		if content == "" {
			continue
		}
		builder.WriteString(content)
	}

	if !foundSSE {
		return "", false
	}

	return builder.String(), true
}

func extractChunkContent(payload string) string {
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Output struct {
			Text string `json:"text"`
		} `json:"output"`
	}

	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return ""
	}

	var builder strings.Builder
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			builder.WriteString(choice.Delta.Content)
			continue
		}
		if choice.Message.Content != "" {
			builder.WriteString(choice.Message.Content)
		}
	}

	if builder.Len() > 0 {
		return builder.String()
	}

	return chunk.Output.Text
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
