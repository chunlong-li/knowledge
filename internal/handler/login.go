package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"knowledge/internal/auth"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// Login 登录：支持两种账号来源（source = zupu | local），默认按检测结果
// POST /api/kb/login
func (h *Handler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Source   string `json:"source"` // zupu=用族谱账号；local=用知识库账号
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Username) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入账号和密码"})
		return
	}
	src := strings.ToLower(strings.TrimSpace(req.Source))
	if src != "zupu" && src != "local" {
		src = h.defaultSource()
	}

	// —— 用族谱账号：调族谱 /api/auth/verify 校验（不签发/不覆盖 token，避免顶号）——
	if src == "zupu" {
		base, online, _ := h.probeZupu()
		if !online || base == "" {
			base = strings.TrimRight(strings.TrimSpace(h.Cfg.ZupuURL), "/")
		}
		if base == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "未检测到族谱系统，请改用知识库账号登录"})
			return
		}
		id, name, role, err := h.verifyWithZupu(base, req.Username, req.Password)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		h.issue(c, id, name, role, "zupu")
		return
	}

	// —— 用知识库账号 ——
	var id int64
	var name, role, hash string
	err := h.DB.QueryRow(
		`SELECT id, COALESCE(display_name,''), COALESCE(role,'管理员'), password_hash FROM kb_users WHERE username=?`,
		req.Username,
	).Scan(&id, &name, &role, &hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "账号或密码错误"})
		return
	}
	if name == "" {
		name = req.Username
	}
	h.issue(c, id, name, role, "local")
}

// verifyWithZupu 调族谱 /api/auth/verify 校验账号密码，返回身份
func (h *Handler) verifyWithZupu(base, username, password string) (int64, string, string, error) {
	body, _ := json.Marshal(gin.H{"username": username, "password": password})
	cli := &http.Client{Timeout: 6 * time.Second}
	resp, err := cli.Post(strings.TrimRight(base, "/")+"/api/auth/verify", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, "", "", errors.New("族谱系统不可达")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		msg := "族谱账号校验失败"
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		return 0, "", "", errors.New(msg)
	}
	var r struct {
		User struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	if json.Unmarshal(raw, &r) != nil || r.User.Username == "" {
		return 0, "", "", errors.New("族谱返回异常")
	}
	return r.User.ID, r.User.Username, r.User.Role, nil
}

// issue 签发本服务 JWT
func (h *Handler) issue(c *gin.Context, id int64, name, role, source string) {
	h.auditAs(c, id, name, role, "登录成功", name, "来源:"+source)
	tok := auth.Sign(h.sessionSecret(), auth.Claims{
		Sub:  id,
		Name: name,
		Role: role,
		Exp:  time.Now().Add(7 * 24 * time.Hour).Unix(),
	})
	// 同步下发 cookie：Markdown 正文里的 <img src="/upload/..."> 无法附加请求头，
	// 只能靠 cookie 携带身份（族谱反代在 /knowledge 下也会写同名 cookie）
	cookiePath := "/"
	if p := strings.TrimRight(h.Cfg.BasePath, "/"); p != "" {
		cookiePath = p + "/"
	}
	c.SetCookie("kb_token", tok, 7*24*3600, cookiePath, "", false, true)

	c.JSON(http.StatusOK, gin.H{
		"token":  tok,
		"source": source,
		"user": gin.H{
			"user_id": id, "name": name, "role": role,
			"can_edit": roleCanEdit(role),
		},
	})
}

func roleCanEdit(role string) bool {
	switch role {
	case "理事", "房支理事", "财务管理", "理事长", "超级管理员", "管理员", "编辑":
		return true
	}
	return false
}
