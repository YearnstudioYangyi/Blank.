# 多阶段构建：Linux 版 Plrx 机器人
# 1) builder 拉 Go 工具链 → 编译
# 2) runtime  用 distroless 静态镜像，几乎 0 攻击面

FROM golang:1.25.4 AS builder
WORKDIR /src

# 单独下载依赖，利用 Docker layer 缓存
COPY go.mod go.sum ./
RUN go mod download

# 拷贝源码
COPY . .

# 编译成静态二进制，CGO 关闭以适配 distroless
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ENV CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH}
RUN go build -trimpath -ldflags="-s -w" -o /out/Plrx .

# ---------------- runtime ----------------
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/Plrx /app/Plrx
COPY ai.yaml.example /app/ai.yaml.example

EXPOSE 37654
USER nonroot:nonroot
ENTRYPOINT ["/app/Plrx"]
