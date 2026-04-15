package rag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github/shipship112/Say2cmd/internal/history"
)

func TestBuildHistoryContextReturnsRelevantEntries(t *testing.T) {
	store, err := history.Load(filepath.Join(t.TempDir(), "history.json"), 10)
	if err != nil {
		t.Fatalf("load history store: %v", err)
	}

	_ = store.Add("查看 git 分支", "git branch")
	_ = store.Add("查看 docker 容器", "docker ps")
	_ = store.Add("查看 git 状态", "git status")

	ctx := BuildHistoryContext("git", store, 2)
	if ctx == "" {
		t.Fatalf("expected non-empty context")
	}

	if !strings.Contains(ctx, "git") {
		t.Fatalf("expected git-related content, got: %s", ctx)
	}

	if strings.Contains(ctx, "docker ps") {
		t.Fatalf("expected docker entry filtered out for top 2 git results: %s", ctx)
	}
}

func TestBuildHistoryContextHandlesEmptyStore(t *testing.T) {
	store, err := history.Load(filepath.Join(t.TempDir(), "history.json"), 10)
	if err != nil {
		t.Fatalf("load history store: %v", err)
	}

	ctx := BuildHistoryContext("git", store, 3)
	if ctx != "" {
		t.Fatalf("expected empty context, got: %s", ctx)
	}
}

func TestBuildHistoryContextWithFeedbackPrefersSuccessfulCommand(t *testing.T) {
	store, err := history.Load(filepath.Join(t.TempDir(), "history.json"), 10)
	if err != nil {
		t.Fatalf("load history store: %v", err)
	}

	_ = store.Add("列出文件", "ls -la")
	_ = store.Add("列出文件", "find . -type f")

	feedbackStore, err := LoadFeedback(filepath.Join(t.TempDir(), "feedback.json"))
	if err != nil {
		t.Fatalf("load feedback store: %v", err)
	}

	_ = feedbackStore.RecordSuccess("find . -type f")
	_ = feedbackStore.RecordSuccess("find . -type f")
	_ = feedbackStore.RecordFailure("ls -la")

	ctx := BuildHistoryContextWithFeedback("列出 文件", store, feedbackStore, 1)
	if !strings.Contains(ctx, "find . -type f") {
		t.Fatalf("expected successful command to be preferred, got: %s", ctx)
	}
}

func TestFeedbackStoreRecordsWeight(t *testing.T) {
	feedbackStore, err := LoadFeedback(filepath.Join(t.TempDir(), "feedback.json"))
	if err != nil {
		t.Fatalf("load feedback store: %v", err)
	}

	_ = feedbackStore.RecordSuccess("git status")
	_ = feedbackStore.RecordSuccess("git status")
	_ = feedbackStore.RecordFailure("git status")

	if got := feedbackStore.WeightForScript("git status"); got != 1 {
		t.Fatalf("expected weight 1, got %d", got)
	}
}

func TestBuildHistoryContextChineseWithoutWhitespace(t *testing.T) {
	store, err := history.Load(filepath.Join(t.TempDir(), "history.json"), 10)
	if err != nil {
		t.Fatalf("load history store: %v", err)
	}

	_ = store.Add("查看远程仓库", "git remote -v")
	_ = store.Add("查看本地分支", "git branch")

	ctx := BuildHistoryContext("远程仓库", store, 1)
	if !strings.Contains(ctx, "git remote -v") {
		t.Fatalf("expected chinese no-whitespace query to match remote command, got: %s", ctx)
	}
}

func TestBuildHistoryContextFallbackForLegacyEntriesWithoutTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	legacy := `[
		{"prompt":"查看远程仓库","script":"git remote -v","updated_at":"2026-01-01T00:00:00Z"},
		{"prompt":"查看本地分支","script":"git branch","updated_at":"2026-01-01T00:00:01Z"}
	]`

	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy history: %v", err)
	}

	store, err := history.Load(path, 10)
	if err != nil {
		t.Fatalf("load history store: %v", err)
	}

	ctx := BuildHistoryContext("远程仓库", store, 1)
	if !strings.Contains(ctx, "git remote -v") {
		t.Fatalf("expected legacy entries without tokens to still match, got: %s", ctx)
	}
}

func TestBuildHistoryMatchWithFeedbackProvidesBestScore(t *testing.T) {
	store, err := history.Load(filepath.Join(t.TempDir(), "history.json"), 10)
	if err != nil {
		t.Fatalf("load history store: %v", err)
	}

	_ = store.Add("查看 git 状态", "git status")
	_ = store.Add("查看 docker 容器", "docker ps")

	match := BuildHistoryMatchWithFeedback("git 状态", store, nil, 1)
	if match.Context == "" {
		t.Fatalf("expected non-empty match context")
	}
	if match.BestScore <= 0 {
		t.Fatalf("expected positive best score, got %d", match.BestScore)
	}
	if match.Coverage <= 0 {
		t.Fatalf("expected positive coverage, got %f", match.Coverage)
	}
	if !strings.Contains(match.Context, "git status") {
		t.Fatalf("expected git status in top result, got: %s", match.Context)
	}
}

func TestBuildHistoryContextWrapperKeepsCompatibility(t *testing.T) {
	store, err := history.Load(filepath.Join(t.TempDir(), "history.json"), 10)
	if err != nil {
		t.Fatalf("load history store: %v", err)
	}

	_ = store.Add("查看远程仓库", "git remote -v")

	ctx := BuildHistoryContextWithFeedback("远程仓库", store, nil, 1)
	if ctx == "" {
		t.Fatalf("expected context from compatibility wrapper")
	}
}
