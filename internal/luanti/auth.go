// 管理员权限同步：引擎的 auth 条目只在玩家首次进服时创建（默认仅
// interact/shout），而面板 bridge 以 conf 的 name 身份执行 /grant 等命令，
// 引擎查不到条目即拒绝（"Your privileges are insufficient"）。
// 因此面板在实例启动前直接创建/更新管理员条目：完整权限 + 面板管理的密码。
package luanti

import (
	"crypto/rand"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// AdminPrivileges 是面板为管理员写入的标准权限集（引擎内置 + 常用管理项）。
var AdminPrivileges = []string{
	"interact", "shout", "basic_privs", "privs", "server",
	"teleport", "bring", "settime", "give", "kick", "ban", "password",
	"fast", "fly", "noclip", "zoom", "creative",
}

// LegacyPasswordHash 与引擎 src/util/auth.cpp translate_password 一致：
// base64(SHA1(玩家名 + 密码))。旧版格式，服务端与客户端均兼容。
func LegacyPasswordHash(name, password string) string {
	if password == "" {
		return ""
	}
	sum := sha1.Sum([]byte(name + password))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// RandomPassword 生成 10 位字母数字密码。
func RandomPassword() string {
	const cs = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, 10)
	rand.Read(b)
	for i := range b {
		b[i] = cs[int(b[i])%len(cs)]
	}
	return string(b)
}

func openAuthDB(worldDir string, create bool) (*sql.DB, string, error) {
	dbPath := filepath.Join(worldDir, "auth.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		if !create {
			return nil, "", err
		}
		// 引擎尚未创建过：按引擎 schema 建库
		db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?_pragma=busy_timeout(4000)")
		if err != nil {
			return nil, "", err
		}
		schema := `
CREATE TABLE IF NOT EXISTS auth (id INTEGER PRIMARY KEY AUTOINCREMENT, name VARCHAR(32) NOT NULL UNIQUE, password TEXT, last_login INTEGER);
CREATE TABLE IF NOT EXISTS user_privileges (id INTEGER, privilege TEXT);
CREATE INDEX IF NOT EXISTS user_privileges_id ON user_privileges (id);`
		if _, err := db.Exec(schema); err != nil {
			db.Close()
			return nil, "", err
		}
		return db, dbPath, nil
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?_pragma=busy_timeout(4000)")
	if err != nil {
		return nil, "", err
	}
	return db, dbPath, nil
}

// EnsureAdminEntry 在实例启动前调用：管理员条目不存在则以给定密码创建
// （完整权限）；已存在则仅补齐权限（不动玩家已改过的密码）。
// 返回是否新创建了条目。
func EnsureAdminEntry(worldDir, adminName, password string) (bool, error) {
	if adminName == "" || !IsValidPlayerName(adminName) {
		return false, nil
	}
	db, _, err := openAuthDB(worldDir, true)
	if err != nil {
		return false, err
	}
	defer db.Close()

	var id int
	err = db.QueryRow(`SELECT id FROM auth WHERE name = ?`, adminName).Scan(&id)
	if err == sql.ErrNoRows {
		tx, err := db.Begin()
		if err != nil {
			return false, err
		}
		defer tx.Rollback()
		res, err := tx.Exec(`INSERT INTO auth(name, password, last_login) VALUES(?, ?, -1)`,
			adminName, LegacyPasswordHash(adminName, password))
		if err != nil {
			return false, err
		}
		newID, _ := res.LastInsertId()
		for _, p := range AdminPrivileges {
			if _, err := tx.Exec(`INSERT INTO user_privileges(id, privilege) VALUES(?, ?)`, newID, p); err != nil {
				return false, err
			}
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	// 已有条目：补齐权限（幂等）
	return false, syncPrivs(db, id)
}

// SetAdminPassword 更新管理员密码（条目不存在时创建）。
func SetAdminPassword(worldDir, adminName, password string) error {
	if adminName == "" || !IsValidPlayerName(adminName) {
		return os.ErrInvalid
	}
	db, _, err := openAuthDB(worldDir, true)
	if err != nil {
		return err
	}
	defer db.Close()

	var id int
	err = db.QueryRow(`SELECT id FROM auth WHERE name = ?`, adminName).Scan(&id)
	if err == sql.ErrNoRows {
		if password == "" {
			return nil
		}
		_, err = db.Exec(`INSERT INTO auth(name, password, last_login) VALUES(?, ?, -1)`,
			adminName, LegacyPasswordHash(adminName, password))
		return err
	}
	if err != nil {
		return err
	}
	if password == "" {
		_, err = db.Exec(`UPDATE auth SET password = '' WHERE id = ?`, id)
	} else {
		_, err = db.Exec(`UPDATE auth SET password = ? WHERE id = ?`, LegacyPasswordHash(adminName, password), id)
	}
	return err
}

func syncPrivs(db *sql.DB, id int) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_privileges WHERE id = ? AND privilege = 'privs'`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil // 已是完整权限
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM user_privileges WHERE id = ?`, id); err != nil {
		return err
	}
	for _, p := range AdminPrivileges {
		if _, err := tx.Exec(`INSERT INTO user_privileges(id, privilege) VALUES(?, ?)`, id, p); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AdminAuthInfo 供 UI 展示：条目是否存在、是否已同步完整权限。
type AdminAuthInfo struct {
	Exists   bool `json:"exists"`
	FullPriv bool `json:"full_priv"`
}

// AdminStatus 读取管理员条目状态。
func AdminStatus(worldDir, adminName string) AdminAuthInfo {
	var info AdminAuthInfo
	if adminName == "" {
		return info
	}
	db, _, err := openAuthDB(worldDir, false)
	if err != nil {
		return info
	}
	defer db.Close()
	var id int
	if err := db.QueryRow(`SELECT id FROM auth WHERE name = ?`, adminName).Scan(&id); err != nil {
		return info
	}
	info.Exists = true
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_privileges WHERE id = ? AND privilege = 'privs'`, id).Scan(&n); err == nil {
		info.FullPriv = n > 0
	}
	return info
}

var _ = hex.EncodeToString
var _ = strings.TrimSpace
