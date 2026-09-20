# verify 服务：一次性验收任务，对真实 backend 执行可观察的端到端核对后退出
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/verify .

FROM alpine:3.20
COPY --from=build /out/verify /usr/local/bin/verify
# 等待后端就绪后再开始验收
ENTRYPOINT ["/usr/local/bin/verify"]
