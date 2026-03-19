package utils

import (
	"os"
	"runtime"
	"strings"
)

// GetShell 获取当前shell
func GetShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		if runtime.GOOS == "windows" {
			return "cmd"
		}
		return "bash"
	}
	return shell
}

// GetOS 获取当前操作系统
func GetOS() string {
	return runtime.GOOS
}

// ExtractScript 从响应中提取脚本，移除代码块标记
func ExtractScript(response string) string {
	// 首先去除首尾的空白字符
	response = strings.TrimSpace(response)
	// 移除开头的代码块标记
	if strings.HasPrefix(response, "```") {
		// 可能存在语言标识（如 ```bash），先移除 ```，再处理语言标识
		response = strings.TrimPrefix(response, "```")
		// 处理可能的语言标识
		response = strings.TrimSpace(response)
		// 如果包含换行，说明可能有语言标识，尝试提取真正的命令行
		if strings.Contains(response, "\n") {
			//根据换行分割，第一行可能是语言标识，后续行才是命令
			lines := strings.Split(response, "\n")
			firstLine := strings.TrimSpace(lines[0])
			// 第一行是语言标识（如 bash/sh/zsh），跳过它取真正的命令行0
			if isShellTag(firstLine) {
				// 从第二行开始寻找第一个非空行，作为命令行
				for _, line := range lines[1:] {
					line = strings.TrimSpace(line)
					// 只要不是空行或代码块结束标记，就认为是命令行
					if line != "" && line != "```" {
						response = line
						break
					}
				}
			} else {
				// 没有语言标识，直接使用第一行作为命令行
				response = firstLine
			}
		}
	}

	// 移除末尾的代码块标记
	if strings.HasSuffix(response, "```") {
		response = strings.TrimSuffix(response, "```")
	}
	// 移除残余反引号
	response = strings.TrimPrefix(response, "`")
	response = strings.TrimSuffix(response, "`")
	// 最后去除首尾的空白字符，确保返回干净的命令行
	return strings.TrimSpace(response)

}

// isShellTag 判断字符串是否是 shell 语言标识符
func isShellTag(s string) bool {
	switch strings.ToLower(s) {
	case "bash", "sh", "zsh", "fish", "cmd", "powershell", "pwsh", "shell":
		return true
	}
	return false
}

// ListFiles 列出指定目录中的文件和目录
func ListFiles(dir string) ([]string, error) {
	// 如果 dir 为空，默认使用当前目录
	if dir == "" {
		dir = "."
	}
 // 读取目录内容
	entries, err := os.ReadDir(dir)

	if err != nil {
		return nil, err
	}
	var files []string
	// 遍历目录项，区分文件和目录
	for _, entry := range entries {
		// 目录前加上 [DIR] 标识，文件前加上 [FILE] 标识
		if entry.IsDir() {
			files = append(files, "[DIR] "+entry.Name())
		} else {
			files = append(files, "[FILE] "+entry.Name())
		}
	}
	
	return files, nil
}

// FileExists 检查文件是否存在
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// GetCurrentDir 获取当前工作目录
func GetCurrentDir() (string, error) {
	return os.Getwd()
}

// GetFileContent 读取文件内容
func GetFileContent(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
