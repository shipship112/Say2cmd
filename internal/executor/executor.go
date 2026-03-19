package executor

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ExecutionResult 包含命令执行结果
type ExecutionResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// ExecuteCommand 执行命令
func ExecuteCommand(script string) (*ExecutionResult, error) {
	// 验证命令是否合法
	if err := validateCommand(script); err != nil {
		return &ExecutionResult{
			Stderr:   err.Error(),
			ExitCode: -1,
		}, err
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd.exe", "/c", script)
	} else {
		cmd = exec.Command("sh", "-c", script)
	}

	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer

	// 将命令的标准输出和标准错误同时写入缓冲区和控制台
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuffer)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuffer)
	cmd.Stdin = os.Stdin

	err := cmd.Run()
	result := &ExecutionResult{
		Stdout:   stdoutBuffer.String(),
		Stderr:   stderrBuffer.String(),
		ExitCode: exitCode(cmd, err),
	}

	return result, err
}

func validateCommand(script string) error {
	fields := strings.Fields(strings.TrimSpace(script))
	if len(fields) == 0 {
		return fmt.Errorf("empty command is not allowed")
	}

	command := normalizeCommandName(fields[0])
	if isForbiddenShellLauncher(command) {
		return fmt.Errorf("shell launcher commands are not allowed: %s", fields[0])
	}

	return nil
}

func normalizeCommandName(command string) string {
	command = strings.Trim(command, "\"'")
	command = strings.ReplaceAll(command, "\\", "/")
	return strings.ToLower(filepath.Base(command))
}

func isForbiddenShellLauncher(command string) bool {
	switch command {
	case "bash", "sh", "zsh", "fish", "dash", "ksh", "cmd", "cmd.exe", "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return true
	default:
		return false
	}
}

func exitCode(cmd *exec.Cmd, err error) int {
	if err == nil {
		return 0
	}

	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}

	return -1
}
