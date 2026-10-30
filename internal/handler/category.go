package handler

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Category 知识分类（树）
type Category struct {
	ID          int64  `json:"id"`
	ParentID    int64  `json:"parent_id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
	Sys         string `json:"sys"` // 非空 = 系统分类（族谱联动，只读）
	EntryCount  int    `json:"entry_count"`
	CreatedAt   string `json:"created_at"`
}

// catSysOf 分类的联动标记（空 = 普通分类）
func (h *Handler) catSysOf(id any) string {
	var sys string
	if err := h.DB.QueryRow(`SELECT COALESCE(sys,'') FROM kb_categories WHERE id=?`, id).Scan(&sys); err != nil {
		return ""
	}
	return sys
}

func currentUser(c *gin.Context) (int64, string, string) {
	id, _ := c.Get("user_id")
	uid, _ := id.(int64)
	name, _ := c.Get("user_name")
	role, _ := c.Get("role")
	ns, _ := name.(string)
	rs, _ := role.(string)
	return uid, ns, rs
}

func canEdit(c *gin.Context) bool {
	_, _, role := currentUser(c)
	switch role {
	case "理事", "房支理事", "财务管理", "理事长", "超级管理员", "管理员", "编辑":
		return true
	}
	return false
}

// isKBAdmin 是否为管理账号（可管理账号 / 在线更新 / 同步）
// 口径与族谱侧保持一致：理事长与超级管理员同等（族谱 /update、/settings 的 roles 就是 ['理事长','超级管理员']），
// 另保留本地账号体系里的「管理员」。
func isKBAdmin(c *gin.Context) bool {
	_, _, role := currentUser(c)
	return role == "理事长" || role == "超级管理员" || role == "管理员"
}

// ListCategories GET /api/kb/categories
func (h *Handler) ListCategories(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT c.id, c.parent_id, c.name, c.icon, c.description, c.sort_order, COALESCE(c.sys,''), c.created_at,
		(SELECT COUNT(*) FROM kb_entries e WHERE e.category_id = c.id) AS cnt
		FROM kb_categories c ORDER BY c.sort_order ASC, c.id ASC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []Category{}
	for rows.Next() {
		var x Category
		var created sql.NullString
		if err := rows.Scan(&x.ID, &x.ParentID, &x.Name, &x.Icon, &x.Description, &x.SortOrder, &x.Sys, &created, &x.EntryCount); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		x.CreatedAt = created.String
		list = append(list, x)
	}
	// 多级分类：父分类的条目数 = 自身 + 全部后代（与条目筛选口径一致）
	kids := map[int64][]int64{}
	direct := map[int64]int{}
	for _, x := range list {
		kids[x.ParentID] = append(kids[x.ParentID], x.ID)
		direct[x.ID] = x.EntryCount
	}
	var sum func(id int64, seen map[int64]bool) int
	sum = func(id int64, seen map[int64]bool) int {
		if seen[id] {
			return 0
		}
		seen[id] = true
		n := direct[id]
		for _, k := range kids[id] {
			n += sum(k, seen)
		}
		return n
	}
	for i := range list {
		list[i].EntryCount = sum(list[i].ID, map[int64]bool{})
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}

type categoryReq struct {
	ParentID    int64  `json:"parent_id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

// CreateCategory POST /api/kb/categories
func (h *Handler) CreateCategory(c *gin.Context) {
	if !canEdit(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	var r categoryReq
	if err := c.ShouldBindJSON(&r); err != nil || r.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "名称不能为空"})
		return
	}
	if r.ParentID != 0 && h.catSysOf(r.ParentID) != "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "族谱同步分类为只读，不能在其下新建分类"})
		return
	}
	res, err := h.DB.Exec(`INSERT INTO kb_categories (parent_id,name,icon,description,sort_order) VALUES (?,?,?,?,?)`,
		r.ParentID, r.Name, r.Icon, r.Description, r.SortOrder)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// UpdateCategory PUT /api/kb/categories/:id
func (h *Handler) UpdateCategory(c *gin.Context) {
	if !canEdit(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	var r categoryReq
	if err := c.ShouldBindJSON(&r); err != nil || r.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "名称不能为空"})
		return
	}
	id := atoi64(c.Param("id"))
	if h.catSysOf(id) != "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "族谱同步分类为只读，不能修改"})
		return
	}
	if r.ParentID != 0 && h.catSysOf(r.ParentID) != "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "不能移动到族谱同步分类下"})
		return
	}
	if r.ParentID == id {
		c.JSON(http.StatusBadRequest, gin.H{"error": "父分类不能是自己"})
		return
	}
	// 禁止把分类移动到自己或自己的后代下（形成环）
	for p, i := r.ParentID, 0; p != 0 && i < 500; i++ {
		if p == id {
			c.JSON(http.StatusBadRequest, gin.H{"error": "不能移动到自己的子分类下"})
			return
		}
		var pp int64
		if err := h.DB.QueryRow(`SELECT parent_id FROM kb_categories WHERE id=?`, p).Scan(&pp); err != nil {
			break
		}
		p = pp
	}
	_, err := h.DB.Exec(`UPDATE kb_categories SET parent_id=?,name=?,icon=?,description=?,sort_order=?,updated_at=datetime('now','localtime') WHERE id=?`,
		r.ParentID, r.Name, r.Icon, r.Description, r.SortOrder, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteCategory DELETE /api/kb/categories/:id
func (h *Handler) DeleteCategory(c *gin.Context) {
	if !canEdit(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	id := c.Param("id")
	if h.catSysOf(id) != "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "族谱同步分类为只读，不能删除"})
		return
	}
	var n int
	h.DB.QueryRow(`SELECT COUNT(*) FROM kb_categories WHERE parent_id=?`, id).Scan(&n)
	if n > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先删除子分类"})
		return
	}
	// 该分类下的条目转为「未分类」
	h.DB.Exec(`UPDATE kb_entries SET category_id=0 WHERE category_id=?`, id)
	if _, err := h.DB.Exec(`DELETE FROM kb_categories WHERE id=?`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type catOrderReq struct {
	Items []struct {
		ID        int64 `json:"id"`
		ParentID  int64 `json:"parent_id"`
		SortOrder int   `json:"sort_order"`
	} `json:"items"`
}

// ReorderCategories POST /api/kb/categories/reorder
// 批量更新分类的「上级分类 + 排序」，用于拖动排序 / 调整层级
func (h *Handler) ReorderCategories(c *gin.Context) {
	if !canEdit(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	var r catOrderReq
	if err := c.ShouldBindJSON(&r); err != nil || len(r.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	tx, err := h.DB.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for _, it := range r.Items {
		if it.ID == 0 || it.ID == it.ParentID {
			tx.Rollback()
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		if h.catSysOf(it.ID) != "" || (it.ParentID != 0 && h.catSysOf(it.ParentID) != "") {
			tx.Rollback()
			c.JSON(http.StatusForbidden, gin.H{"error": "族谱同步分类为只读，不能拖动"})
			return
		}
		if _, err := tx.Exec(`UPDATE kb_categories SET parent_id=?, sort_order=?, updated_at=datetime('now','localtime') WHERE id=?`,
			it.ParentID, it.SortOrder, it.ID); err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
