package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/joho/godotenv"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"

	"github/xiuivfbc/NaturalCmd/internal/completion"
	"github/xiuivfbc/NaturalCmd/internal/config"
	"github/xiuivfbc/NaturalCmd/internal/executor"
	"github/xiuivfbc/NaturalCmd/internal/history"
	"github/xiuivfbc/NaturalCmd/internal/rag"
	"github/xiuivfbc/NaturalCmd/internal/ui"
)

// bundle 全局 i18n bundle，用于国际化翻译
// 在 init() 函数中初始化，加载 en.json 和 zh.json
var bundle *i18n.Bundle

// main 程序入口函数
// 执行流程：
// 1. 加载 .env 文件（godotenv.Load）
// 2. 解析命令行参数（-p 提示、-h 历史搜索、-s 静默模式）
// 3. 加载配置（config.Load）
// 4. 检查 API 密钥
// 5. 加载历史记录和 RAG 反馈数据
// 6. 处理历史记录查询（如使用 -h 参数）
// 7. 进入主循环：获取用户输入 → 生成命令 → 用户确认 → 执行命令 → 记录结果
func main() {
	// 步骤 1：加载 .env 文件
	// godotenv.Load() 会从项目根目录读取 .env 文件
	// 并将其中定义的环境变量设置到 os.Getenv() 可读取的位置
	// 如果 .env 文件不存在，仅打印警告，不影响程序运行
	if err := godotenv.Load(); err != nil {
		// 创建默认本地化器（英文）
		// 注意：此时 bundle 已经由 init() 初始化完成
		localizer := i18n.NewLocalizer(bundle, "en")
		fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
			MessageID: "envFileNotFound",
		}))
	}

	// 步骤 2：声明命令行参数变量
	var prompt string          // 用户输入的提示词
	var historyQuery string     // 历史记录搜索关键词
	var silent bool             // 是否静默模式（不生成命令解释）

	// 定义命令行标志
	// -p: 直接指定提示词（例如：./ai -p "列出当前目录文件"）
	// -h: 搜索历史记录（例如：./ai -h "git" 或 ./ai -h）
	// -s: 跳过解释生成（仅生成命令）
	flag.StringVar(&prompt, "p", "", "Prompt to run")
	flag.StringVar(&historyQuery, "h", "", "Search prompt history")
	flag.BoolVar(&silent, "s", false, "Skip explanation generation")

	// 参数规范化处理
	// normalizeArgs() 将组合参数（如 -hs、-ps）展开为标准格式
	// 例如：-hs → -s -h=
	// 返回值：
	//   - normalizedArgs: 规范化后的参数列表
	//   - historyFlagUsed: 是否使用了 -h 参数（用于判断是否要展示历史列表）
	normalizedArgs, historyFlagUsed := normalizeArgs(os.Args[1:])

	// 解析命令行参数
	// flag.CommandLine.Parse() 会根据上面定义的 flag.StringVar 等解析参数
	// 解析结果会填充到 prompt、historyQuery、silent 变量中
	if err := flag.CommandLine.Parse(normalizedArgs); err != nil {
		os.Exit(2)
	}

	// 判断是否请求查看全部历史记录
	// historyFlagUsed 为 true 表示使用了 -h 参数
	// historyQuery 为空字符串表示 -h 后面没有跟搜索词
	// 此时应该展示所有历史记录供用户选择
	historyRequested := historyFlagUsed && strings.TrimSpace(historyQuery) == ""

	// 如果命令行参数中没有通过 -p 指定 prompt
	// 则从剩余参数中获取（例如：./ai "列出文件"）
	if prompt == "" {
		prompt = strings.Join(flag.Args(), " ")
	}

	// 步骤 3：加载配置
	// loadConfig() 会：
	//   1. 调用 config.Load() 从环境变量读取配置
	//   2. 根据 cfg.Language 创建对应的本地化器
	//   3. 返回配置对象和本地化器
	cfg, localizer, err := loadConfig()
	if err != nil {
		fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
			MessageID: "errorLoadingConfig",
			TemplateData: map[string]interface{}{
				"Error": err,
			},
		}))
		os.Exit(1)
	}

	// 步骤 4：检查 API 密钥是否设置
	// API_KEY 是必填项，如果没有设置则提示用户并退出
	if !checkAPIKey(cfg, localizer) {
		os.Exit(1)
	}

	// 步骤 5：加载历史记录
	// history.Load() 会：
	//   1. 从指定路径（或默认路径 ~/.github/xiuivfbc/NaturalCmd_history.json）读取 JSON 文件
	//   2. 解析为 []Entry 列表
	//   3. 返回 Store 对象，提供 Add() 和 Search() 方法
	// 注意：历史记录加载失败不会导致程序退出，仅打印警告
	historyStore, err := history.Load(cfg.HistoryFile, cfg.HistoryMax)
	if err != nil {
		fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
			MessageID: "errorLoadingHistory",
			TemplateData: map[string]interface{}{
				"Error": err,
			},
		}))
	}

	// 步骤 6：加载 RAG 反馈数据（如果启用 RAG）
	// FeedbackStore 记录每个命令的成功/失败执行次数
	// 用于 RAG 检索时调整权重（成功+1，失败-1）
	// 反馈文件默认路径：~/.github/xiuivfbc/NaturalCmd_rag_feedback.json
	var feedbackStore *rag.FeedbackStore
	if cfg.RAGEnabled {
		feedbackStore, err = rag.LoadFeedback(cfg.RAGFeedbackFile)
		if err != nil {
			fmt.Printf("Warning: failed to load rag feedback store: %v\n", err)
		}
	}

	// 步骤 7：处理历史记录查询和选择
	// 如果用户使用了 -h 参数（historyRequested 为 true 或 historyQuery 不为空）
	// 则展示历史记录列表供用户选择
	if historyStore != nil && (historyRequested || historyQuery != "") {
		// promptFromHistory() 会：
		//   1. 调用 historyStore.Search(query) 搜索匹配的历史记录
		//   2. 使用 survey.Select 展示选项列表
		//   3. 返回用户选择的 prompt
		// 返回值：
		//   - resolvedPrompt: 用户选择的 prompt（或空字符串）
		//   - selectedFromHistory: 是否从历史记录中选择了
		//   - shouldExit: 是否应该退出程序（用户选择了"都不是"或按 Ctrl+C）
		resolvedPrompt, selectedFromHistory, shouldExit := promptFromHistory(historyQuery, historyStore, localizer)
		if shouldExit {
			fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
				MessageID: "goodbyeMessage",
			}))
			os.Exit(0)
		}
		// 如果用户从历史记录中选择了某个 prompt，则用它替换当前的 prompt
		if selectedFromHistory {
			prompt = resolvedPrompt
		}
	}

	// 步骤 8：进入主循环
	// 外层循环：处理多个连续的任务
	// 内层循环：处理单个任务的多次尝试（失败重试）
	for {
		// 8.1 获取用户输入（如果没有通过命令行参数指定 prompt）
		if prompt == "" {
			prompt = getUserPrompt(localizer, historyStore)
			if prompt == "" {
				// 用户输入为空（回车），继续循环（可能清屏）
				continue
			}
		}

		// 保存用户的原始输入（用于记录历史）
		// initialPrompt 在整个任务处理过程中保持不变
		initialPrompt := strings.TrimSpace(prompt)

		// executionFeedback 用于失败重试
		// 初始为空字符串，表示没有执行失败的信息
		// 如果命令执行失败，会将错误信息填充到这里，然后重新生成命令
		executionFeedback := ""

		// 内层循环：处理单次任务的多次尝试
		// 每次循环都会：
		//   1. 生成命令和解释
		//   2. 询问用户是否执行
		//   3. 根据用户选择执行/重试/取消
		//   4. 如果执行失败，构建错误反馈并重新生成
		for {
			// 8.2 生成脚本和解释
			// generateScriptAndExplanation() 会：
			//   1. 如果启用 RAG，从历史记录中检索相关命令作为上下文
			//   2. 如果本地检索未命中且启用语义扩展，调用 AI 扩展搜索词
			//   3. 将 RAG 上下文和 executionFeedback 追加到 prompt
			//   4. 调用 AI API 生成命令
			//   5. 如果非静默模式，调用 AI API 生成解释（流式输出）
			script, err := generateScriptAndExplanation(prompt, executionFeedback, cfg, localizer, historyStore, feedbackStore, silent)
			if err != nil {
				os.Exit(1)
			}

			// 8.3 询问用户是否执行生成的命令
			// promptUserForAction() 使用 survey.Select 展示三个选项：
			//   - "必须呀"（confirm）：执行命令
			//   - "换一个吧。。。"（retry）：输入补充信息重新生成
			//   - "算了,不执行了,你个SB。。。"（cancel）：退出程序
			selectedOption := promptUserForAction(localizer)
			if selectedOption == "" {
				// 用户选择无效，重新生成
				continue
			}

			if selectedOption == "confirm" {
				// 8.4 用户选择执行命令
				// executeCommand() 会：
				//   1. 清屏（如果终端支持）
				//   2. 打印执行提示
				//   3. 调用 executor.ExecuteCommand() 执行命令
				//   4. 实时输出 stdout 和 stderr 到终端
				result, err := executeCommand(script, localizer)

				// 8.5 处理命令执行结果
				if err != nil {
					// 命令执行失败
					// 记录失败反馈（降低该命令在 RAG 检索中的权重）
					if feedbackStore != nil {
						_ = feedbackStore.RecordFailure(script)
					}

					// 打印错误信息
					fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
						MessageID: "errorExecutingCommand",
						TemplateData: map[string]interface{}{
							"Error": err,
						},
					}))
					fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
						MessageID: "retryingAfterExecutionError",
					}))

					// 构建执行反馈信息
					// buildExecutionFeedback() 会将：
					//   - 失败的命令
					//   - 错误信息
					//   - 退出码
					//   - 捕获的 stdout/stderr
					// 拼接成一个反馈字符串，追加到下一次生成命令的 prompt 中
					executionFeedback = buildExecutionFeedback(script, result, err)

					// 继续内层循环，重新生成命令
					continue
				}

				// 命令执行成功
				// 8.6 保存到历史记录（如果 historyStore 存在）
				if historyStore != nil {
					if err := historyStore.Add(initialPrompt, script); err != nil {
						fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
							MessageID: "errorSavingHistory",
							TemplateData: map[string]interface{}{
								"Error": err,
							},
						}))
					}
				}

				// 8.7 记录成功反馈（提高该命令在 RAG 检索中的权重）
				if feedbackStore != nil {
					_ = feedbackStore.RecordSuccess(script)
				}

				// 8.8 打印成功庆祝横幅
				printSuccessCelebration(localizer, initialPrompt, script)

				// 8.9 清空 prompt，准备处理下一个任务
				prompt = ""

				// 跳出内层循环，回到外层循环等待下一个用户输入
				break
			} else if selectedOption == "cancel" {
				// 用户选择取消，退出程序
				fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
					MessageID: "goodbyeMessage",
				}))
				os.Exit(0)
			} else if selectedOption == "retry" {
				// 用户选择重试（不得行）
				// 8.10 让用户输入补充信息
				var additionalInfo string
				for additionalInfo == "" {
					// 调用 ui.GetAdditionalInfo() 提示用户输入补充信息
					value, err := ui.GetAdditionalInfo(localizer)
					if err != nil {
						// 处理 Ctrl+C 中断
						if ui.IsInterrupt(err) {
							fmt.Println()
							fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
								MessageID: "goodbyeMessage",
							}))
							os.Exit(0)
						}
						// 其他错误，打印提示并继续等待输入
						fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
							MessageID: "errorReadingInput",
							TemplateData: map[string]interface{}{
								"Error": err,
							},
						}))
						continue
					}
					additionalInfo = value
				}

				// 8.11 更新 prompt，添加补充信息
				// 例如：原 prompt "列出文件" + 补充 "只显示 Go 文件"
				// → 新 prompt "列出文件 只显示 Go 文件"
				prompt = prompt + " " + additionalInfo

				// 清空 executionFeedback，因为这是新的尝试
				executionFeedback = ""

				// 继续内层循环，使用新的 prompt 重新生成命令
				continue
			}
		}
	}
}

