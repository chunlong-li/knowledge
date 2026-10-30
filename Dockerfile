# ===== 阶段1：构建后端 =====
FROM golang:alpine AS backend
ENV GOPROXY=https://goproxy.cn,direct
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
COPY main.go ./
COPY internal/ ./internal/
RUN go mod download && CGO_ENABLED=1 go build -tags sqlite_fts5 -o knowledge .

# ===== 阶段2：运行时 =====
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata curl
ENV TZ=Asia/Shanghai
RUN ln -sf /usr/share/zoneinfo/$TZ /etc/localtime && echo "$TZ" > /etc/timezone
WORKDIR /app
COPY --from=backend /app/knowledge .
COPY frontend/dist ./web

ENV KB_DATA_DIR=/data \
    KB_PORT=8090 \
    KB_TIMEZONE=Asia/Shanghai

VOLUME ["/data"]
EXPOSE 8090

# 启动脚本：优先使用数据卷 /data/runtime 中的二进制与前端（支持在线更新覆盖后自重启），否则回退内置版本
RUN printf '#!/bin/sh\nRUNTIME=/data/runtime\nif [ -x $RUNTIME/knowledge ]; then\n  cp -f $RUNTIME/knowledge /app/knowledge\n  chmod +x /app/knowledge\nfi\nif [ -d $RUNTIME/web ]; then\n  rm -rf /app/web\n  cp -r $RUNTIME/web /app/web\nfi\n' > /app/load_runtime.sh && chmod +x /app/load_runtime.sh

CMD sh -c 'sh /app/load_runtime.sh && exec ./knowledge'
