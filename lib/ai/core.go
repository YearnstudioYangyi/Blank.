// Package ai 把 AIBot 的流式对话 + Docker 工具调用能力接入到 QBot 框架中。
//
// 通过 /ai 指令触发一段多轮 agent 会话：
//   - 用户消息作为新一轮 user 输入
//   - AI 流式输出：<say> 文本逐 token 推送给用户，<shell> 在沙箱容器里执行并把结果回灌给 AI
//   - 当 AI 输出 <finish> 时结束本轮会话
package ai

import (
	"Plrx/lib/buttons"
	"Plrx/lib/config"
	"Plrx/lib/constant"
	"Plrx/lib/context"
	qbotctx "Plrx/lib/context"
	"Plrx/lib/message"
	"Plrx/lib/searxng"
	stdctx "context"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"Plrx/lib/ai/internal/docker_agent"
	"Plrx/lib/ai/internal/response"
	"Plrx/lib/ai/internal/streamer"

	"github.com/openai/openai-go"
)

// AgentConfig 描述运行 AI 所需的全部配置，ai.yaml 中读取。
type AgentConfig struct {
	APIKey       string `yaml:"api_key"`
	BaseURL      string `yaml:"base_url"`
	ModelID      string `yaml:"model_id"`
	Image        string `yaml:"docker_image"`
	Container    string `yaml:"docker_container"`
	MemoryBytes  int64  `yaml:"docker_memory_bytes"`
	NanoCPUs     int64  `yaml:"docker_nano_cpus"`
	ConfigPath   string `yaml:"config_path"`
	SystemPrompt string `yaml:"system_prompt"`
}

var pendingTools []string = make([]string, 0)

// 默认配置（当 ai.yaml 缺失时退回到这套）。
var defaultConfig = AgentConfig{
	APIKey:      "",
	BaseURL:     "",
	ModelID:     "minimax-m3",
	Image:       "agent-runner",
	Container:   "ai-bot-agent-runner",
	MemoryBytes: 4 * 1024 * 1024 * 1024,
	NanoCPUs:    2 * 1e9,
	ConfigPath:  "./ai.yaml",
	SystemPrompt: `你现在是一个QQ的AI机器人, 可以帮助人类完成诸多工作

## 工具调用说明
所有的工具调用必须严格遵循 XML 格式。
**正确格式示例：**
<say>
你好，我正在处理你的请求
</say>

<shell>
ls -la
</shell>

<finish>
我已经完成任务
</finish>

## 工具返回说明
按照你调用工具的顺序返回。无返回值的工具不会出现在消息里。对于包含返回值的工具，返回值必须包裹在同样的 XML 标签中，且严格遵守上述换行规则。

## 可用工具
- 发送消息: say (参数: 要发送给用户的消息, 返回: 无)
- 执行bash指令: shell (参数: 要执行的命令, 返回: 执行结果)
- 发送沙箱文件: send_file (参数: 第一行文件类型 image|video|voice|file，第二行容器内绝对路径；也可只写一行路径并按扩展名推断类型。返回: 成功/失败说明)
   send_file工具示范:
   <send_file>
   image
   /tmp/out.png
   </send_file>
   或:
   <send_file>
   /tmp/out.png
   </send_file>
- 联网搜索: web_search (参数: 搜索关键词一行。返回: 标题/详情/链接列表，用于查事实、新闻、资料)
   web_search工具示范:
   <web_search>
   Go context 超时最佳实践
   </web_search>
- 完成当前任务: finish (参数: 给用户的完成任务的说明, 额外说明: 当一轮任务结束后, 必须且仅能调用一次此工具)
- 询问用户问题: ask (参数: 最多6行, 第一行为问题, 剩余为可选择的选项, 最多5个选项, 返回: 用户选择结果)
   ask工具示范:
   <ask>
   你今天如何
   还好
   不好
   不知道
   </ask>

## 工作流
1. 当人类下达指令后, 你需要返回完成工作。
2. 期间可以多次调用 say 工具暂时上报结果。
3. 需要把沙箱里的图片/视频/文件发给用户时，先用 shell 生成文件，再调用 send_file。
4. 当任务完成后, 调用 finish 工具告诉人类完成了任务。
注意, 如果不调用finish工具, 将会一直循环请求API
`,
}

