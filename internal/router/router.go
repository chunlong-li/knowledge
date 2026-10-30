package router

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"knowledge/internal/config"
	"knowledge/internal/handler"

	"github.com/gin-gonic/gin"
)

// Setup 组装路由
func Setup(cfg *config.Config, h *handler.Handler) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.MaxMultipartMemory = 128 << 20

	// 静态前端：优先配置的部署目录（在线更新覆盖），其次 /data 下的常规位置，最后内置 ./web
	webDir := "./web"
	for _, cand := range []string{cfg.WebDir, "/data/runtime/web", "/data/web"} {
		if cand == "" {
			continue
		}
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			webDir = cand
			break
		}
	}
	r.Static("/assets", webDir+"/assets")
	// 附件走鉴权路由（原来这里是 Static，等于免鉴权下载，机密附件会被直接拿走）
	// 附件：不挂 Auth —— 由 ServeUpload 自判（公开内容免登录、族内/房支要求身份、机密再按角色）
	r.GET("/upload/*path", h.ServeUpload)
	r.HEAD("/upload/*path", h.ServeUpload) // HEAD 也走同一处理（否则会落到 SPA 兜底返回 index.html）
	// index.html 禁用缓存：否则发新版后浏览器仍用缓存页面（里面引用的旧分块已删除 → 页面空白）
	r.GET("/", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.File(webDir + "/index.html")
	})
	r.StaticFile("/favicon.ico", webDir+"/favicon.ico")

	api := r.Group("/api")
	api.GET("/health", h.Health) // 健康探测：族谱据此门控

	// 公开接口：运行模式探测 + 本地登录（仅独立模式生效）
	api.GET("/kb/mode", h.Mode)
	api.POST("/kb/login", h.Login)

	kb := api.Group("/kb")
	kb.Use(h.Auth())
	{
		kb.GET("/me", h.Me)

		kb.GET("/categories", h.ListCategories)
		kb.POST("/categories", h.CreateCategory)
		kb.PUT("/categories/:id", h.UpdateCategory)
		kb.DELETE("/categories/:id", h.DeleteCategory)
		kb.POST("/categories/reorder", h.ReorderCategories)

		kb.GET("/entries", h.ListEntries)
		kb.POST("/entries", h.CreateEntry)
		kb.GET("/entries/:id", h.GetEntry)
		kb.PUT("/entries/:id", h.UpdateEntry)
		kb.DELETE("/entries/:id", h.DeleteEntry)

		kb.POST("/upload", h.Upload)

		// 导出：单条 Markdown / 整棵知识库 zip（可含附件文件）
		kb.GET("/entries/:id/export", h.ExportEntry)
		kb.GET("/export.zip", h.ExportAll)

		// 版本历史（改/删前自动留档，可回滚）
		kb.GET("/entries/:id/versions", h.ListVersions)
		kb.POST("/entries/:id/versions/:vid/restore", h.RestoreVersion)

		// 相关条目（双向）
		kb.GET("/entries/:id/relations", h.ListRelations)
		kb.POST("/entries/:id/relations", h.AddRelation)
		kb.DELETE("/relations/:id", h.DeleteRelation)

		// 操作日志（仅管理账号）
		kb.GET("/audit", h.AuditLogs)

		// 回收站（软删除）
		kb.GET("/trash", h.ListTrash)
		kb.POST("/trash/:id/restore", h.RestoreTrash)
		kb.DELETE("/trash/:id", h.PurgeTrash)

		// 族谱联动同步（状态：登录可见；手动触发：仅管理账号）
		kb.GET("/sync/status", h.SyncStatus)
		kb.POST("/sync/zupu", h.SyncZupu)

		// 在线更新（仅管理账号）
		kb.GET("/update/check", h.CheckUpdate)
		kb.POST("/update/apply", h.ApplyUpdate)
		kb.GET("/update/progress", h.UpdateProgress)

		// 本地账号管理（仅独立模式；集成模式下账号由族谱统一管理）
		kb.GET("/users", h.ListUsers)
		kb.POST("/users", h.CreateUser)
		kb.PUT("/users/:id", h.UpdateUser)
		kb.DELETE("/users/:id", h.DeleteUser)
		// 当前用户修改自己的密码（仅限本地 kb_users 账号）
		kb.POST("/users/me/change-password", h.ChangeOwnPassword)
	}

	// SPA 回退
	r.NoRoute(func(c *gin.Context) {
		if p := c.Request.URL.Path; p != "/" {
			clean := filepath.Clean(p)
			if !strings.Contains(clean, "..") {
				fp := filepath.Join(webDir, clean)
				if st, err := os.Stat(fp); err == nil && !st.IsDir() {
					c.File(fp)
					return
				}
			}
		}
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.File(webDir + "/index.html")
	})
	return r
}

var _ = http.StatusOK