// init 包初始化函数
// 在 main() 执行之前自动调用，用于全局初始化
// 执行时机：Go 运行时会先执行 init()，再执行 main()
// 作用：初始化 i18n bundle，加载多语言翻译文件
func init() {
	// 1. 创建 i18n bundle，指定默认语言为英语
	// bundle 是翻译文件的容器，用于存储和查找翻译条目
	bundle = i18n.NewBundle(language.English)

	// 2. 注册 JSON 反序列化函数
	// 翻译文件使用 JSON 格式，需要告诉 bundle 如何解析 JSON
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)

	// 3. 加载英文翻译文件
	// locales/en.json 包含所有英文翻译条目
	// 如果加载失败，打印错误但不影响程序运行（会使用 MessageID 作为输出）
	_, err := bundle.LoadMessageFile("locales/en.json")
	if err != nil {
		fmt.Printf("Error loading en.json: %v\n", err)
	}

	// 4. 加载中文翻译文件
	// locales/zh.json 包含所有中文翻译条目（包括方言风格）
	// 如果加载失败，打印错误但不影响程序运行
	_, err = bundle.LoadMessageFile("locales/zh.json")
	if err != nil {
		fmt.Printf("Error loading zh.json: %v\n", err)
	}
}

// loadConfig 加载配置并返回配置对象和本地化器
// 执行流程：
// 1. 调用 config.Load() 从环境变量加载配置
// 2. 根据 cfg.Language 创建对应的本地化器
// 3. 返回配置对象和本地化器
//
// 返回值：
//   - *config.Config: 配置对象，包含所有配置项
//   - *i18n.Localizer: 本地化器，用于翻译文本
//   - error: 错误信息（如果加载失败）
func loadConfig() (*config.Config, *i18n.Localizer, error) {
	// 1. 加载配置
	// config.Load() 会从环境变量读取所有配置项
	// 包括：API_KEY、PROVIDER、MODEL、LANGUAGE、RAG_ENABLED 等
	cfg, err := config.Load()
	if err != nil {
		// 配置加载失败，创建默认本地化器（英文）
		// 此时无法使用 cfg.Language，因为 cfg 可能为 nil
		localizer := i18n.NewLocalizer(bundle, "en")
		return nil, localizer, err
	}

	// 2. 创建本地化器
	// Localizer 根据 cfg.Language 选择对应的翻译文件
	// 例如：cfg.Language = "zh" → 使用 zh.json 的翻译
	//       cfg.Language = "en" → 使用 en.json 的翻译
	// localizer.MustLocalize() 会根据 MessageID 查找对应的翻译文本
	localizer := i18n.NewLocalizer(bundle, cfg.Language)

	// 3. 返回配置和本地化器
	return cfg, localizer, nil
}

