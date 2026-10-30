package handler

// settingValue 读取 kb_settings（开源版保留的通用设置读取）
func (h *Handler) settingValue(k string) string {
	var v string
	h.DB.QueryRow(`SELECT value FROM kb_settings WHERE key=?`, k).Scan(&v)
	return v
}