var (
	loadedConfig AgentConfig
	loadOnce     sync.Once
	loadErr      error

	// 容器执行器在加载时启动一次，所有会话复用。
	executor *docker_agent.AgentExecutor
)

// 加载 ai.yaml，仅执行一次。
func loadAgentConfig() AgentConfig {
	loadOnce.Do(func() {
		loadedConfig = defaultConfig
		loadedConfig.ConfigPath = defaultConfig.ConfigPath

		data, err := os.ReadFile(defaultConfig.ConfigPath)
		if err != nil {
			if os.IsNotExist(err) {
				// 文件不存在时退回默认配置，但保留可用的 base_url/api_key/model_id 默认值
				loadErr = nil
				return
			}
			loadErr = err
			return
		}

		// 用 yaml 解码覆盖默认值，缺失字段保留默认。
		type rawCfg struct {
			APIKey          string `yaml:"api_key"`
			BaseURL         string `yaml:"base_url"`
			ModelID         string `yaml:"model_id"`
			DockerImage     string `yaml:"docker_image"`
			DockerContainer string `yaml:"docker_container"`
			DockerMemory    int64  `yaml:"docker_memory_bytes"`
			DockerNanoCPUs  int64  `yaml:"docker_nano_cpus"`
			SystemPrompt    string `yaml:"system_prompt"`
		}
		var raw rawCfg
		if err := yamlUnmarshal(data, &raw); err != nil {
			loadErr = err
			return
		}
		if raw.APIKey != "" {
			loadedConfig.APIKey = raw.APIKey
		}
		if raw.BaseURL != "" {
			loadedConfig.BaseURL = raw.BaseURL
		}
		if raw.ModelID != "" {
			loadedConfig.ModelID = raw.ModelID
		}
		if raw.DockerImage != "" {
			loadedConfig.Image = raw.DockerImage
		}
		if raw.DockerContainer != "" {
			loadedConfig.Container = raw.DockerContainer
		}
		if raw.DockerMemory > 0 {
			loadedConfig.MemoryBytes = raw.DockerMemory
		}
		if raw.DockerNanoCPUs > 0 {
			loadedConfig.NanoCPUs = raw.DockerNanoCPUs
		}
		if strings.TrimSpace(raw.SystemPrompt) != "" {
			loadedConfig.SystemPrompt = raw.SystemPrompt
		}
	})
	return loadedConfig
}

func init() {
	cfg := loadAgentConfig()

	// 预热沙箱容器
	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	defer cancel()
	exec, err := docker_agent.NewAgentExecutor(ctx, cfg.Image, cfg.Container, docker_agent.ResourceConfig{
		MemoryBytes: cfg.MemoryBytes,
		NanoCPUs:    cfg.NanoCPUs,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ai] 启动沙箱容器失败: %v\n", err)
	} else {
		executor = exec
	}

	// var commands []*plugin.Command = make([]*plugin.Command, 0)

	// commands = append(commands, &plugin.Command{
	// 	Prefix:   "/ai",
	// 	Role:     constant.RoleMember,
	// 	Describe: "与 AI 助手对话（可调用 shell 工具）",
	// 	Handle:   aiHandle,
	// })

	// // /ai reset 清空当前会话
	// commands = append(commands, &plugin.Command{
	// 	Prefix:   "/ai reset",
	// 	Role:     constant.RoleMember,
	// 	Describe: "重置当前会话",
	// 	Handle:   aiResetHandle,
	// })

	// plugin.Register(&plugin.Plugin{
	// 	Id:       "ai",
	// 	Commands: commands,
	// })
}

// 会话存储：以群/私聊维度为 key 保存历史消息。
var (
	sessionLock sync.Mutex
	sessions    = make(map[string][]openai.ChatCompletionMessageParamUnion)
)

func sessionKey(ctx *qbotctx.MessageContext) string {
	// 群聊用 groupOpenID，私聊用 userID，避免不同会话串台
	if ctx.GroupId != "" {
		return "g:" + ctx.GroupId
	}
	return "u:" + ctx.UserId
}