// checkAPIKey 检查 API 密钥是否设置
// API_KEY 是必填项，没有 API 密钥无法调用 AI 模型
//
// 参数：
//   - cfg: 配置对象
//   - localizer: 本地化器，用于翻译提示文本
//
// 返回值：
//   - true: API 密钥已设置
//   - false: API 密钥未设置（会打印提示信息）
func checkAPIKey(cfg *config.Config, localizer *i18n.Localizer) bool {
	// 检查 API_KEY 是否为空字符串
	if cfg.APIKey == "" {
		// API_KEY 未设置，打印错误提示
		fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
			MessageID: "errorApiKeyNotSet",
		}))

		// 打印设置 API 密钥的提示
		fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
			MessageID: "setApiKeyHint",
		}))

		// 返回 false，表示 API 密钥未设置
		return false
	}

	// API_KEY 已设置，返回 true
	return true
}

// getUserPrompt 获取用户输入的 prompt
// 在交互式模式下，循环等待用户输入，直到获得有效的 prompt
//
// 功能：
// 1. 调用 ui.GetPrompt() 显示输入提示并获取用户输入
// 2. 处理 Ctrl+C 中断（优雅退出）
// 3. 支持内联历史查询（输入 "-h 关键词"）
// 4. 支持清屏（直接回车）
// 5. 过滤空输入
//
// 参数：
//   - localizer: 本地化器，用于翻译提示文本
//   - historyStore: 历史记录存储，用于内联历史查询
//
// 返回值：
//   - string: 用户输入的 prompt（已 trim）
func getUserPrompt(localizer *i18n.Localizer, historyStore *history.Store) string {
	prompt := ""

	// 循环等待用户输入，直到获得非空的 prompt
	for prompt == "" {
		// 1. 调用 ui.GetPrompt() 获取用户输入
		// ui.GetPrompt() 使用 survey.Input 封装，显示本地化的输入提示
		value, err := ui.GetPrompt(localizer)
		if err != nil {
			// 2. 处理错误
			if ui.IsInterrupt(err) {
				// 用户按 Ctrl+C，优雅退出
				fmt.Println()
				fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
					MessageID: "goodbyeMessage",
				}))
				os.Exit(0)
			}
			// 其他错误，打印提示并继续等待输入
			fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
				MessageID: "errorReadingInput",
				TemplateData: map[string]interface{}{
					"Error": err,
				},
			}))
			continue
		}

		// 3. 处理内联历史查询
		// 如果用户输入 "-h" 或 "-h 关键词"，则执行历史记录查询
		if historyStore != nil {
			if query, ok := parseInlineHistoryQuery(value); ok {
				// 调用 promptFromHistory() 展示历史记录列表
				resolvedPrompt, selectedFromHistory, shouldExit := promptFromHistory(query, historyStore, localizer)
				if shouldExit {
					// 用户选择了"都不是"或按 Ctrl+C，继续等待输入
					continue
				}
				if selectedFromHistory {
					// 用户从历史记录中选择了某个 prompt
					prompt = resolvedPrompt
				}
				continue
			}
		}

		// 4. 处理清屏
		// 如果用户直接回车（输入为空），则清屏
		if strings.TrimSpace(value) == "" {
			clearScreenIfSupported()
			continue
		}

		// 5. 获得有效的 prompt，退出循环
		prompt = value
	}

	return prompt
}

