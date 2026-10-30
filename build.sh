#!/bin/bash
# 传承服务器（家族知识库）本地构建/打包
# 用法：./build.sh [版本号]
set -e
cd "$(dirname "$0")"
VER="${1:-0.2.0}"

echo "==> 1/3 构建前端"
( cd frontend && npm run build )

echo "==> 2/3 复制前端资源到 web/"
rm -rf web && cp -r frontend/dist web
find web -type f -exec chmod 644 {} + 2>/dev/null || true
find web -type d -exec chmod 755 {} + 2>/dev/null || true

echo "==> 3/3 构建后端（CGO + FTS5）"
CGO_ENABLED=1 go build -tags sqlite_fts5 -o knowledge .

PKG="传承服务器v${VER}.update.tar.gz"
tar -czf "$PKG" knowledge web
echo "==> 完成: $(pwd)/$PKG"
