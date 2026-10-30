package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// audit 记一条操作日志（谁、什么时候、干了什么）。日志量按 5000 条滚动保留。
func (h *Handler) audit(c *gin.Context, action, target, detail string) {
	uid, name, role := currentUser(c)
	h.auditAs(c, uid, name, role, action, target, detail)
}

// auditAs 指定身份记日志（登录接口还没走 Auth 中间件，用它）
func (h *Handler) auditAs(c *gin.Context, uid int64, name, role, action, target, detail string) {
	ip := ""
	if c != nil {
		ip = c.ClientIP()
	}
	if _, err := h.DB.Exec(`INSERT INTO kb_audit (user_id,user_name,role,action,target,detail,ip) VALUES (?,?,?,?,?,?,?)`,
		uid, name, role, action, target, detail, ip); err != nil {
		return
	}
	// 只保留最近 5000 条
	h.DB.Exec(`DELETE FROM kb_audit WHERE id <= (SELECT COALESCE(MAX(id),0)-5000 FROM kb_audit)`)
}

// AuditLogs GET /api/kb/audit?page=&page_size=&q= —— 仅管理账号可查看
func (h *Handler) AuditLogs(c *gin.Context) {
	if !isKBAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 50
	}
	kw := strings.TrimSpace(c.Query("q"))
	where := "1=1"
	args := []any{}
	if kw != "" {
		where = "(action LIKE ? OR target LIKE ? OR user_name LIKE ? OR detail LIKE ?)"
		lk := "%" + kw + "%"
		args = append(args, lk, lk, lk, lk)
	}
	var total int
	if err := h.DB.QueryRow("SELECT COUNT(*) FROM kb_audit WHERE "+where, args...).Scan(&total); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	q := "SELECT id, COALESCE(user_name,''), COALESCE(role,''), COALESCE(action,''), COALESCE(target,''), COALESCE(detail,''), COALESCE(ip,''), COALESCE(created_at,'')" +
		" FROM kb_audit WHERE " + where + " ORDER BY id DESC LIMIT ? OFFSET ?"
	args = append(args, size, (page-1)*size)
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var id int64
		var user, role, action, target, detail, ip, at string
		if rows.Scan(&id, &user, &role, &action, &target, &detail, &ip, &at) == nil {
			items = append(items, gin.H{"id": id, "user_name": user, "role": role, "action": action,
				"target": target, "detail": detail, "ip": ip, "created_at": at})
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}
