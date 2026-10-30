package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// probeState 族谱系统探测缓存（避免每次请求都去探测）
type probeState struct {
	mu     sync.Mutex
	last   time.Time
	base   string
	online bool
	since  int64 // 族谱系统首次初始化时间（秒）
}

// candidates 族谱系统候选地址：优先 KB_ZUPU_URL（可逗号分隔），否则用常见部署地址兜底
func (h *Handler) candidates() []string {
	var out []string
	if h.Cfg.ZupuURL != "" {
		for _, u := range strings.Split(h.Cfg.ZupuURL, ",") {
			if u = strings.TrimSpace(u); u != "" {
				out = append(out, strings.TrimRight(u, "/"))
			}
		}
	}
	out = append(out,
		"http://familytree:8080",           // 同一 compose 网络内的服务名
		"http://127.0.0.1:8080",            // 同机部署
		"http://host.docker.internal:8080", // 容器访问宿主机
	)
	return out
}

// probeZupu 检测族谱系统是否已部署（结果缓存 30s）
func (h *Handler) probeZupu() (base string, online bool, since int64) {
	h.probe.mu.Lock()
	defer h.probe.mu.Unlock()
	if !h.probe.last.IsZero() && time.Since(h.probe.last) < 30*time.Second {
		return h.probe.base, h.probe.online, h.probe.since
	}
	h.probe.last = time.Now()
	h.probe.base, h.probe.online, h.probe.since = "", false, 0
	if h.Cfg.Standalone {
		return "", false, 0
	}
	cli := &http.Client{Timeout: 2 * time.Second}
	for _, c := range h.candidates() {
		resp, err := cli.Get(c + "/api/settings/version")
		if err != nil {
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			continue
		}
		var m struct {
			Version string `json:"version"`
			Since   int64  `json:"since"`
		}
		if json.Unmarshal(raw, &m) == nil && m.Version != "" {
			h.probe.base, h.probe.online, h.probe.since = c, true, m.Since
			return c, true, m.Since
		}
	}
	return "", false, 0
}

// deployedAt 本服务首次初始化时间（秒）
func (h *Handler) deployedAt() int64 {
	var s string
	if err := h.DB.QueryRow(`SELECT value FROM kb_settings WHERE key='deployed_at'`).Scan(&s); err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// resolveMode 决定登录源：
//
//	standalone = 本服务自带登录库；integrated = 借用族谱系统登录（SSO）
//
// 规则：显式 KB_LOGIN_MODE 优先；auto 时“检测到族谱系统则借用其登录，否则用自带登录库”
func (h *Handler) resolveMode() (mode, base string, online bool) {
	base, online, _ = h.probeZupu()
	switch strings.ToLower(strings.TrimSpace(h.Cfg.LoginMode)) {
	case "local", "standalone":
		return "standalone", base, online
	case "zupu", "integrated":
		if online {
			return "integrated", base, true
		}
		return "standalone", base, false
	}
	// auto：检测到族谱系统 => 借用族谱登录；否则用自带登录库
	if online {
		return "integrated", base, true
	}
	return "standalone", base, false
}

// Mode 当前运行模式（公开，供前端与族谱端决定登录方式）
// GET /api/kb/mode
func (h *Handler) Mode(c *gin.Context) {
	mode, base, online := h.resolveMode()
	c.JSON(http.StatusOK, gin.H{
		"mode":           mode,
		"zupu_url":       base,
		"zupu_online":    online,
		"login":          true, // 登录入口始终可用（来源可选）
		"default_source": h.defaultSource(),
		"since":          h.deployedAt(),
		"app":            h.Cfg.AppName,
		"version":        h.Cfg.Version,
	})
}

// defaultSource 默认登录来源：检测到族谱则族谱账号，否则知识库账号
func (h *Handler) defaultSource() string {
	switch strings.ToLower(strings.TrimSpace(h.Cfg.LoginMode)) {
	case "local", "standalone":
		return "local"
	case "zupu", "integrated":
		return "zupu"
	}
	if _, online, _ := h.probeZupu(); online {
		return "zupu"
	}
	return "local"
}

// currentMode 内部用：返回模式字符串
func (h *Handler) currentMode() string {
	m, _, _ := h.resolveMode()
	return m
}