func getOrCreateSession(key string) []openai.ChatCompletionMessageParamUnion {
	sessionLock.Lock()
	defer sessionLock.Unlock()
	if s, ok := sessions[key]; ok {
		return s
	}
	cfg := loadAgentConfig()
	s := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(cfg.SystemPrompt),
	}
	sessions[key] = s
	return s
}

func resetSession(key string) {
	sessionLock.Lock()
	defer sessionLock.Unlock()
	delete(sessions, key)
}

// aiHandle 处理 /ai <question>。
func AiHandle(ctx *qbotctx.MessageContext) error {
	cfg := loadAgentConfig()

	if cfg.APIKey == "" || cfg.BaseURL == "" {
		return ctx.Text("AI 插件尚未配置 api_key / base_url，请检查 ai.yaml").Send()
	}

	// 取出用户问题：去掉前缀之后的所有内容
	question := ctx.Content
	// args := strings.SplitN(content, " ", 2)
	// var question string
	// if len(args) == 2 {
	// 	question = strings.TrimSpace(args[1])
	// }
	// if question == "" {
	// 	return ctx.Text("请输入内容").Send()
	// }
	if question == "重置" {
		return aiResetHandle(ctx)
	}

	key := sessionKey(ctx)
	messages := getOrCreateSession(key)
	messages = append(messages, openai.UserMessage(question))
	saveSession(key, messages)

	// 异步跑 agent 主循环
	go runAgentLoop(ctx, key, messages)
	return nil
}

// aiResetHandle 清空当前会话。
func aiResetHandle(ctx *qbotctx.MessageContext) error {
	resetSession(sessionKey(ctx))
	return ctx.Text("已重置当前会话").Send()
}

func saveSession(key string, messages []openai.ChatCompletionMessageParamUnion) {
	sessionLock.Lock()
	defer sessionLock.Unlock()
	sessions[key] = messages
}

func getSession(key string) []openai.ChatCompletionMessageParamUnion {
	sessionLock.Lock()
	defer sessionLock.Unlock()
	return sessions[key]
}

