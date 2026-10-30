package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// kbUser 本地账号（仅独立模式使用）
type kbUser struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at"`
}

var kbRoles = map[string]bool{"超级管理员": true, "管理员": true, "编辑": true, "访客": true}

// requireLocalAdmin 需管理员身份（账号管理；两种登录来源均可，不区分模式）
func (h *Handler) requireLocalAdmin(c *gin.Context) bool {
	if !isKBAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅管理员可管理账号"})
		return false
	}
	return true
}

// ListUsers GET /api/kb/users
func (h *Handler) ListUsers(c *gin.Context) {
	if !h.requireLocalAdmin(c) {
		return
	}
	rows, err := h.DB.Query(`SELECT id, username, COALESCE(display_name,''), COALESCE(role,''), COALESCE(created_at,'') FROM kb_users ORDER BY id ASC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []kbUser{}
	for rows.Next() {
		var u kbUser
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		list = append(list, u)
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}

// CreateUser POST /api/kb/users
func (h *Handler) CreateUser(c *gin.Context) {
	if !h.requireLocalAdmin(c) {
		return
	}
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Password) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "账号必填，密码至少 6 位"})
		return
	}
	role := req.Role
	if !kbRoles[role] {
		role = "编辑"
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码处理失败"})
		return
	}
	if _, err := h.DB.Exec(
		`INSERT INTO kb_users (username, password_hash, display_name, role) VALUES (?, ?, ?, ?)`,
		req.Username, string(hash), req.DisplayName, role,
	); err != nil {
		if strings.Contains(strings.ToUpper(err.Error()), "UNIQUE") {
			c.JSON(http.StatusConflict, gin.H{"error": "账号已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "新建账号", req.Username, "角色:"+role)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// UpdateUser PUT /api/kb/users/:id（可改显示名 / 角色 / 重置密码）
func (h *Handler) UpdateUser(c *gin.Context) {
	if !h.requireLocalAdmin(c) {
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
		Password    string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if req.Role != "" && !kbRoles[req.Role] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "角色无效"})
		return
	}
	if req.Role != "" && req.Role != "超级管理员" && h.isLastSuper(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "至少保留一个超级管理员"})
		return
	}
	if req.DisplayName != "" || req.Role != "" {
		if _, err := h.DB.Exec(
			`UPDATE kb_users SET display_name=COALESCE(NULLIF(?,''),display_name), role=COALESCE(NULLIF(?,''),role) WHERE id=?`,
			req.DisplayName, req.Role, id,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if req.Password != "" {
		if len(req.Password) < 6 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "密码至少 6 位"})
			return
		}
		hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
		h.DB.Exec(`UPDATE kb_users SET password_hash=? WHERE id=?`, string(hash), id)
	}
	h.audit(c, "修改账号", "#"+c.Param("id"), "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteUser DELETE /api/kb/users/:id
func (h *Handler) DeleteUser(c *gin.Context) {
	if !h.requireLocalAdmin(c) {
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	uid, _, _ := currentUser(c)
	if id == uid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不能删除当前登录账号"})
		return
	}
	var role string
	if err := h.DB.QueryRow(`SELECT COALESCE(role,'') FROM kb_users WHERE id=?`, id).Scan(&role); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "账号不存在"})
		return
	}
	if role == "超级管理员" && h.isLastSuper(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "至少保留一个超级管理员"})
		return
	}
	if _, err := h.DB.Exec(`DELETE FROM kb_users WHERE id=?`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "删除账号", "#"+c.Param("id"), "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ChangeOwnPassword 当前登录用户修改自己的密码（仅限本地账号）
// POST /api/kb/users/me/change-password
func (h *Handler) ChangeOwnPassword(c *gin.Context) {
	uid, _, _ := currentUser(c)
	if uid == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅本地账号支持修改密码"})
		return
	}
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if len(req.NewPassword) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码至少 6 位"})
		return
	}
	if req.OldPassword == req.NewPassword {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码不能与旧密码相同"})
		return
	}

	// 校验该用户是否为本地账号（kb_users 中存在）
	var hash string
	err := h.DB.QueryRow(`SELECT password_hash FROM kb_users WHERE id=?`, uid).Scan(&hash)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅本地账号支持修改密码，族谱集成登录请在族谱系统中修改"})
		return
	}

	// 验证旧密码
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "旧密码错误"})
		return
	}

	// 更新为新密码
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 12)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码处理失败"})
		return
	}
	if _, err := h.DB.Exec(`UPDATE kb_users SET password_hash=? WHERE id=?`, string(newHash), uid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "密码修改成功，请使用新密码重新登录"})
}

// isLastSuper 该账号是否为最后一个超级管理员
func (h *Handler) isLastSuper(id int64) bool {
	var role string
	if err := h.DB.QueryRow(`SELECT COALESCE(role,'') FROM kb_users WHERE id=?`, id).Scan(&role); err != nil || role != "超级管理员" {
		return false
	}
	var n int
	h.DB.QueryRow(`SELECT COUNT(*) FROM kb_users WHERE role='超级管理员'`).Scan(&n)
	return n <= 1
}