// parseInlineHistoryQuery 解析内联历史查询
// 支持用户在输入提示词时直接查询历史记录
//
// 支持的格式：
//   - "-h" : 查看全部历史记录
//   - "-h 关键词" : 搜索包含关键词的历史记录
//   - '-h "关键词"' : 支持引号包裹的关键词
//
// 参数：
//   - input: 用户输入的原始字符串
//
// 返回值：
//   - string: 搜索关键词（如果输入 "-h"，返回空字符串）
//   - bool: true 表示是历史查询命令，false 表示普通输入
func parseInlineHistoryQuery(input string) (string, bool) {
	value := strings.TrimSpace(input)

	// 情况 1: 用户输入 "-h"，表示查看全部历史记录
	if value == "-h" {
		return "", true
	}

	// 情况 2: 用户输入 "-h 关键词"，表示搜索历史记录
	if strings.HasPrefix(value, "-h ") {
		// 提取 "-h " 后面的部分作为搜索关键词
		query := strings.TrimSpace(strings.TrimPrefix(value, "-h "))
		// 去除可能存在的引号包裹（'-h "git"' → 'git'）
		query = strings.Trim(query, "\"'")
		return query, true
	}

	// 不是历史查询命令
	return "", false
}

// generateScriptAndExplanation 生成脚本和解释
// 这是核心的 AI 生成函数，整合了 RAG 检索、失败反馈、命令生成、解释生成
//
// 执行流程：
// 1. 如果启用 RAG，从历史记录中检索相关命令作为上下文
// 2. 如果本地检索未命中且启用语义扩展，调用 AI 扩展搜索词
// 3. 将 RAG 上下文和 executionFeedback 追加到 prompt
// 4. 调用 AI API 生成命令（非流式）
// 5. 如果非静默模式，调用 AI API 生成解释（流式输出）
//
// 参数：
//   - prompt: 用户的原始输入（自然语言描述）
//   - executionFeedback: 执行失败的反馈信息（空字符串表示首次生成）
//   - cfg: 配置对象
//   - localizer: 本地化器
//   - historyStore: 历史记录存储
//   - feedbackStore: RAG 反馈存储
//   - silent: 是否静默模式（不生成解释）
//
// 返回值：
//   - string: 生成的单行命令
//   - error: 错误信息（如果生成失败）
func generateScriptAndExplanation(prompt string, executionFeedback string, cfg *config.Config, localizer *i18n.Localizer, historyStore *history.Store, feedbackStore *rag.FeedbackStore, silent bool) (string, error) {
	// 1. 准备发送给 AI 的 prompt
	promptForModel := strings.TrimSpace(prompt)

	// 2. RAG 检索增强（如果启用）
	if cfg.RAGEnabled {
		// 2.1 从历史记录中检索相关命令
		// rag.BuildHistoryMatchWithFeedback() 会：
		//   - 使用 tokenizer 对 prompt 分词
		//   - 在历史记录中计算匹配分值
		//   - 叠加反馈权重
		//   - 返回 Top-K 相关历史记录的上下文
		match := rag.BuildHistoryMatchWithFeedback(prompt, historyStore, feedbackStore, cfg.RAGTopK)
		ragContext := match.Context

		// 2.2 判断本地检索是否命中
		// 命中条件：
		//   - 最佳分值 >= RAGMinLocalHit（默认 4）
		//   - 覆盖率 >= RAGMinLocalCover（默认 0.45）
		localHit := match.BestScore >= cfg.RAGMinLocalHit && match.Coverage >= cfg.RAGMinLocalCover

		// 2.3 如果本地检索未命中且启用语义扩展
		if !localHit && cfg.RAGSemanticExpand {
			// 调用 AI 扩展搜索词（如："列出文件" → "list, files, ls, dir"）
			expandedTerms, err := completion.GenerateQueryExpansion(prompt, cfg)
			if err == nil && strings.TrimSpace(expandedTerms) != "" {
				// 使用扩展后的搜索词重新检索历史记录
				expandedMatch := rag.BuildHistoryMatchWithFeedback(prompt+" "+expandedTerms, historyStore, feedbackStore, cfg.RAGTopK)
				if expandedMatch.BestScore >= cfg.RAGMinLocalHit && expandedMatch.Coverage >= cfg.RAGMinLocalCover {
					// 扩展后命中，使用扩展检索的上下文
					ragContext = expandedMatch.Context
				} else {
					// 扩展后仍未命中，清空上下文
					ragContext = ""
				}
			} else {
				// 扩展失败，清空上下文
				ragContext = ""
			}
		}

		// 2.4 如果检索到相关上下文，追加到 prompt
		if ragContext != "" {
			promptForModel += "\n\n" + ragContext
		}
	}

	// 3. 如果有执行失败反馈，追加到 prompt
	// executionFeedback 包含：
	//   - 失败的命令
	//   - 错误信息
	//   - 退出码
	//   - stdout/stderr
	if strings.TrimSpace(executionFeedback) != "" {
		promptForModel += executionFeedback
	}

	// 4. 调用 AI API 生成命令
	// completion.GenerateScript() 会：
	//   - 构建完整的 prompt（包含环境信息：Shell、OS、当前目录等）
	//   - 调用 OpenAI 或阿里云 API
	//   - 解析响应，提取单行命令
	script, err := completion.GenerateScript(promptForModel, cfg)
	if err != nil {
		fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
			MessageID: "errorGeneratingScript",
			TemplateData: map[string]interface{}{
				"Error": err,
			},
		}))
		return "", err
	}

	// 5. 打印生成的命令
	fmt.Printf("\n%s\n", localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID: "generatedScript",
		TemplateData: map[string]interface{}{
			"Script": script,
		},
	}))

	// 6. 生成解释（如果非静默模式）
	if !silent && !cfg.SilentMode {
		fmt.Printf("\n%s\n", localizer.MustLocalize(&i18n.LocalizeConfig{
			MessageID: "explanation",
		}))
		// completion.GenerateExplanation() 会：
		//   - 使用流式响应实时输出解释
		//   - 边生成边打印，提升用户体验
		_, err := completion.GenerateExplanation(script, cfg)
		if err != nil {
			fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
				MessageID: "errorGeneratingExplanation",
				TemplateData: map[string]interface{}{
					"Error": err,
				},
			}))
		}
	}

	// 7. 返回生成的命令
	return script, nil
}