// runAgentLoop 持续跟 AI 交互，直到出现 <finish> 或达到最大轮数。
func runAgentLoop(ctx *qbotctx.MessageContext, key string, initialMessages []openai.ChatCompletionMessageParamUnion) {
	// 这里 messages 是值拷贝，loop 内部修改时需要回写
	messages := append([]openai.ChatCompletionMessageParamUnion{}, initialMessages...)

	cfg := loadAgentConfig()
	const maxRounds = 150
	stop := false
	pause := false
	for round := 0; round < maxRounds && !stop; round++ {
		// 重置流式解析的全局状态
		response.Reset()

		err := streamer.StreamChatCompletion(
			bgCtx(),
			streamer.StreamConfig{
				APIKey:  cfg.APIKey,
				BaseURL: cfg.BaseURL,
				ModelID: cfg.ModelID,
			},
			messages,
			response.ProcessLine,
		)
		if err != nil {
			ctx.Text(fmt.Sprintf("❌ AI 请求失败: %v", err)).Send()
			return
		}

		// 把整轮 AI 输出追加到历史
		assistantText := response.GetAssistantMessage()
		messages = append(messages, openai.AssistantMessage(assistantText))

		// 处理本轮解析出来的工具调用
		var (
			toolResults  strings.Builder
			finished     bool
			finishNotice string
		)
		toolResults.WriteString("[System Response]\n")
		for _, tag := range response.GetResults() {
			fmt.Printf("调用了%v工具:\n\n%v\n---\n", tag.TagName, tag.Value)
			pendingTools = append(
				pendingTools,
				fmt.Sprintf("%s: %s", tag.TagName, strings.Split(tag.Value, " ")[0]),
			)
			if len(pendingTools) > 10 {
				var result strings.Builder
				result.Write([]byte("已调用工具\n```bash"))
				for _, v := range pendingTools {
					result.Write(fmt.Appendf(nil, "\n- bash %v", v))
				}
				result.Write([]byte("\n```"))
				ctx.Markdown(result.String()).Send()
				pendingTools = make([]string, 0)
			}
			switch tag.TagName {
			case "say":
				if len(pendingTools) > 0 {
					var result strings.Builder
					result.Write([]byte("已调用工具\n```bash"))
					for _, v := range pendingTools {
						result.Write(fmt.Appendf(nil, "\n- bash %v", v))
					}
					result.Write([]byte("\n```"))
					ctx.Markdown(result.String()).Send()
					pendingTools = make([]string, 0)
				}
				err := ctx.Markdown(tag.Value).Send()
				if err != nil {
					fmt.Printf("发送QQ消息时出错: %v", err)
				}
			case "shell":
				if executor == nil {
					toolResults.WriteString("\n<shell>\n沙箱容器未启动，无法执行命令\n</shell>\n")
					continue
				}
				std, ser, execErr := executor.Execute(bgCtx(), tag.Value)
				toolResults.WriteString("\n<shell>\n")
				if execErr != nil {
					fmt.Fprintf(&toolResults, "Failed when run bash: %v\n", execErr)
				} else if len(std) > 0 && len(ser) == 0 {
					toolResults.WriteString(std)
				} else if len(std) == 0 && len(ser) > 0 {
					toolResults.WriteString(ser)
				} else if len(std) == 0 && len(ser) == 0 {
					toolResults.WriteString("(no output)")
				} else {
					fmt.Fprintf(&toolResults, "--Std output--\n%v\n--std error--\n%v", std, ser)
				}
				toolResults.WriteString("\n</shell>\n")
			case "send_file":
				toolResults.WriteString("\n<send_file>\n")
				if err := sendFileFromDocker(ctx, tag.Value); err != nil {
					fmt.Fprintf(&toolResults, "failed: %v", err)
				} else {
					toolResults.WriteString("ok")
				}
				toolResults.WriteString("\n</send_file>\n")
			case "web_search":
				toolResults.WriteString("\n<web_search>\n")
				toolResults.WriteString(runWebSearch(strings.TrimSpace(tag.Value)))
				toolResults.WriteString("\n</web_search>\n")
			case "finish":
				finished = true
				finishNotice = strings.TrimSpace(tag.Value)
			case "ask":
				choices := strings.Split(tag.Value, "\n")
				finalChoices := []string{}
				for k := range choices {
					if strings.TrimSpace(choices[k]) != "" {
						finalChoices = append(finalChoices, choices[k])
					}
				}
				choices = finalChoices
				if len(choices) < 3 {
					toolResults.WriteString("\n<ask>\n参数少于三行, 至少需要一个问题及两个选项\n</ask>\n")
					break
				}
				if len(choices) > 6 {
					toolResults.WriteString("\n<ask>\n参数多于六行, 最多支持一个问题及五个选项\n</ask>\n")
					break
				}
				var md strings.Builder
				fmt.Fprintf(&md, "## Agent向你提问: \n%v", choices[0])
				keyboard := &buttons.Keyboard{}
				for i := 1; i < len(choices); i++ {
					fmt.Fprintf(&md, "\n%v. %v", i, choices[i])
					btn, err := keyboard.AppendButton("ask-callback", fmt.Sprintf("选项%v", i), "已选择", buttons.Blue, i-1)
					if err != nil {
						fmt.Printf("添加按钮出错: %v", err)
						break
					}
					btn.SetCallback(fmt.Sprintf("%v", i), func(cc *qbotctx.CallbackContext) error {
						content := fmt.Sprintf("[System Response]\n<ask>用户选择了%v\n", cc.Data)
						ctx := context.MessageContext{
							Context: cc.Context,
							Raw:     content,
							UserMessage: message.UserMessage{
								Content: content,
							},
						}
						return AiHandle(&ctx)
					}).SetPermission(buttons.AllUser).SetUnsupportedTip("不支持按钮")
				}
				msg := ctx.Markdown(md.String())
				msg.Keyboard(keyboard)
				msg.Send()
				pause = true
			}
		}

		if finished {
			notice := ""
			if finishNotice != "" {
				notice = fmt.Sprintf("✅ 任务完成:\n\n %s", finishNotice)
			}
			if err := ctx.Markdown(notice).Send(); err != nil {
				_ = err
			}
			// 不再追加 toolResults，直接结束
			stop = true
			saveSession(key, messages)
			return
		}

		// 把工具结果追加到历史并继续下一轮
		messages = append(messages, openai.UserMessage(toolResults.String()))
		saveSession(key, messages)

		if pause {
			return
		}
	}

	if !stop {
		ctx.Text("⚠️ 已达到最大轮数，强制结束本轮会话。").Send()
		saveSession(key, messages)
	}
}

