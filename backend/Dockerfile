# ---- 构建阶段 ----
FROM golang:1.23-alpine AS build
WORKDIR /src

# 先依赖缓存
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/wafer-audit ./cmd/server

# ---- 运行阶段 ----
FROM alpine:3.20
RUN apk add --no-cache wget
WORKDIR /app
COPY --from=build /out/wafer-audit /app/wafer-audit

ENV PORT=8080 GIN_MODE=release
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/wafer-audit"]
