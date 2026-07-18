package response

import (
	"fmt"
	"strings"
)

type Tool struct {
	TagName string
	Value   string
}

var Result []Tool
var isRecordValue bool
var tagName string
var value strings.Builder
var assistantText strings.Builder

// GetAssistantMessage 接收一行文本并进行解析
func ProcessLine(line string) {
	assistantText.Write([]byte("\n" + line))
	rest := line
	for {
		if isRecordValue {
			// 尝试寻找闭合标签 </tagname>
			v, after, ok := strings.Cut(rest, fmt.Sprintf("</%s>", tagName))
			if !ok {
				// 如果没找到闭合标签，将当前剩余内容写入 Builder，等待下一行拼接
				value.WriteString("\n" + rest)
				return
			}

			// 找到闭合标签，写入剩余部分并保存
			value.WriteString("\n" + v)
			Result = append(Result, Tool{TagName: tagName, Value: value.String()})

			// 重置状态
			value.Reset()
			isRecordValue = false
			tagName = ""
			rest = after
			continue
		} else {
			// 寻找标签开始 <
			_, after, ok := strings.Cut(rest, "<")
			if !ok {
				return
			}
			// 寻找标签结束 >
			ty, v, ok := strings.Cut(after, ">")
			if !ok {
				return
			}

			rest = v
			tagName = ty
			isRecordValue = true
		}
	}
}

// GetResults 返回当前已解析出的所有结果
func GetResults() []Tool {
	return Result
}

// Reset 重置所有全局变量
func Reset() {
	Result = nil
	value.Reset()
	assistantText.Reset()
	isRecordValue = false
	tagName = ""
}

func GetAssistantMessage() string {
	return assistantText.String()
}