// bgCtx 返回一个不会随请求被取消的后台 context。
// 实际工程里建议包一层超时控制。
func bgCtx() stdctx.Context {
	return stdctx.Background()
}

// runWebSearch 调用自部署 SearXNG，配置来自 config.json 的 searxng 段。
func runWebSearch(query string) string {
	if strings.TrimSpace(query) == "" {
		return "错误: 搜索关键词为空"
	}
	cfg := config.Get()
	base := strings.TrimSpace(cfg.SearXNG.BaseURL)
	if base == "" {
		// 若 main 尚未 InitConfig，尝试加载一次
		cfg = config.InitConfig()
		base = strings.TrimSpace(cfg.SearXNG.BaseURL)
	}
	if base == "" {
		return "错误: 未配置 searxng.base_url（见 config.json）"
	}
	client := searxng.New(base)
	if cfg.SearXNG.Limit > 0 {
		client.Limit = cfg.SearXNG.Limit
	}
	if cfg.SearXNG.Language != "" {
		client.Language = cfg.SearXNG.Language
	}
	ctx, cancel := stdctx.WithTimeout(bgCtx(), 20*time.Second)
	defer cancel()
	results, err := client.Search(ctx, query)
	if err != nil {
		return fmt.Sprintf("搜索失败: %v", err)
	}
	return searxng.FormatForAgent(results)
}

// 从 Docker 沙箱读取文件并发送给当前会话用户/群
func sendFileFromDocker(ctx *qbotctx.MessageContext, raw string) error {
	if executor == nil {
		return fmt.Errorf("沙箱容器未启动，无法读取文件")
	}
	fileType, filePath, err := parseSendFileArgs(raw)
	if err != nil {
		return err
	}
	data, err := executor.ReadFile(bgCtx(), filePath)
	if err != nil {
		return fmt.Errorf("读取容器文件失败: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("文件为空: %s", filePath)
	}
	fileName := path.Base(filePath)
	if fileName == "." || fileName == "/" {
		fileName = "file"
	}
	if err := ctx.SendFileBytesNamed(fileType, data, fileName, ""); err != nil {
		return fmt.Errorf("发送文件失败: %w", err)
	}
	return nil
}

func parseSendFileArgs(raw string) (constant.FileType, string, error) {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	switch len(lines) {
	case 1:
		return guessFileType(lines[0]), lines[0], nil
	case 2:
		ft, err := parseFileType(lines[0])
		if err != nil {
			return 0, "", err
		}
		return ft, lines[1], nil
	default:
		return 0, "", fmt.Errorf("参数格式错误: 需要 1 行路径，或 2 行(类型+路径)")
	}
}

func parseFileType(s string) (constant.FileType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "image", "img", "1":
		return constant.FileTypeImage, nil
	case "video", "2":
		return constant.FileTypeVideo, nil
	case "voice", "audio", "3":
		return constant.FileTypeVoice, nil
	case "file", "4":
		return constant.FileTypeFile, nil
	default:
		return 0, fmt.Errorf("未知文件类型: %s (可用 image|video|voice|file)", s)
	}
}

func guessFileType(filePath string) constant.FileType {
	ext := strings.ToLower(path.Ext(filePath))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp":
		return constant.FileTypeImage
	case ".mp4", ".mov", ".avi", ".mkv", ".webm":
		return constant.FileTypeVideo
	case ".silk", ".wav", ".mp3", ".flac", ".ogg", ".m4a":
		return constant.FileTypeVoice
	default:
		return constant.FileTypeFile
	}
}
