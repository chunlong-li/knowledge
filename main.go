package main

import (
	"log"

	"knowledge/internal/config"
	"knowledge/internal/db"
	"knowledge/internal/handler"
	"knowledge/internal/router"
)

func main() {
	cfg := config.Load()
	database, err := db.Init(cfg)
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer database.Close()

	h := &handler.Handler{DB: database, Cfg: cfg}
	r := router.Setup(cfg, h)
	handler.StartZupuSync(h) // 后台定时同步族谱内容（族谱不在线时自动跳过）

	log.Printf("%s v%s 启动，监听 :%s（数据目录 %s）", cfg.AppName, cfg.Version, cfg.Port, cfg.DataDir)
	if cfg.SSOSecret == "zupu-kb-sso-dev" {
		log.Printf("[kb] ⚠️ 安全提醒：KB_SSO_SECRET 仍是默认值 zupu-kb-sso-dev（公开值，可被伪造为超管 JWT）。" +
			"请在部署配置里改成随机值，并让族谱侧 FAMILY_TREE_KB_SSO_SECRET 保持一致；" +
			"独立部署建议同时设置 KB_JWT_SECRET。")
	}
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}
