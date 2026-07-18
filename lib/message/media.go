package message

import "encoding/json"

// 富媒体资源信息
type Media struct {
	FileInfo string `json:"file_info"`
}

// 富媒体消息
type MediaMessage struct {
	*Message
	Content string `json:"content,omitempty"`
	Media   Media  `json:"media"`
}

// 设置附加文本
func (msg *MediaMessage) SetContent(content string) *MediaMessage {
	msg.Content = content
	return msg
}

// 设置 file_info
func (msg *MediaMessage) SetFileInfo(fileInfo string) *MediaMessage {
	msg.Media.FileInfo = fileInfo
	return msg
}

// 实现 CanMarshal
func (msg *MediaMessage) Marshal() ([]byte, error) {
	return json.Marshal(msg)
}

// 初始化 Message 结构体
func (msg *MediaMessage) Init() {
	var metamsg *Message
	if msg.Message == nil {
		metamsg = &Message{}
		metamsg.InitRef()
	} else {
		metamsg = msg.Message
	}
	metamsg.MarshalInterface = msg
	msg.Message = metamsg
}

func NewMediaMessage() *MediaMessage {
	msg := &MediaMessage{}
	msg.Init()
	return msg
}