// promptUserForAction 提示用户选择操作（执行/重试/取消）
// 使用 survey.Select 展示三个选项，让用户选择如何处理生成的命令
//
// 选项：
//   - "要得"（executeScriptOptionConfirm）：执行生成的命令
//   - "不得行"（executeScriptOptionRetry）：输入补充信息重新生成
//   - "算了"（executeScriptOptionCancel）：退出程序
//
// 参数：
//   - localizer: 本地化器，用于翻译选项文本
//
// 返回值：
//   - "confirm": 用户选择执行命令
//   - "retry": 用户选择重新生成
//   - "cancel": 用户选择取消
//   - "": 无效选择（用户按 Ctrl+C 或其他错误）
func promptUserForAction(localizer *i18n.Localizer) string {
	var selectedOption string

	// 1. 获取三个选项的翻译文本
	confirmOption := localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID: "executeScriptOptionConfirm",
	})
	retryOption := localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID: "executeScriptOptionRetry",
	})
	cancelOption := localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID: "executeScriptOptionCancel",
	})

	// 2. 创建 survey.Select 提示
	// survey.Select 是交互式单选组件，使用箭头键选择
	selectPrompt := &survey.Select{
		Message: localizer.MustLocalize(&i18n.LocalizeConfig{
			MessageID: "executeScriptQuestion",  // "你想要执行这个脚本吗？"
		}),
		Options: []string{confirmOption, retryOption, cancelOption},
		Default: confirmOption,  // 默认选中"要得"
	}

	// 3. 等待用户选择
	err := survey.AskOne(selectPrompt, &selectedOption)
	if err != nil {
		// 处理错误
		if ui.IsInterrupt(err) {
			// 用户按 Ctrl+C，优雅退出
			fmt.Println()
			fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
				MessageID: "goodbyeMessage",
			}))
			os.Exit(0)
		}
		// 其他错误，打印提示
		fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
			MessageID: "errorReadingInput",
			TemplateData: map[string]interface{}{
				"Error": err,
			},
		}))
		return ""
	}

	// 4. 打印空行，美化输出
	fmt.Println()

	// 5. 根据用户选择的文本返回对应的操作标识
	if selectedOption == confirmOption {
		return "confirm"
	}
	if selectedOption == retryOption {
		return "retry"
	}
	if selectedOption == cancelOption {
		return "cancel"
	}

	// 未知选项，返回空字符串
	return ""
}

