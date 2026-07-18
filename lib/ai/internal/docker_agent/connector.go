package docker_agent

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// ResourceConfig 定义容器的资源限制，包括内存和CPU
type ResourceConfig struct {
	MemoryBytes int64 // 内存限制，单位为字节
	NanoCPUs    int64 // CPU限制，单位为 10^-9 CPU
}

// FileInfo 表示容器内文件或目录的元数据
type FileInfo struct {
	Name  string // 文件名
	IsDir bool   // 是否为目录
	Size  int64  // 字节
}

// AgentExecutor 封装了与 Docker 交互的逻辑
type AgentExecutor struct {
	cli         *client.Client
	containerID string
}

// NewAgentExecutor 创建一个新的 AgentExecutor 实例，确保容器已创建并运行
func NewAgentExecutor(ctx context.Context, image, name string, res ResourceConfig) (*AgentExecutor, error) {
	cli, err := client.New(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	exec := &AgentExecutor{cli: cli}
	id, err := exec.ensureContainer(ctx, image, name, res)
	if err != nil {
		return nil, err
	}
	exec.containerID = id
	return exec, nil
}

// Execute 在容器中执行一条 shell 命令并返回 stdout 与 stderr 结果
func (a *AgentExecutor) Execute(ctx context.Context, cmd string) (string, string, error) {
	resp, err := a.cli.ExecCreate(ctx, a.containerID, client.ExecCreateOptions{
		Cmd:          []string{"/bin/bash", "-c", cmd},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return "", "", err
	}

	attach, err := a.cli.ExecAttach(ctx, resp.ID, client.ExecAttachOptions{})
	if err != nil {
		return "", "", err
	}
	defer attach.HijackedResponse.Close()

	var stdout, stderr strings.Builder
	err = demuxDockerStream(attach.HijackedResponse.Reader, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

// ReadFile 从容器中读取指定路径的文件内容
func (a *AgentExecutor) ReadFile(ctx context.Context, filePath string) ([]byte, error) {
	cleanPath := path.Clean(filePath)

	res, err := a.cli.CopyFromContainer(ctx, a.containerID, client.CopyFromContainerOptions{
		SourcePath: cleanPath,
	})
	if err != nil {
		return nil, err
	}
	defer res.Content.Close()

	tr := tar.NewReader(res.Content)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("file not found in archive from path: %s", cleanPath)
}

// WriteFile 将指定数据写入容器中的文件路径下，并设置文件权限
func (a *AgentExecutor) WriteFile(ctx context.Context, filePath string, data []byte, perm os.FileMode) error {
	cleanPath := path.Clean(filePath)
	dir := path.Dir(cleanPath)
	filename := path.Base(cleanPath)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name: filename,
		Mode: int64(perm),
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}

	_, err := a.cli.CopyToContainer(ctx, a.containerID, client.CopyToContainerOptions{
		DestinationPath: dir,
		Content:         &buf,
	})
	return err
}

// ListDir 列出容器中指定目录下的文件和子目录（非递归）
func (a *AgentExecutor) ListDir(ctx context.Context, dirPath string) ([]FileInfo, error) {
	cleanPath := path.Clean(dirPath)
	if cleanPath == "" {
		cleanPath = "."
	}

	resp, err := a.cli.ExecCreate(ctx, a.containerID, client.ExecCreateOptions{
		Cmd:          []string{"/bin/bash", "-c", `find "$1" -maxdepth 1 -mindepth 1 -exec stat -c "%f|%F|%s" {} +`, "--", cleanPath},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return nil, err
	}

	attach, err := a.cli.ExecAttach(ctx, resp.ID, client.ExecAttachOptions{})
	if err != nil {
		return nil, err
	}
	defer attach.HijackedResponse.Close()

	var stdout, stderr strings.Builder
	err = demuxDockerStream(attach.HijackedResponse.Reader, &stdout, &stderr)
	if err != nil {
		return nil, err
	}

	if stderr.Len() > 0 && stdout.Len() == 0 {
		return nil, fmt.Errorf("failed to list directory: %s", strings.TrimSpace(stderr.String()))
	}

	lines := strings.Split(stdout.String(), "\n")
	var fileInfos []FileInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}
		name := parts[0]
		isDir := parts[1] == "directory"
		size, _ := strconv.ParseInt(parts[2], 10, 64)

		fileInfos = append(fileInfos, FileInfo{
			Name:  name,
			IsDir: isDir,
			Size:  size,
		})
	}
	return fileInfos, nil
}

// Restart 重启沙箱容器
func (a *AgentExecutor) Restart(ctx context.Context) error {
	_, err := a.cli.ContainerRestart(ctx, a.containerID, client.ContainerRestartOptions{})
	return err
}

// Rebuild 使用新镜像与资源配置重新构建容器
func (a *AgentExecutor) Rebuild(ctx context.Context, image string, res ResourceConfig) error {
	inspect, err := a.cli.ContainerInspect(ctx, a.containerID, client.ContainerInspectOptions{})
	if err != nil {
		return err
	}
	name := strings.TrimPrefix(inspect.Container.Name, "/")

	_, err = a.cli.ContainerRemove(ctx, a.containerID, client.ContainerRemoveOptions{Force: true})
	if err != nil {
		return err
	}

	id, err := a.ensureContainer(ctx, image, name, res)
	if err != nil {
		return err
	}
	a.containerID = id
	return nil
}

func (a *AgentExecutor) ensureContainer(ctx context.Context, image, name string, res ResourceConfig) (string, error) {
	var existingContainerID string
	var foundExisting bool

	listResult, err := a.cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err == nil {
		for _, c := range listResult.Items {
			for _, n := range c.Names {
				if n == "/"+name {
					inspect, inspectErr := a.cli.ContainerInspect(ctx, c.ID, client.ContainerInspectOptions{})
					if inspectErr == nil {
						if len(inspect.Container.Config.Cmd) == 0 || inspect.Container.Config.Cmd[0] != "sleep" {
							_, _ = a.cli.ContainerRemove(ctx, c.ID, client.ContainerRemoveOptions{Force: true})
							break
						}
					}
					existingContainerID = c.ID
					foundExisting = true
					break
				}
			}
			if foundExisting || existingContainerID != "" {
				break
			}
		}
	}

	if foundExisting && existingContainerID != "" {
		_, err = a.cli.ContainerUpdate(ctx, existingContainerID, client.ContainerUpdateOptions{
			Resources: &container.Resources{Memory: res.MemoryBytes, NanoCPUs: res.NanoCPUs},
		})
		if err != nil {
			return "", fmt.Errorf("更新容器资源限制失败: %w", err)
		}

		inspect, err := a.cli.ContainerInspect(ctx, existingContainerID, client.ContainerInspectOptions{})
		if err == nil && !inspect.Container.State.Running {
			_, err = a.cli.ContainerStart(ctx, existingContainerID, client.ContainerStartOptions{})
			if err != nil {
				return "", fmt.Errorf("启动已有容器失败: %w", err)
			}
		}
		return existingContainerID, nil
	}

	resp, err := a.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{
			Image: image,
			Cmd:   []string{"sleep", "infinity"},
		},
		HostConfig: &container.HostConfig{
			Resources: container.Resources{
				Memory:   res.MemoryBytes,
				NanoCPUs: res.NanoCPUs,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("创建容器失败: %w", err)
	}

	_, err = a.cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	if err != nil {
		return "", fmt.Errorf("启动新创建的容器失败: %w", err)
	}
	return resp.ID, nil
}

func demuxDockerStream(src io.Reader, dstStdout, dstStderr io.Writer) error {
	header := make([]byte, 8)
	for {
		_, err := io.ReadFull(src, header)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		streamType := header[0]
		frameSize := binary.BigEndian.Uint32(header[4:8])
		var dst io.Writer
		switch streamType {
		case 1:
			dst = dstStdout
		case 2:
			dst = dstStderr
		default:
			dst = io.Discard
		}
		_, err = io.CopyN(dst, src, int64(frameSize))
		if err != nil {
			return err
		}
	}
}
