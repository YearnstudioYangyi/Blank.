# Plrx 北极星 — QQ 官方机器人 + AI Agent

> 轻量的 QQ 官方机器人框架，自带 AI 插件（流式对话 + Docker 沙箱工具调用）。

## 仓库结构

```
QBotOfficalFramework/
├── main.go                   # Gin webhook 入口
├── go.mod / go.sum
├── config.json               # QQ 机器人配置（运行时挂载）
├── ai.yaml.example           # AI 插件配置示例（复制为 ai.yaml）
├── Dockerfile                # Linux 镜像（多阶段，distroless 运行时）
├── .dockerignore
├── deploy/
│   └── docker-compose.yml    # 一键起容器
├── lib/                      # 框架核心（context / plugin / qqapi ...）
├── plugins/
│   ├── register.go           # 匿名导入所有插件
│   ├── echo/                 # 示例：回声洞、随机图
│   ├── bind/                 # 示例：UID 绑定流程
│   └── ai/                   # AI 插件（/ai 指令）
│       ├── plugin.go         # 会话管理 + agent 主循环
│       ├── yaml.go           # yaml 解析封装
│       └── internal/
│           ├── streamer/     # OpenAI 流式拉取
│           ├── response/     # XML 工具调用解析
│           └── docker_agent/ # Docker 沙箱执行器
└── templates/markdown/       # 业务 markdown 模板
```

## 快速开始（Linux / Docker）

### 1. 准备配置

```bash
git clone <repo> Plrx
cd Plrx/deploy

# 写 QQ 配置
cat > config.json <<'EOF'
{
  "port": 37654,
  "appid": "your_appid",
  "secret": "your_secret",
  "proxy": "https://api.sgroup.qq.com"
}
EOF

# 写 AI 配置
cp ../ai.yaml.example ai.yaml
vim ai.yaml   # 填 api_key / base_url / model_id
```

### 2. 起容器

```bash
docker compose up -d --build
docker compose logs -f plrx
```

容器会做下面这些事：

- 编译当前目录并生成 `plrx:linux` 镜像
- 启动 distroless 二进制，监听 `:37654`
- 把宿主 `/var/run/docker.sock` 挂进去（AI 沙箱需要它去起 `agent-runner` 容器）
- 读 `/app/config.json` 和 `/app/ai.yaml`

### 3. 在 QQ 开放平台配置回调

```
公网地址:37654/webhook
```

事件订阅建议至少勾选：

- 群 @ 消息 / 群消息
- 按钮交互事件

### 4. 使用 AI 插件

- `/ai <问题>`：发起一段多轮 agent 会话，AI 会按需调用 shell 工具
- `/ai reset`：清空当前会话

会话按群/私聊维度保存历史，不会串台。

## 本地交叉编译（不通过 Docker）

```bash
# 在 Windows / macOS 上
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o Plrx .
scp Plrx user@server:/opt/plrx/
ssh user@server
```

## 沙箱要求

- 宿主机装有 Docker（`docker info` 能通）
- AI 沙箱镜像已构建：
  ```bash
  cd AIBot
  docker build -t agent-runner .
  ```
- 沙箱容器默认名 `ai-bot-agent-runner`，复用同名容器不会重复创建

## 框架 API 速览

### 写一个插件

在 `plugins/<name>/` 下放任意 go 文件，根目录 `init()` 即可：

```go
package myplugin

import (
    "Plrx/lib/constant"
    "Plrx/lib/plugin"
    "Plrx/lib/context"
)

func init() {
    plugin.Register(&plugin.Plugin{
        Id: "myplugin",
        Commands: []*plugin.Command{
            {
                Prefix:   "/hi",
                Role:     constant.RoleMember,
                Describe: "打个招呼",
                Handle:   hi,
            },
        },
    })
}

func hi(ctx *context.MessageContext) error {
    return ctx.Text("hi").Send()
}
```

最后在 `plugins/register.go` 匿名导入：

```go
import _ "Plrx/plugins/myplugin"
```

### 发送消息

- `ctx.Text("...").Send()` — 纯文本
- `ctx.Markdown("...").Send()` — Markdown
- 群/私聊目标由 `ctx.GroupId` / `ctx.UserId` 决定，无需手动选

### 解析参数

- 默认 `DefaultParser` 把整条消息原样塞进 `ctx.Content`
- `PositionalParser` + `reflect.Type` 可按位置解析到结构体

## 已知约束

- 不支持频道（Channel）相关事件
- 沙箱为单进程共享（所有用户复用同一个 `agent-runner` 容器），多租户场景请自建隔离层
- AI 流式响应在 QBot 端不是真正 SSE，是按 `<say>` 拆条推送，模型必须严格遵守 XML 标签

## 许可证

MIT