// executeCommand 执行命令
// 清屏、打印提示、调用 executor.ExecuteCommand() 执行命令
//
// 参数：
//   - script: 要执行的单行命令字符串
//   - localizer: 本地化器，用于翻译提示文本
//
// 返回值：
//   - *executor.ExecutionResult: 执行结果（包含 stdout、stderr、退出码）
//   - error: 错误信息（如果执行失败）
func executeCommand(script string, localizer *i18n.Localizer) (*executor.ExecutionResult, error) {
	// 1. 清屏（如果终端支持）
	clearScreenIfSupported()

	// 2. 打印执行提示
	fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID: "executingCommand",  // "正在执行命令..."
	}))

	// 3. 打印要执行的命令
	fmt.Printf("Running: %s\n", script)

	// 4. 调用 executor.ExecuteCommand() 执行命令
	// executor.ExecuteCommand() 会：
	//   - 跨平台适配（Windows 用 cmd.exe，Unix 用 sh -c）
	//   - 实时输出 stdout/stderr 到终端
	//   - 捕获输出和退出码
	result, err := executor.ExecuteCommand(script)

	// 5. 返回执行结果和错误
	return result, err
}

// clearScreenIfSupported 如果终端支持，则清屏
// 使用 ANSI 转义序列清屏，类似于 Ctrl+L 的效果
//
// 清屏条件：
//   - TERM 环境变量不为空
//   - TERM 不等于 "dumb"（dumb 表示不支持 ANSI 转义的终端）
func clearScreenIfSupported() {
	// 检查 TERM 环境变量
	term := strings.TrimSpace(os.Getenv("TERM"))

	// 如果 TERM 为空或为 "dumb"，则不清屏
	if term == "" || strings.EqualFold(term, "dumb") {
		return
	}

	// ANSI 转义序列清屏
	// \033[H: 将光标移动到左上角
	// \033[2J: 清除整个屏幕
	fmt.Print("\033[H\033[2J")
}

// printSuccessCelebration 打印成功庆祝横幅
// 命令执行成功后，打印一个格式化的横幅，展示原始提示词和最终执行的命令
//
// 参数：
//   - localizer: 本地化器
//   - initialPrompt: 用户的原始输入
//   - finalScript: 最终执行的命令
func printSuccessCelebration(localizer *i18n.Localizer, initialPrompt string, finalScript string) {
	// 1. 获取翻译文本
	title := localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID: "celebrationTitle",  // "任务完成"
	})
	promptLabel := localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID: "celebrationInitialPromptLabel",  // "初始提示词："
	})
	scriptLabel := localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID: "celebrationFinalScriptLabel",  // "最终脚本："
	})

	// 2. 构建横幅（使用等号作为边框）
	line := strings.Repeat("=", 72)
	fmt.Println()
	fmt.Println(line)
	fmt.Printf("= %s\n", title)
	fmt.Printf("= %s %s\n", promptLabel, strings.TrimSpace(initialPrompt))
	fmt.Printf("= %s %s\n", scriptLabel, strings.TrimSpace(finalScript))
	fmt.Println(line)
	fmt.Println()
}

