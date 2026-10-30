package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ListRelations GET /api/kb/entries/:id/relations —— 双向显示相关条目
func (h *Handler) ListRelations(c *gin.Context) {
	id := atoi64(c.Param("id"))
	_, _, role := currentUser(c)
	q := `SELECT r.id, r.target_id, COALESCE(e.title,''), COALESCE(c.name,''), COALESCE(NULLIF(e.visibility,''),'族内'), 'out'
	        FROM kb_relations r
	        LEFT JOIN kb_entries e ON e.id = r.target_id
	        LEFT JOIN kb_categories c ON c.id = e.category_id
	       WHERE r.entry_id = ? AND COALESCE(e.deleted_at,'') = '' AND e.id IS NOT NULL
	      UNION ALL
	      SELECT r.id, r.entry_id, COALESCE(e.title,''), COALESCE(c.name,''), COALESCE(NULLIF(e.visibility,''),'族内'), 'in'
	        FROM kb_relations r
	        LEFT JOIN kb_entries e ON e.id = r.entry_id
	        LEFT JOIN kb_categories c ON c.id = e.category_id
	       WHERE r.target_id = ? AND COALESCE(e.deleted_at,'') = '' AND e.id IS NOT NULL`
	rows, err := h.DB.Query(q, id, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var rid, other int64
		var title, cat, vis, dir string
		if rows.Scan(&rid, &other, &title, &cat, &vis, &dir) != nil {
			continue
		}
		// 目标条目可能对当前账号不可见（机密/草稿）→ 不列出
		if !visibilityAllowed(role, vis) {
			continue
		}
		if other == id {
			continue
		}
		items = append(items, gin.H{"id": rid, "other_id": other, "title": title, "category_name": cat, "dir": dir})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type relationReq struct {
	TargetID int64 `json:"target_id"`
}

// AddRelation POST /api/kb/entries/:id/relations
func (h *Handler) AddRelation(c *gin.Context) {
	if !canEdit(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	id := atoi64(c.Param("id"))
	var r relationReq
	if err := c.ShouldBindJSON(&r); err != nil || r.TargetID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 target_id"})
		return
	}
	if r.TargetID == id {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不能关联自己"})
		return
	}
	var n int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM kb_entries WHERE id=? AND COALESCE(deleted_at,'')=''`, r.TargetID).Scan(&n); err != nil || n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "目标条目不存在"})
		return
	}
	h.DB.QueryRow(`SELECT COUNT(*) FROM kb_relations WHERE (entry_id=? AND target_id=?) OR (entry_id=? AND target_id=?)`,
		id, r.TargetID, r.TargetID, id).Scan(&n)
	if n > 0 {
		c.JSON(http.StatusOK, gin.H{"ok": true, "dup": true})
		return
	}
	if _, err := h.DB.Exec(`INSERT INTO kb_relations (entry_id, target_type, target_id) VALUES (?,?,?)`, id, "entry", r.TargetID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var title string
	h.DB.QueryRow(`SELECT COALESCE(title,'') FROM kb_entries WHERE id=?`, id).Scan(&title)
	h.audit(c, "关联条目", title, "→ #"+itoa(r.TargetID))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteRelation DELETE /api/kb/relations/:id
func (h *Handler) DeleteRelation(c *gin.Context) {
	if !canEdit(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	if _, err := h.DB.Exec(`DELETE FROM kb_relations WHERE id=?`, atoi64(c.Param("id"))); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "取消关联", "#"+c.Param("id"), "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	b := []byte{}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
