package constant

// 消息来源/发送目标
type MessageOrigin uint8

const (
	GroupMessage MessageOrigin = iota
	PrivateMessage
)

// 发送消息类型

type MessageType uint8

const (
	PlainText MessageType = 0
	Markdown  MessageType = 2
	Media     MessageType = 7
)

// 富媒体文件类型
type FileType int

const (
	FileTypeImage FileType = 1 // 图片 png/jpg
	FileTypeVideo FileType = 2 // 视频 mp4
	FileTypeVoice FileType = 3 // 语音 silk/wav/mp3/flac
	FileTypeFile  FileType = 4 // 普通文件
)
