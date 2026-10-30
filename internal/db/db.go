package db

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"strings"

	"knowledge/internal/config"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

// DB 封装（保留 FTS5 可用性标记，搜索时据此退化到 LIKE）
type DB struct {
	*sql.DB
	FTS bool
}

// Init 打开数据库并建表
func Init(cfg *config.Config) (*DB, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "upload"), 0o755); err != nil {
		return nil, err
	}
	dsn := cfg.DBPath + "?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on"
	sqldb, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	if err := sqldb.Ping(); err != nil {
		return nil, err
	}
	d := &DB{DB: sqldb}
	if err := d.migrate(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *DB) migrate() error {
	stmts := []string{
		// 分类树
		`CREATE TABLE IF NOT EXISTS kb_categories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			parent_id INTEGER DEFAULT 0,
			name TEXT NOT NULL,
			icon TEXT DEFAULT '',
			description TEXT DEFAULT '',
			sort_order INTEGER DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now','localtime')),
			updated_at TEXT DEFAULT (datetime('now','localtime'))
		)`,
		// 知识条目（统一模型）
		`CREATE TABLE IF NOT EXISTS kb_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			category_id INTEGER DEFAULT 0,
			title TEXT NOT NULL,
			summary TEXT DEFAULT '',
			content TEXT DEFAULT '',
			tags TEXT DEFAULT '',
			cover TEXT DEFAULT '',
			visibility TEXT DEFAULT '族内',
			status TEXT DEFAULT 'published',
			source TEXT DEFAULT '',
			author_id INTEGER DEFAULT 0,
			author_name TEXT DEFAULT '',
			sort_order INTEGER DEFAULT 0,
			is_pinned INTEGER DEFAULT 0,
			view_count INTEGER DEFAULT 0,
			doc_path TEXT DEFAULT '',
			doc_name TEXT DEFAULT '',
			doc_kind TEXT DEFAULT '',
			doc_size INTEGER DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now','localtime')),
			updated_at TEXT DEFAULT (datetime('now','localtime'))
		)`,
		// 附件
		`CREATE TABLE IF NOT EXISTS kb_attachments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entry_id INTEGER DEFAULT 0,
			kind TEXT DEFAULT 'file',
			name TEXT DEFAULT '',
			path TEXT DEFAULT '',
			url TEXT DEFAULT '',
			mime TEXT DEFAULT '',
			size INTEGER DEFAULT 0,
			sort_order INTEGER DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now','localtime'))
		)`,
		// —— 预留表 ——
		`CREATE TABLE IF NOT EXISTS kb_versions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entry_id INTEGER DEFAULT 0,
			snapshot TEXT DEFAULT '',
			editor_id INTEGER DEFAULT 0,
			editor_name TEXT DEFAULT '',
			created_at TEXT DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE TABLE IF NOT EXISTS kb_relations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entry_id INTEGER DEFAULT 0,
			target_type TEXT DEFAULT '',
			target_id INTEGER DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE TABLE IF NOT EXISTS kb_audit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER DEFAULT 0,
			user_name TEXT DEFAULT '',
			role TEXT DEFAULT '',
			action TEXT DEFAULT '',
			target TEXT DEFAULT '',
			detail TEXT DEFAULT '',
			ip TEXT DEFAULT '',
			created_at TEXT DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_audit_created ON kb_audit(created_at)`,
		`CREATE TABLE IF NOT EXISTS kb_settings (key TEXT PRIMARY KEY, value TEXT)`,
		`INSERT OR REPLACE INTO kb_settings (key,value) VALUES ('schema', '1')`,
		// 首次初始化时间（用于“先部署为准”判定登录源；OR IGNORE 保证只写一次）
		`INSERT OR IGNORE INTO kb_settings (key,value) VALUES ('deployed_at', CAST(strftime('%s','now') AS TEXT))`,
		// 本地账号（仅独立模式使用；检测到族谱系统时改走族谱 SSO）
		`CREATE TABLE IF NOT EXISTS kb_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			display_name TEXT DEFAULT '',
			role TEXT DEFAULT '超级管理员',
			created_at TEXT DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_entries_cat ON kb_entries(category_id)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_entries_status ON kb_entries(status)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			return err
		}
	}
	d.alterStatements()
	if err := d.seedUsers(); err != nil {
		return err
	}
	// FTS5 可选：可用则建虚拟表（trigram 分词 → 中文子串可搜）；不可用则退化 LIKE
	if err := d.setupFTS(); err != nil {
		log.Printf("[kb] FTS5 不可用，全文检索退化为 LIKE：%v", err)
	} else {
		d.FTS = true
	}
	return nil
}

// setupFTS 建立/迁移全文索引
//   - 老库若是默认分词（unicode61：中文按标点切成整块 → 句中词永远搜不到），自动换成 trigram 并重建一次
//   - 索引维护改为 kb_entries 上的触发器（增量），不再每次写都全量 rebuild
func (d *DB) setupFTS() error {
	var existing string
	_ = d.QueryRow(`SELECT COALESCE(sql,'') FROM sqlite_master WHERE type='table' AND name='kb_entries_fts'`).Scan(&existing)
	if existing != "" && !strings.Contains(existing, "trigram") {
		log.Printf("[kb] 迁移全文索引为 trigram 分词（原定义：%s）", strings.TrimSpace(existing))
		if _, err := d.Exec(`DROP TABLE IF EXISTS kb_entries_fts`); err != nil {
			return err
		}
		existing = ""
	}
	base := `CREATE VIRTUAL TABLE IF NOT EXISTS kb_entries_fts USING fts5(title, summary, content, tags, content='kb_entries', content_rowid='id'`
	if _, err := d.Exec(base + `, tokenize='trigram')`); err != nil {
		// 老版本 SQLite 不支持 trigram → 退回默认分词（中文子串会搜不到，至少不崩）
		if _, err2 := d.Exec(base + `)`); err2 != nil {
			return err2
		}
	}
	for _, t := range []string{
		`CREATE TRIGGER IF NOT EXISTS kb_entries_ai AFTER INSERT ON kb_entries BEGIN
			INSERT INTO kb_entries_fts(rowid,title,summary,content,tags) VALUES (new.id,new.title,new.summary,new.content,new.tags);
		END`,
		`CREATE TRIGGER IF NOT EXISTS kb_entries_ad AFTER DELETE ON kb_entries BEGIN
			INSERT INTO kb_entries_fts(kb_entries_fts,rowid,title,summary,content,tags) VALUES('delete',old.id,old.title,old.summary,old.content,old.tags);
		END`,
		`CREATE TRIGGER IF NOT EXISTS kb_entries_au AFTER UPDATE ON kb_entries BEGIN
			INSERT INTO kb_entries_fts(kb_entries_fts,rowid,title,summary,content,tags) VALUES('delete',old.id,old.title,old.summary,old.content,old.tags);
			INSERT INTO kb_entries_fts(rowid,title,summary,content,tags) VALUES (new.id,new.title,new.summary,new.content,new.tags);
		END`,
	} {
		if _, err := d.Exec(t); err != nil {
			log.Printf("[kb] 建 FTS 触发器失败：%v", err)
		}
	}
	if existing == "" { // 新表或刚迁移 → 重建一次索引
		if _, err := d.Exec(`INSERT INTO kb_entries_fts(kb_entries_fts) VALUES('rebuild')`); err != nil {
			return err
		}
	}
	return nil
}

// 预留：后续加列时在此追加 ALTER TABLE（对老库生效）
func (d *DB) alterStatements() {
	stmts := []string{
		`ALTER TABLE kb_entries ADD COLUMN doc_path TEXT DEFAULT ''`,
		`ALTER TABLE kb_entries ADD COLUMN doc_name TEXT DEFAULT ''`,
		`ALTER TABLE kb_entries ADD COLUMN doc_kind TEXT DEFAULT ''`,
		`ALTER TABLE kb_entries ADD COLUMN doc_size INTEGER DEFAULT 0`,
		// 族谱系统联动：ext_key=同步来源键（幂等去重），sys=系统分类标记（只读）
		`ALTER TABLE kb_entries ADD COLUMN ext_key TEXT DEFAULT ''`,
		`ALTER TABLE kb_categories ADD COLUMN sys TEXT DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_kb_entries_ext ON kb_entries(ext_key)`,
		// 回收站：软删除标记（空 = 正常）
		`ALTER TABLE kb_entries ADD COLUMN deleted_at TEXT DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_kb_entries_deleted ON kb_entries(deleted_at)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			log.Printf("[kb] alter 失败：%v（%s）", err, s)
		}
	}
}

// seedUsers 首次初始化默认管理员（admin / admin123，独立模式登录用）
func (d *DB) seedUsers() error {
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM kb_users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), 12)
	if err != nil {
		return err
	}
	_, err = d.Exec(
		`INSERT INTO kb_users (username, password_hash, display_name, role) VALUES ('admin', ?, '管理员', '超级管理员')`,
		string(hash),
	)
	if err == nil {
		log.Printf("[kb] 已创建默认账号 admin / admin123（独立模式登录用，请尽快修改）")
	}
	return err
}
