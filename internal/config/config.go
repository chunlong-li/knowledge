package config

import (
	"os"
	"strings"
)

// Config 传承服务器配置（全部可由环境变量覆盖；与族谱系统对齐的部署习惯）
type Config struct {
	AppName    string
	Version    string
	Port       string
	DataDir    string
	DBPath     string
	Timezone   string
	SSOSecret  string // 与族谱系统共享的密钥：用于校验族谱签发的短时 JWT
	JWTSecret  string // 本地会话签名密钥（KB_JWT_SECRET）；留空 = 沿用 SSOSecret（兼容旧部署）
	BasePath   string // 反向代理子路径（如 /knowledge），空表示根路径
	ZupuURL    string // 族谱系统地址（探测是否已部署；支持逗号分隔多个候选）
	Standalone bool   // 强制独立模式（忽略族谱探测）
	LoginMode  string // 登录源模式：auto（先部署为准）| zupu（用族谱登录）| local（用本服务登录）

	// 在线更新
	EnableUpdate   bool   // 是否启用在线更新
	UpdateURL      string // 更新清单地址（latest.json 或 packages.json）
	UpdateInsecure bool   // 拉取/下载时忽略证书校验（自签证用）
	AppDir         string // 部署目录（数据卷）：二进制 + web，容器启动脚本从这里覆盖内置版本
	WebDir         string // 前端目录
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Load 读取配置
func Load() *Config {
	dataDir := getEnv("KB_DATA_DIR", "./data")
	appDir := getEnv("KB_APP_DIR", dataDir+"/runtime")
	return &Config{
		AppName:    getEnv("KB_APP_NAME", "传承服务器"),
		Version:    getEnv("KB_VERSION", "1.2.0.1007.1"),
		Port:       getEnv("KB_PORT", "8090"),
		DataDir:    dataDir,
		DBPath:     getEnv("KB_DB_PATH", dataDir+"/kb.db"),
		Timezone:   getEnv("KB_TIMEZONE", getEnv("FAMILY_TREE_TIMEZONE", "Asia/Shanghai")),
		SSOSecret:  getEnv("KB_SSO_SECRET", "zupu-kb-sso-dev"),
		JWTSecret:  getEnv("KB_JWT_SECRET", ""), // 本地会话签名密钥；留空则沿用 KB_SSO_SECRET（兼容旧部署）
		BasePath:   strings.TrimRight(getEnv("KB_BASE_PATH", ""), "/"),
		ZupuURL:    getEnv("KB_ZUPU_URL", ""),
		Standalone: isTruthy(getEnv("KB_STANDALONE", "")),
		LoginMode:  getEnv("KB_LOGIN_MODE", "auto"),

		EnableUpdate:   isTruthy(getEnv("KB_ENABLE_UPDATE", "true")),
		UpdateURL:      getEnv("KB_UPDATE_URL", ""),
		UpdateInsecure: isTruthy(getEnv("KB_UPDATE_INSECURE", "")),
		AppDir:         appDir,
		WebDir:         getEnv("KB_WEB_DIR", appDir+"/web"),
	}
}

func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
