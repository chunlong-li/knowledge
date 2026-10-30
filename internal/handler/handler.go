package handler

import (
	"net/http"
	"strings"

	"knowledge/internal/auth"
	"knowledge/internal/config"
	"knowledge/internal/db"

	"github.com/gin-gonic/gin"
)

// Handler 传承服务器所有处理器的公共依赖
type Handler struct {
	DB    *db.DB
	Cfg   *config.Config
	probe probeState
}

// Health 健康探测：族谱系统据此判断传承服务器是否在线、有哪些能力（caps 门控）
// GET /api/health
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"ok":           true,
		"app":          "knowledge",
		"name":         h.Cfg.AppName,
		"api":          1,
		"version":      h.Cfg.Version,
		"mode":         h.currentMode(), // standalone | integrated
		"since":        h.deployedAt(),
		"capabilities": []string{"library", "search"}, // 预留：library/library-lend/craft/ai
	})
}

// Me 当前身份（前端展示用）
// GET /api/kb/me
func (h *Handler) Me(c *gin.Context) {
	uid, name, role := currentUser(c)
	t, _ := c.Get("tenant")
	tn, _ := t.(string)
	c.JSON(http.StatusOK, gin.H{
		"user_id": uid, "name": name, "role": role, "tenant": tn,
		"app": h.Cfg.AppName, "version": h.Cfg.Version,
		"can_edit": canEdit(c), "can_manage": isKBAdmin(c),
	})
}

// sessionSecret 本地会话签名密钥（独立配置 KB_JWT_SECRET 时与族谱共享密钥分离）
func (h *Handler) sessionSecret() string {
	if v := strings.TrimSpace(h.Cfg.JWTSecret); v != "" {
		return v
	}
	return h.Cfg.SSOSecret
}

// claimsFromRequest 解析请求里的身份凭证：X-KB-Auth 头 / ?token= / kb_token cookie。
// （浏览器对 <img>/<a> 发不出请求头，只能靠 cookie —— 本地登录会下发，族谱反代也会写同名 cookie）
func (h *Handler) claimsFromRequest(c *gin.Context) (*auth.Claims, bool) {
	t := c.GetHeader("X-KB-Auth")
	if t == "" {
		t = c.Query("token")
	}
	if t == "" {
		t, _ = c.Cookie("kb_token")
	}
	if t == "" {
		return nil, false
	}
	cl, err := auth.Verify(h.Cfg.SSOSecret, t)
	if err != nil && h.sessionSecret() != h.Cfg.SSOSecret {
		// 兼容：本地会话可能用独立的 KB_JWT_SECRET 签发
		cl, err = auth.Verify(h.sessionSecret(), t)
	}
	if err != nil {
		return nil, false
	}
	return cl, true
}

// Auth 身份中间件：优先校验族谱签发的 JWT；未配置密钥时进入独立/开发模式
func (h *Handler) Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.Cfg.SSOSecret == "" {
			// 独立模式（尚未接入族谱 SSO）：放行，给默认身份，便于主体自测
			c.Set("user_id", int64(0))
			c.Set("user_name", "访客")
			c.Set("role", "超级管理员")
			c.Set("tenant", "default")
			c.Next()
			return
		}
		cl, ok := h.claimsFromRequest(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录：缺少或无效的身份凭证"})
			return
		}
		c.Set("user_id", cl.Sub)
		c.Set("user_name", cl.Name)
		c.Set("role", cl.Role)
		c.Set("tenant", cl.Tenant)
		c.Next()
	}
}
