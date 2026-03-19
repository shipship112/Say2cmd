package rag

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultFeedbackMaxWeight = 10
	defaultFeedbackMinWeight = -10
)

// FeedbackStore 记录命令执行反馈，用于检索重排时的学习权重。
type FeedbackStore struct {
	path    string
	weights map[string]int
}

// LoadFeedback 加载反馈权重。
func LoadFeedback(path string) (*FeedbackStore, error) {
	if strings.TrimSpace(path) == "" {
		defaultPath, err := DefaultFeedbackPath()
		if err != nil {
			return nil, err
		}
		path = defaultPath
	}

	store := &FeedbackStore{
		path:    path,
		weights: make(map[string]int),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return nil, err
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return store, nil
	}

	if err := json.Unmarshal(data, &store.weights); err != nil {
		return nil, err
	}

	return store, nil
}

// DefaultFeedbackPath 返回默认反馈文件路径。
func DefaultFeedbackPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".github/xiuivfbc/NaturalCmd_rag_feedback.json"), nil
}

// RecordSuccess 记录成功执行，提升对应命令权重。
func (s *FeedbackStore) RecordSuccess(script string) error {
	return s.adjust(script, 1)
}

// RecordFailure 记录失败执行，降低对应命令权重。
func (s *FeedbackStore) RecordFailure(script string) error {
	return s.adjust(script, -1)
}

// WeightForScript 返回脚本权重。
func (s *FeedbackStore) WeightForScript(script string) int {
	if s == nil {
		return 0
	}

	return s.weights[normalizeScriptKey(script)]
}

func (s *FeedbackStore) adjust(script string, delta int) error {
	if s == nil {
		return nil
	}

	key := normalizeScriptKey(script)
	if key == "" {
		return nil
	}

	next := s.weights[key] + delta
	if next > defaultFeedbackMaxWeight {
		next = defaultFeedbackMaxWeight
	}
	if next < defaultFeedbackMinWeight {
		next = defaultFeedbackMinWeight
	}

	s.weights[key] = next
	return s.save()
}

func (s *FeedbackStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s.weights, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0o600)
}

func normalizeScriptKey(script string) string {
	script = strings.ToLower(strings.TrimSpace(script))
	if script == "" {
		return ""
	}
	return strings.Join(strings.Fields(script), " ")
}
