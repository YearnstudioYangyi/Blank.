package context

import (
	"Plrx/lib/constant"
	"Plrx/lib/message"
	"Plrx/lib/qqapi"
	"encoding/base64"
	"fmt"
	"net/url"
	"path"
	"strings"
)

// 富媒体文件管理器：上传 -> 发送
type FileManager struct {
	manager *MessageManager
}

func newFileManager(manager *MessageManager) *FileManager {
	return &FileManager{manager: manager}
}

// 上传结果
type FileUploadResult = qqapi.FileUploadResult

// ---------- 上传 ----------

// 通过 URL 上传群聊富媒体
func (fm *FileManager) UploadGroupByURL(fileType constant.FileType, fileURL string) (*FileUploadResult, error) {
	return fm.UploadGroupByURLNamed(fileType, fileURL, fileNameFromURL(fileURL))
}

// 通过 URL 上传群聊富媒体（指定文件名）
func (fm *FileManager) UploadGroupByURLNamed(fileType constant.FileType, fileURL string, fileName string) (*FileUploadResult, error) {
	if err := fm.requireGroup(); err != nil {
		return nil, err
	}
	return fm.manager.Qapi.UploadGroupFile(fm.manager.GroupId, int(fileType), fileURL, "", fileName)
}

// 通过二进制数据上传群聊富媒体
func (fm *FileManager) UploadGroupByData(fileType constant.FileType, data []byte) (*FileUploadResult, error) {
	return fm.UploadGroupByDataNamed(fileType, data, "")
}

// 通过二进制数据上传群聊富媒体（指定文件名）
func (fm *FileManager) UploadGroupByDataNamed(fileType constant.FileType, data []byte, fileName string) (*FileUploadResult, error) {
	if err := fm.requireGroup(); err != nil {
		return nil, err
	}
	return fm.manager.Qapi.UploadGroupFile(fm.manager.GroupId, int(fileType), "", base64.StdEncoding.EncodeToString(data), fileName)
}

// 通过 URL 上传私聊富媒体
func (fm *FileManager) UploadPrivateByURL(fileType constant.FileType, fileURL string) (*FileUploadResult, error) {
	return fm.UploadPrivateByURLNamed(fileType, fileURL, fileNameFromURL(fileURL))
}

// 通过 URL 上传私聊富媒体（指定文件名）
func (fm *FileManager) UploadPrivateByURLNamed(fileType constant.FileType, fileURL string, fileName string) (*FileUploadResult, error) {
	if err := fm.requireUser(); err != nil {
		return nil, err
	}
	return fm.manager.Qapi.UploadPrivateFile(fm.manager.UserId, int(fileType), fileURL, "", fileName)
}

// 通过二进制数据上传私聊富媒体
func (fm *FileManager) UploadPrivateByData(fileType constant.FileType, data []byte) (*FileUploadResult, error) {
	return fm.UploadPrivateByDataNamed(fileType, data, "")
}

// 通过二进制数据上传私聊富媒体（指定文件名）
func (fm *FileManager) UploadPrivateByDataNamed(fileType constant.FileType, data []byte, fileName string) (*FileUploadResult, error) {
	if err := fm.requireUser(); err != nil {
		return nil, err
	}
	return fm.manager.Qapi.UploadPrivateFile(fm.manager.UserId, int(fileType), "", base64.StdEncoding.EncodeToString(data), fileName)
}

// ---------- 发送（复用 message.Send） ----------

// 使用已上传的 file_info 发送群聊富媒体消息
func (fm *FileManager) SendGroup(fileInfo string, content string) error {
	if err := fm.requireGroup(); err != nil {
		return err
	}
	msg := fm.manager.Media(fileInfo)
	msg.Target = constant.GroupMessage
	if content != "" {
		msg.SetContent(content)
	}
	return msg.Send()
}

// 使用已上传的 file_info 发送私聊富媒体消息
func (fm *FileManager) SendPrivate(fileInfo string, content string) error {
	if err := fm.requireUser(); err != nil {
		return err
	}
	msg := fm.manager.Media(fileInfo)
	msg.Target = constant.PrivateMessage
	if content != "" {
		msg.SetContent(content)
	}
	return msg.Send()
}

// ---------- 上传并发送 ----------

// 通过 URL 上传并发送到群聊
func (fm *FileManager) SendGroupByURL(fileType constant.FileType, fileURL string) error {
	result, err := fm.UploadGroupByURL(fileType, fileURL)
	if err != nil {
		return err
	}
	return fm.SendGroup(result.FileInfo, "")
}

// 通过二进制数据上传并发送到群聊
func (fm *FileManager) SendGroupByData(fileType constant.FileType, data []byte) error {
	return fm.SendGroupByDataNamed(fileType, data, "")
}

// 通过二进制数据上传并发送到群聊（指定文件名）
func (fm *FileManager) SendGroupByDataNamed(fileType constant.FileType, data []byte, fileName string) error {
	result, err := fm.UploadGroupByDataNamed(fileType, data, fileName)
	if err != nil {
		return err
	}
	return fm.SendGroup(result.FileInfo, "")
}

