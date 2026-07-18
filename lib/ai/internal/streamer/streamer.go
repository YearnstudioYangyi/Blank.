package streamer

import (
	"context"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// LineCallback 每收到一行完整文本时调用（不含换行符）。
type LineCallback func(line string)

// StreamConfig 流式请求所需的配置项。
type StreamConfig struct {
	APIKey  string
	BaseURL string
	ModelID string
}

// StreamChatCompletion 以流式方式请求 Chat Completion，并通过 onLine 回调输出每一行。
func StreamChatCompletion(ctx context.Context, cfg StreamConfig, messages []openai.ChatCompletionMessageParamUnion, onLine LineCallback) error {
	// 创建客户端，可自定义 BaseURL 和 APIKey
	client := openai.NewClient(
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL(cfg.BaseURL),
	)

	// 构建流式请求
	stream := client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(cfg.ModelID),
		Messages: messages,
	})

	// 累积 delta 内容，按行分割并回调
	var lineBuf strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta.Content
			if delta == "" {
				continue
			}
			// 追加到缓冲区
			lineBuf.WriteString(delta)

			// 从缓冲区中提取完整的行
			for {
				text := lineBuf.String()
				idx := strings.IndexAny(text, "\r\n")
				if idx == -1 {
					break
				}
				line := text[:idx]
				onLine(line)

				// 跳过连续换行符
				advance := idx + 1
				if text[idx] == '\r' && advance < len(text) && text[advance] == '\n' {
					advance++
				}
				lineBuf.Reset()
				if advance < len(text) {
					lineBuf.WriteString(text[advance:])
				}
			}
		}
	}

	// 处理可能发生的流错误
	if err := stream.Err(); err != nil {
		return err
	}

	// 流结束，若缓冲区还有残留内容，作为最后一行回调
	if lineBuf.Len() > 0 {
		onLine(lineBuf.String())
	}

	return nil
}