// buildExecutionFeedback 构建执行失败反馈信息
// 当命令执行失败时，构建详细的错误反馈，追加到下一次生成的 prompt 中
//
// 反馈信息包含：
//   - 失败的命令
//   - 执行错误
//   - 退出码（如果非 0）
//   - 捕获的 stdout（如果存在）
//   - 捕获的 stderr（如果存在）
//
// 参数：
//   - script: 失败的命令
//   - result: 执行结果（包含 stdout、stderr、退出码）
//   - err: 错误信息
//
// 返回值：
//   - string: 构建的反馈文本（用于追加到下一次生成的 prompt）
func buildExecutionFeedback(script string, result *executor.ExecutionResult, err error) string {
	var builder strings.Builder

	// 1. 添加提示语（告诉 AI 命令失败了）
	builder.WriteString("\n\nThe previous generated command failed. Analyze the failure and generate a corrected replacement command.\n")

	// 2. 添加失败的命令
	builder.WriteString("Failed command:\n")
	builder.WriteString(script)
	builder.WriteString("\n")

	// 3. 添加执行错误
	builder.WriteString("Execution error:\n")
	builder.WriteString(err.Error())
	builder.WriteString("\n")

	// 4. 如果 result 不为空，添加退出码和输出
	if result != nil {
		// 4.1 添加退出码（如果非 0）
		if result.ExitCode != 0 {
			builder.WriteString("Exit code:\n")
			builder.WriteString(strconv.Itoa(result.ExitCode))
			builder.WriteString("\n")
		}

		// 4.2 添加 stdout（如果存在）
		stdout := trimExecutionOutput(result.Stdout)
		if stdout != "" {
			builder.WriteString("Captured stdout:\n")
			builder.WriteString(stdout)
			builder.WriteString("\n")
		}

		// 4.3 添加 stderr（如果存在）
		stderr := trimExecutionOutput(result.Stderr)
		if stderr != "" {
			builder.WriteString("Captured stderr:\n")
			builder.WriteString(stderr)
			builder.WriteString("\n")
		}
	}

	// 5. 添加指令，要求 AI 只返回单行命令，不要解释
	builder.WriteString("Return a new single-line command only. Do not explain it.\n")

	// 6. 返回构建的反馈文本
	return builder.String()
}

// promptFromHistory 从历史记录中查询并让用户选择
// 支持关键词搜索和查看全部历史记录
//
// 执行流程：
// 1. 调用 historyStore.Search(query) 搜索匹配的历史记录
// 2. 如果没有匹配，提示用户并返回 shouldExit=true
// 3. 如果有匹配，使用 survey.Select 展示选项列表
// 4. 等待用户选择（或选择"都不是"）
//
// 参数：
//   - query: 搜索关键词（空字符串表示查看全部）
//   - historyStore: 历史记录存储
//   - localizer: 本地化器
//
// 返回值：
//   - string: 用户选择的 prompt（或空字符串）
//   - bool: selectedFromHistory，true 表示从历史记录中选择了
//   - bool: shouldExit，true 表示应该退出历史查询
func promptFromHistory(query string, historyStore *history.Store, localizer *i18n.Localizer) (string, bool, bool) {
	// 1. 搜索历史记录
	// historyStore.Search() 会返回匹配的历史记录列表
	entries := historyStore.Search(query)

	// 2. 如果没有匹配项
	if len(entries) == 0 {
		fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
			MessageID: "historyNoMatches",
			TemplateData: map[string]interface{}{
				"Query": query,
			},
		}))
		// 返回 shouldExit=true，让调用者继续等待用户输入
		return "", false, true
	}

	// 3. 获取"都不是"选项的翻译文本
	noneOption := localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID: "historyNoneOption",
	})

	// 4. 构建选项列表
	options := make([]string, 0, len(entries)+1)
	selectedPromptByOption := make(map[string]string, len(entries))
	for _, entry := range entries {
		// 格式化历史记录选项（如："列出文件 => ls -la"）
		option := formatHistoryOption(entry)
		options = append(options, option)
		// 建立选项文本到原始 prompt 的映射
		selectedPromptByOption[option] = entry.Prompt
	}
	// 添加"都不是"选项
	options = append(options, noneOption)

	// 5. 展示选项列表并等待用户选择
	selectedOption, err := ui.SelectOption(localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID: "historySelectPrompt",  // "请选择一条历史记录："
	}), options)
	if err != nil {
		// 处理错误
		if ui.IsInterrupt(err) {
			// 用户按 Ctrl+C，优雅退出
			fmt.Println()
			fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
				MessageID: "goodbyeMessage",
			}))
			os.Exit(0)
		}
		// 其他错误，打印提示
		fmt.Println(localizer.MustLocalize(&i18n.LocalizeConfig{
			MessageID: "errorReadingInput",
			TemplateData: map[string]interface{}{
				"Error": err,
			},
		}))
		return "", false, false
	}

	// 6. 用户选择"都不是"
	if selectedOption == noneOption {
		return "", false, false
	}

	// 7. 返回用户选择的原始 prompt
	return selectedPromptByOption[selectedOption], true, false
}