// 通过 URL 上传并发送到私聊
func (fm *FileManager) SendPrivateByURL(fileType constant.FileType, fileURL string) error {
	result, err := fm.UploadPrivateByURL(fileType, fileURL)
	if err != nil {
		return err
	}
	return fm.SendPrivate(result.FileInfo, "")
}

// 通过二进制数据上传并发送到私聊
func (fm *FileManager) SendPrivateByData(fileType constant.FileType, data []byte) error {
	return fm.SendPrivateByDataNamed(fileType, data, "")
}

// 通过二进制数据上传并发送到私聊（指定文件名）
func (fm *FileManager) SendPrivateByDataNamed(fileType constant.FileType, data []byte, fileName string) error {
	result, err := fm.UploadPrivateByDataNamed(fileType, data, fileName)
	if err != nil {
		return err
	}
	return fm.SendPrivate(result.FileInfo, "")
}

// 发送已读取的文件字节（Docker 等来源），自动按 GroupId/UserId 选择目标
func (fm *FileManager) SendFileBytes(fileType constant.FileType, data []byte, content string) error {
	return fm.SendFileBytesNamed(fileType, data, "", content)
}

// 发送已读取的文件字节并指定文件名
func (fm *FileManager) SendFileBytesNamed(fileType constant.FileType, data []byte, fileName string, content string) error {
	result, err := fm.UploadByDataNamed(fileType, data, fileName)
	if err != nil {
		return err
	}
	return fm.Send(result.FileInfo, content)
}

// ---------- 按上下文自动选择目标 ----------

// 上传（GroupId 优先，否则私聊）
func (fm *FileManager) UploadByURL(fileType constant.FileType, fileURL string) (*FileUploadResult, error) {
	if fm.manager.GroupId != "" {
		return fm.UploadGroupByURL(fileType, fileURL)
	}
	return fm.UploadPrivateByURL(fileType, fileURL)
}

// 上传二进制（GroupId 优先，否则私聊）
func (fm *FileManager) UploadByData(fileType constant.FileType, data []byte) (*FileUploadResult, error) {
	return fm.UploadByDataNamed(fileType, data, "")
}

// 上传二进制并指定文件名（GroupId 优先，否则私聊）
func (fm *FileManager) UploadByDataNamed(fileType constant.FileType, data []byte, fileName string) (*FileUploadResult, error) {
	if fm.manager.GroupId != "" {
		return fm.UploadGroupByDataNamed(fileType, data, fileName)
	}
	return fm.UploadPrivateByDataNamed(fileType, data, fileName)
}

// 发送已上传文件（GroupId 优先，否则私聊）
func (fm *FileManager) Send(fileInfo string, content string) error {
	if fm.manager.GroupId != "" {
		return fm.SendGroup(fileInfo, content)
	}
	return fm.SendPrivate(fileInfo, content)
}

// 上传并发送（GroupId 优先，否则私聊）
func (fm *FileManager) SendByURL(fileType constant.FileType, fileURL string) error {
	if fm.manager.GroupId != "" {
		return fm.SendGroupByURL(fileType, fileURL)
	}
	return fm.SendPrivateByURL(fileType, fileURL)
}

// 上传二进制并发送（GroupId 优先，否则私聊）
func (fm *FileManager) SendByData(fileType constant.FileType, data []byte) error {
	return fm.SendByDataNamed(fileType, data, "")
}

// 上传二进制并发送（指定文件名，GroupId 优先，否则私聊）
func (fm *FileManager) SendByDataNamed(fileType constant.FileType, data []byte, fileName string) error {
	if fm.manager.GroupId != "" {
		return fm.SendGroupByDataNamed(fileType, data, fileName)
	}
	return fm.SendPrivateByDataNamed(fileType, data, fileName)
}

func (fm *FileManager) requireGroup() error {
	if fm == nil || fm.manager == nil || fm.manager.Qapi == nil {
		return fmt.Errorf("FileManager 未初始化")
	}
	if fm.manager.GroupId == "" {
		return fmt.Errorf("GroupId 为空")
	}
	return nil
}

func (fm *FileManager) requireUser() error {
	if fm == nil || fm.manager == nil || fm.manager.Qapi == nil {
		return fmt.Errorf("FileManager 未初始化")
	}
	if fm.manager.UserId == "" {
		return fmt.Errorf("UserId 为空")
	}
	return nil
}

func fileNameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return ""
	}
	name := path.Base(u.Path)
	if name == "." || name == "/" {
		return ""
	}
	if q, err := url.PathUnescape(name); err == nil {
		name = q
	}
	return strings.TrimSpace(name)
}

// 生成富媒体消息（供 MessageManager 复用发送逻辑）
func (manager *MessageManager) Media(fileInfo string) *message.MediaMessage {
	metamsg := manager.baseStruct()
	msg := message.MediaMessage{
		Message: metamsg,
		Media: message.Media{
			FileInfo: fileInfo,
		},
	}
	msg.Type = constant.Media
	msg.Init()
	return &msg
}
