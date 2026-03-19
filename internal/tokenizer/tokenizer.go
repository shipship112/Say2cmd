package tokenizer

import (
	"strings"
	"sync"
	"unicode"

	"github.com/go-ego/gse"
)

var (
	segmenter     gse.Segmenter       // 中文分词器（gse 库）
	segmenterOnce sync.Once           // 确保分词器只初始化一次
)

// TokenizeForSearch 生成用于检索的 token 集合（去重后返回）。
// 处理策略：
// - 中文：gse 分词 + 2-gram + 3-gram（完整短语）
// - 英文：完整单词保留
// - 长度过滤：token 长度必须 ≥ 2
// - 去重：使用 map 去重，返回无序切片
func TokenizeForSearch(text string) []string {
	// 1. 预处理：转小写、去空格
	text = strings.TrimSpace(strings.ToLower(text))
	if text == "" {
		return nil
	}

	// 2. 使用 map 去重（key: token, value: 空结构体）
	tokenSet := make(map[string]struct{})
	
	// 3. 添加 token 的辅助函数（长度过滤、去重、小写）
	add := func(token string) {
		token = strings.TrimSpace(strings.ToLower(token))
		if token == "" {
			return
		}

		// 过滤长度 < 2 的 token
		runes := []rune(token)
		if len(runes) < 2 {
			return
		}

		// 添加到集合（自动去重）
		tokenSet[token] = struct{}{}
	}

	// 4. 分离中英文处理
	var latinBuilder strings.Builder  // 拼接英文字符（单词/数字/下划线/连字符）
	hanRunes := make([]rune, 0, len(text))  // 存储中文字符

	// 5. 刷新并处理拉丁字符（英文）的函数
	flushLatin := func() {
		if latinBuilder.Len() == 0 {
			return
		}
		// 将完整单词作为一个 token
		add(latinBuilder.String())
		latinBuilder.Reset()
	}

	// 6. 刷新并处理中文字符的函数
	flushHan := func() {
		if len(hanRunes) == 0 {
			return
		}

		chunk := string(hanRunes)
		// 6.1 使用 gse 分词器进行中文分词
		for _, token := range cutChinese(chunk) {
			add(token)
		}

		// 6.2 添加完整短语（如："列出文件" → ["列出文件"]）
		add(string(hanRunes))
		
		// 6.3 生成 2-gram（如："列出" → ["列", "出", "列出"]）
		for i := 0; i < len(hanRunes)-1; i++ {
			add(string(hanRunes[i : i+2]))
		}
		
		// 6.4 生成 3-gram（如："列出文件" → ["列", "出", "文", "列出", "出文", "列出文件"]）
		for i := 0; i < len(hanRunes)-2; i++ {
			add(string(hanRunes[i : i+3]))
		}

		hanRunes = hanRunes[:0]  // 清空中文缓冲区
	}

	// 7. 遍历输入文本，分离中英文
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):  // 判断是否为汉字
			flushLatin()  // 遇到中文，先刷新英文缓冲区
			hanRunes = append(hanRunes, r)  // 收集中文字符
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':  // 英文字母、数字、-、_
			flushHan()  // 遇到英文，先刷新中文缓冲区
			latinBuilder.WriteRune(r)  // 拼接英文字符
		default:  // 其他字符（标点、空格等）
			flushLatin()  // 刷新两个缓冲区
			flushHan()
		}
	}

	// 8. 循环结束后，刷新剩余的缓冲区内容
	flushLatin()
	flushHan()

	// 9. 如果没有任何 token，返回 nil
	if len(tokenSet) == 0 {
		return nil
	}

	// 10. 将 map 转换为切片返回（无序）
	tokens := make([]string, 0, len(tokenSet))
	for token := range tokenSet {
		tokens = append(tokens, token)
	}

	return tokens
}

// cutChinese 使用 gse 分词器对中文文本进行分词
// 参数：
//   - text: 待分词的中文文本
// 返回：
//   - []string: 分词后的 token 列表
// 特性：
//   - 使用 sync.Once 确保分词器字典只加载一次（懒加载）
//   - 使用 defer recover 捕获分词器初始化可能引发的 panic
//   - CutSearch 是 gse 提供的搜索模式分词（比 CutText 更适合检索）
func cutChinese(text string) []string {
	// 1. 空字符串检查
	if strings.TrimSpace(text) == "" {
		return nil
	}

	// 2. 异常恢复：防止 gse 初始化失败导致程序崩溃
	defer func() {
		_ = recover()
	}()

	// 3. 懒加载分词器字典（只加载一次）
	// sync.Once 保证 segmenter.LoadDict() 在整个程序运行期间只执行一次
	segmenterOnce.Do(func() {
		segmenter.LoadDict()  // 加载 gse 内置的中文词典
	})

	// 4. 调用 gse 分词器进行搜索模式分词
	// CutSearch 返回适合检索的 token 列表
	// 第二个参数 true 表示使用搜索模式（会返回更细粒度的分词）
	return segmenter.CutSearch(text, true)
}