// normalizeArgs 规范化命令行参数
// 将组合参数（如 -hs、-ps）展开为标准格式，便于 flag.Parse() 解析
//
// 支持的组合参数：
//   - -hs 或 -sh: 展开为 -s -h（或 -h= 如果后面没有参数）
//   - -ps 或 -sp: 展开为 -s -p
//
// 参数：
//   - args: 原始参数列表（os.Args[1:]）
//
// 返回值：
//   - []string: 规范化后的参数列表
//   - bool: historyFlagUsed，是否使用了 -h 参数
func normalizeArgs(args []string) ([]string, bool) {
	normalized := make([]string, 0, len(args)+2)
	historyFlagUsed := false

	// 遍历所有参数
	for index := 0; index < len(args); index++ {
		arg := args[index]

		switch arg {
		case "-hs", "-sh":
			// 组合参数 -hs 或 -sh
			// 展开：-s（静默模式）+ -h（历史查询）
			normalized = append(normalized, "-s")
			historyFlagUsed = true
			// 判断 -h 后面是否有参数
			if index == len(args)-1 || strings.HasPrefix(args[index+1], "-") {
				// -h 后面没有参数或后面是另一个标志
				// 使用 -h= 格式（空值）
				normalized = append(normalized, "-h=")
			} else {
				// -h 后面有参数，使用 -h 格式
				normalized = append(normalized, "-h")
			}
			continue

		case "-ps", "-sp":
			// 组合参数 -ps 或 -sp
			// 展开：-s（静默模式）+ -p（提示词）
			// 注意：-s 必须在 -p 之前，否则 -s 会被当成 -p 的值
			normalized = append(normalized, "-s", "-p")
			continue

		case "-h":
			// 单独的 -h 参数
			historyFlagUsed = true
			// 判断 -h 后面是否有参数
			if index == len(args)-1 || strings.HasPrefix(args[index+1], "-") {
				// -h 后面没有参数或后面是另一个标志
				// 使用 -h= 格式（空值）
				normalized = append(normalized, "-h=")
				continue
			}
			// -h 后面有参数，保持原样（-h 会在下一个循环中处理）
		}

		// 检查是否是 -h=value 格式
		if strings.HasPrefix(arg, "-h=") {
			historyFlagUsed = true
		}

		// 其他参数，保持原样
		normalized = append(normalized, arg)
	}

	return normalized, historyFlagUsed
}

// formatHistoryOption 格式化历史记录选项
// 将历史记录格式化为简洁的文本，用于 survey.Select 展示
//
// 格式："{prompt} => {script}"
// 示例："列出当前目录文件 => ls -la"
//
// 参数：
//   - entry: 历史记录条目（包含 prompt、script、tokens 等）
//
// 返回值：
//   - string: 格式化后的选项文本
func formatHistoryOption(entry history.Entry) string {
	// 1. 截断 prompt（最多 48 个字符）
	prompt := truncateForOption(entry.Prompt, 48)
	// 2. 截断 script（最多 36 个字符）
	script := truncateForOption(entry.Script, 36)

	// 3. 如果 script 为空，只返回 prompt
	if script == "" {
		return prompt
	}

	// 4. 返回格式化文本："{prompt} => {script}"
	return fmt.Sprintf("%s => %s", prompt, script)
}

// truncateForOption 截断字符串以适应选项显示
// 确保选项文本不会过长，影响 survey.Select 的显示效果
//
// 处理逻辑：
//   1. 移除多余空格（将连续空格压缩为单个空格）
//   2. 如果长度 <= maxLen，直接返回
//   3. 如果长度 > maxLen，截断并添加 "..."
//
// 参数：
//   - value: 原始字符串
//   - maxLen: 最大长度
//
// 返回值：
//   - string: 截断后的字符串
func truncateForOption(value string, maxLen int) string {
	// 1. 移除多余空格（将连续空格压缩为单个空格）
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")

	// 2. 如果长度不超过限制，直接返回
	if len(value) <= maxLen {
		return value
	}

	// 3. 如果 maxLen <= 3，直接截断（无法容纳 "..."）
	if maxLen <= 3 {
		return value[:maxLen]
	}

	// 4. 截断并添加 "..."（保留前 maxLen-3 个字符）
	return value[:maxLen-3] + "..."
}

// trimExecutionOutput 截断执行输出
// 避免过长的 stdout/stderr 占用过多 token 或影响 prompt 质量
//
// 截断策略：
//   - 如果输出长度 <= 4000，直接返回
//   - 如果输出长度 > 4000，保留前 2000 字符 + "\n...\n" + 后 2000 字符
//
// 参数：
//   - output: 原始输出文本
//
// 返回值：
//   - string: 截断后的输出文本
func trimExecutionOutput(output string) string {
	// 最大输出长度限制
	const maxOutputLength = 4000

	// 1. 去除首尾空格
	output = strings.TrimSpace(output)

	// 2. 如果长度不超过限制，直接返回
	if len(output) <= maxOutputLength {
		return output
	}

	// 3. 截断输出（保留前后各一半）
	const retainedLength = maxOutputLength / 2  // 2000

	// 返回：前 2000 字符 + "\n...\n" + 后 2000 字符
	return output[:retainedLength] + "\n...\n" + output[len(output)-retainedLength:]
}
