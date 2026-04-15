package rag

import (
	"fmt"
	"sort"
	"strings"

	"github/shipship112/Say2cmd/internal/history"
	"github/shipship112/Say2cmd/internal/tokenizer"
)

const defaultTopK = 3 // 默认返回前 3 条最相关的历史记录

// MatchResult 表示一次历史检索的结果及其命中分值。
type MatchResult struct {
	Context   string  // 检索到的历史记录文本（作为 AI 上下文）
	BestScore int     // 最佳匹配分值（用于判断是否命中）
	Coverage  float64 // 覆盖率：匹配词数 / 总词数（0.0-1.0）
}

// BuildHistoryContext 从历史记录中检索与当前查询最相关的上下文，供模型生成命令时参考。
func BuildHistoryContext(query string, store *history.Store, topK int) string {
	return BuildHistoryContextWithFeedback(query, store, nil, topK)
}

// BuildHistoryContextWithFeedback 在历史检索基础上叠加执行反馈权重。
func BuildHistoryContextWithFeedback(query string, store *history.Store, feedback *FeedbackStore, topK int) string {
	return BuildHistoryMatchWithFeedback(query, store, feedback, topK).Context
}

// BuildHistoryMatchWithFeedback 在历史检索基础上叠加执行反馈权重，并返回命中分值。
func BuildHistoryMatchWithFeedback(query string, store *history.Store, feedback *FeedbackStore, topK int) MatchResult {
	if store == nil {
		return MatchResult{}
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return MatchResult{}
	}

	entries := store.Search("")
	if len(entries) == 0 {
		return MatchResult{}
	}

	if topK <= 0 {
		topK = defaultTopK
	}

	queryTokens := sliceToTokenSet(tokenizer.TokenizeForSearch(query))
	if len(queryTokens) == 0 {
		return MatchResult{}
	}
	totalQueryTokens := len(queryTokens)

	type scoredEntry struct {
		entry    history.Entry
		score    int
		coverage float64
		idx      int
	}

	results := make([]scoredEntry, 0, len(entries))
	for index, entry := range entries {
		score, matchedCount := scoreEntry(queryTokens, entry, feedback)
		if score <= 0 {
			continue
		}
		coverage := 0.0
		if totalQueryTokens > 0 {
			coverage = float64(matchedCount) / float64(totalQueryTokens)
		}
		results = append(results, scoredEntry{entry: entry, score: score, coverage: coverage, idx: index})
	}

	if len(results) == 0 {
		return MatchResult{}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].score == results[j].score {
			// 保持最近历史优先（Store.Search("") 已按最近在前）
			return results[i].idx < results[j].idx
		}
		return results[i].score > results[j].score
	})

	if len(results) > topK {
		results = results[:topK]
	}

	var builder strings.Builder
	builder.WriteString("Relevant command history (reference only):\n")
	for i, item := range results {
		builder.WriteString(fmt.Sprintf("%d. User intent: %s\n", i+1, strings.TrimSpace(item.entry.Prompt)))
		builder.WriteString(fmt.Sprintf("   Command: %s\n", strings.TrimSpace(item.entry.Script)))
	}

	builder.WriteString("Use these as hints when appropriate, but output a command that best matches the current request.\n")
	return MatchResult{
		Context:   builder.String(),
		BestScore: results[0].score,
		Coverage:  results[0].coverage,
	}
}

func scoreEntry(queryTokens map[string]struct{}, entry history.Entry, feedback *FeedbackStore) (int, int) {
	// 1. 准备历史记录的 token 集合（Prompt部分）
	promptTokens := sliceToTokenSet(entry.PromptTokens)
	if len(promptTokens) == 0 {
		// 如果没有预分词，实时分词
		promptTokens = sliceToTokenSet(tokenizer.TokenizeForSearch(strings.TrimSpace(entry.Prompt)))
	}

	// 2. 准备历史记录的 token 集合（Script部分）
	scriptTokens := sliceToTokenSet(entry.ScriptTokens)
	if len(scriptTokens) == 0 {
		// 如果没有预分词，实时分词
		scriptTokens = sliceToTokenSet(tokenizer.TokenizeForSearch(strings.TrimSpace(entry.Script)))
	}

	// 3. 计算匹配分值
	score := 0
	matchedCount := 0
	for token := range queryTokens {
		matched := false
		
		// 3.1 在 Prompt 中匹配 → +3 分（权重更高）
		if _, ok := promptTokens[token]; ok {
			score += 3
			matched = true
		}
		
		// 3.2 在 Script 中匹配 → +1 分（权重较低）
		if _, ok := scriptTokens[token]; ok {
			score += 1
			matched = true
		}
		
		// 3.3 统计匹配的词数（用于计算覆盖率）
		if matched {
			matchedCount++
		}
	}

	// 4. 叠加反馈权重
	// 反馈权重范围：[-10, +10]
	// 叠加权重 × 2（成功命令会获得更高分值）
	if feedback != nil {
		score += feedback.WeightForScript(entry.Script) * 2
	}

	return score, matchedCount
}

func sliceToTokenSet(tokens []string) map[string]struct{} {
	// 1. 空切片检查
	if len(tokens) == 0 {
		return nil
	}

	// 2. 创建 map 用于去重
	// 使用空结构体 struct{} 作为 value，节省内存
	set := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		// 3. 预处理：去空格、转小写
		value := strings.TrimSpace(strings.ToLower(token))
		if value == "" {
			continue
		}
		// 4. 添加到集合（自动去重）
		set[value] = struct{}{}
	}

	// 5. 如果集合为空，返回 nil
	if len(set) == 0 {
		return nil
	}

	return set
}
