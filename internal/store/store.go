// Package store: SQLite 持久层（modernc.org/sqlite 纯 Go 驱动）。
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB

	initialAdminPassword string // 仅首次创建管理员时非空
}

func Open(path string) (*Store, error) {
	dsn := "file:" + strings.ReplaceAll(path, "\\", "/") +
		"?_pragma=busy_timeout(8000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite 单写者，串行化避免 BUSY
	if err := db.Ping(); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS instances (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	port INTEGER NOT NULL,
	gameid TEXT NOT NULL,
	worldname TEXT NOT NULL,
	admin_name TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'stopped',
	auto_restart INTEGER NOT NULL DEFAULT 1,
	backup_interval_hours INTEGER NOT NULL DEFAULT 0,
	backup_keep_count INTEGER NOT NULL DEFAULT 10,
	bridge_token TEXT NOT NULL DEFAULT '',
	last_exit_code INTEGER,
	last_started_at TEXT,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT UNIQUE NOT NULL,
	password_hash TEXT NOT NULL,
	role TEXT NOT NULL DEFAULT 'operator',
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	token TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL,
	expires_at TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS audit_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	admin_user TEXT,
	action TEXT,
	instance_id TEXT,
	detail TEXT,
	created_at TEXT
);
CREATE TABLE IF NOT EXISTS player_activity_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	instance_id TEXT,
	player_name TEXT,
	activity_type TEXT,
	content TEXT,
	created_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_activity_inst_time ON player_activity_log(instance_id, created_at);
CREATE INDEX IF NOT EXISTS idx_activity_player ON player_activity_log(instance_id, player_name);
CREATE TABLE IF NOT EXISTS config_schema (
	key TEXT PRIMARY KEY,
	type TEXT,
	default_value TEXT,
	description TEXT,
	category TEXT,
	enum_values TEXT
);
`)
	return err
}

func now() string { return time.Now().Format(time.RFC3339) }

func RandomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------- 用户 / 会话 ----------

// InitAdmin 创建默认管理员（仅当没有任何用户时）。
func (s *Store) InitAdmin() error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	pw := RandomToken(6) // 12 个十六进制字符，可读性更好
	hash, err := HashPassword(pw)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`INSERT INTO users(username, password_hash, role, created_at) VALUES(?,?,?,?)`,
		"admin", hash, "admin", now()); err != nil {
		return err
	}
	s.initialAdminPassword = pw
	return nil
}

func (s *Store) InitialAdminPassword() string { return s.initialAdminPassword }

type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt)
	return &u, err
}

func (s *Store) GetUserByName(username string) (*User, string, error) {
	var u User
	var hash string
	err := s.db.QueryRow(`SELECT id, username, role, created_at, password_hash FROM users WHERE username=?`, username).
		Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt, &hash)
	if err != nil {
		return nil, "", err
	}
	return &u, hash, nil
}

func (s *Store) GetUserByID(id int64) (*User, error) {
	row := s.db.QueryRow(`SELECT id, username, role, created_at FROM users WHERE id=?`, id)
	return scanUser(row)
}

func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(`SELECT id, username, role, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) CreateUser(username, hash, role string) error {
	_, err := s.db.Exec(`INSERT INTO users(username, password_hash, role, created_at) VALUES(?,?,?,?)`,
		username, hash, role, now())
	return err
}

func (s *Store) UpdateUserPassword(id int64, hash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, id)
	return err
}

func (s *Store) UpdateUserRole(id int64, role string) error {
	_, err := s.db.Exec(`UPDATE users SET role=? WHERE id=?`, role, id)
	return err
}

func (s *Store) DeleteUser(id int64) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id)
	return err
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateSession(token string, userID int64, ttl time.Duration) error {
	_, err := s.db.Exec(`INSERT INTO sessions(token, user_id, expires_at, created_at) VALUES(?,?,?,?)`,
		token, userID, time.Now().Add(ttl).Format(time.RFC3339), now())
	return err
}

func (s *Store) GetSession(token string) (int64, error) {
	var uid int64
	var exp string
	err := s.db.QueryRow(`SELECT user_id, expires_at FROM sessions WHERE token=?`, token).Scan(&uid, &exp)
	if err != nil {
		return 0, err
	}
	t, err := time.Parse(time.RFC3339, exp)
	if err != nil || time.Now().After(t) {
		_ = s.DeleteSession(token)
		return 0, sql.ErrNoRows
	}
	return uid, nil
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token=?`, token)
	return err
}

func (s *Store) CleanSessions() error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now())
	return err
}

// ---------- 实例 ----------

type Instance struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Port                int    `json:"port"`
	GameID              string `json:"gameid"`
	WorldName           string `json:"worldname"`
	AdminName           string `json:"admin_name"`
	Status              string `json:"status"`
	AutoRestart         bool   `json:"auto_restart"`
	BackupIntervalHours int    `json:"backup_interval_hours"`
	BackupKeepCount     int    `json:"backup_keep_count"`
	BridgeToken         string `json:"-"`
	LastExitCode        *int   `json:"last_exit_code"`
	LastStartedAt       string `json:"last_started_at"`
	CreatedAt           string `json:"created_at"`
}

const instanceCols = `id, name, port, gameid, worldname, admin_name, status, auto_restart,
	backup_interval_hours, backup_keep_count, bridge_token, last_exit_code, last_started_at, created_at`

func scanInstance(row interface{ Scan(...any) error }) (*Instance, error) {
	var i Instance
	var ar, bih, bkc int
	err := row.Scan(&i.ID, &i.Name, &i.Port, &i.GameID, &i.WorldName, &i.AdminName, &i.Status,
		&ar, &bih, &bkc, &i.BridgeToken, &i.LastExitCode, &i.LastStartedAt, &i.CreatedAt)
	if err != nil {
		return nil, err
	}
	i.AutoRestart = ar == 1
	i.BackupIntervalHours = bih
	i.BackupKeepCount = bkc
	return &i, nil
}

func (s *Store) ListInstances() ([]*Instance, error) {
	rows, err := s.db.Query(`SELECT ` + instanceCols + ` FROM instances ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Instance
	for rows.Next() {
		i, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) GetInstance(id string) (*Instance, error) {
	row := s.db.QueryRow(`SELECT `+instanceCols+` FROM instances WHERE id=?`, id)
	return scanInstance(row)
}

func (s *Store) GetInstanceByBridgeToken(token string) (*Instance, error) {
	row := s.db.QueryRow(`SELECT `+instanceCols+` FROM instances WHERE bridge_token=?`, token)
	return scanInstance(row)
}

func (s *Store) CreateInstance(i *Instance) error {
	ar := 0
	if i.AutoRestart {
		ar = 1
	}
	if i.BridgeToken == "" {
		i.BridgeToken = RandomToken(24)
	}
	if i.Status == "" {
		i.Status = "stopped"
	}
	_, err := s.db.Exec(`INSERT INTO instances(`+instanceCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		i.ID, i.Name, i.Port, i.GameID, i.WorldName, i.AdminName, i.Status, ar,
		i.BackupIntervalHours, i.BackupKeepCount, i.BridgeToken, i.LastExitCode, i.LastStartedAt, now())
	return err
}

// InstanceUpdate 描述允许通过 API 修改的字段（指针为 nil 表示不改）。
type InstanceUpdate struct {
	Name                *string
	AutoRestart         *bool
	BackupIntervalHours *int
	BackupKeepCount     *int
	Status              *string
	LastExitCode        *int
	LastStartedAt       *string
}

func (s *Store) UpdateInstance(id string, u *InstanceUpdate) error {
	sets, args := []string{}, []any{}
	if u.Name != nil {
		sets, args = append(sets, "name=?"), append(args, *u.Name)
	}
	if u.AutoRestart != nil {
		v := 0
		if *u.AutoRestart {
			v = 1
		}
		sets, args = append(sets, "auto_restart=?"), append(args, v)
	}
	if u.BackupIntervalHours != nil {
		sets, args = append(sets, "backup_interval_hours=?"), append(args, *u.BackupIntervalHours)
	}
	if u.BackupKeepCount != nil {
		sets, args = append(sets, "backup_keep_count=?"), append(args, *u.BackupKeepCount)
	}
	if u.Status != nil {
		sets, args = append(sets, "status=?"), append(args, *u.Status)
	}
	if u.LastExitCode != nil {
		sets, args = append(sets, "last_exit_code=?"), append(args, *u.LastExitCode)
	}
	if u.LastStartedAt != nil {
		sets, args = append(sets, "last_started_at=?"), append(args, *u.LastStartedAt)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := s.db.Exec(`UPDATE instances SET `+strings.Join(sets, ", ")+` WHERE id=?`, args...)
	return err
}

func (s *Store) DeleteInstance(id string) error {
	_, err := s.db.Exec(`DELETE FROM instances WHERE id=?`, id)
	return err
}

func (s *Store) PortInUseByOtherInstance(port int, excludeID string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM instances WHERE port=? AND id<>?`, port, excludeID).Scan(&n)
	return n > 0, err
}

// ---------- 设置 ----------

func (s *Store) GetSetting(key, def string) string {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err != nil {
		return def
	}
	return v
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key, value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) AllSettings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// ---------- 审计日志 ----------

func (s *Store) Audit(adminUser, action, instanceID, detail string) {
	_, _ = s.db.Exec(`INSERT INTO audit_log(admin_user, action, instance_id, detail, created_at) VALUES(?,?,?,?,?)`,
		adminUser, action, instanceID, detail, now())
}

type AuditRow struct {
	ID         int64  `json:"id"`
	AdminUser  string `json:"admin_user"`
	Action     string `json:"action"`
	InstanceID string `json:"instance_id"`
	Detail     string `json:"detail"`
	CreatedAt  string `json:"created_at"`
}

func (s *Store) ListAudit(instanceID string, limit, offset int) ([]*AuditRow, int, error) {
	where, args := "", []any{}
	if instanceID != "" {
		where = " WHERE instance_id=?"
		args = append(args, instanceID)
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_log`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(`SELECT id, admin_user, action, instance_id, detail, created_at
		FROM audit_log`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*AuditRow
	for rows.Next() {
		r := &AuditRow{}
		if err := rows.Scan(&r.ID, &r.AdminUser, &r.Action, &r.InstanceID, &r.Detail, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// PruneAudit 保留最近 maxRows 条。
func (s *Store) PruneAudit(maxRows int) error {
	_, err := s.db.Exec(`DELETE FROM audit_log WHERE id <= (SELECT MAX(id) FROM audit_log) - ?`, maxRows)
	return err
}

// ---------- 玩家活动日志 ----------

type ActivityRow struct {
	ID         int64  `json:"id"`
	PlayerName string `json:"player_name"`
	Type       string `json:"activity_type"`
	Content    string `json:"content"`
	CreatedAt  string `json:"created_at"`
}

// InsertActivities 批量写入活动记录。
func (s *Store) InsertActivities(instID string, rows []ActivityRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO player_activity_log(instance_id, player_name, activity_type, content, created_at) VALUES(?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		ts := r.CreatedAt
		if ts == "" {
			// 与日志时间戳同格式（空格分隔），保证 created_at 排序一致
			ts = time.Now().Format("2006-01-02 15:04:05")
		}
		if _, err := stmt.Exec(instID, r.PlayerName, r.Type, r.Content, ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListActivity(instID, player, typ, from, to string, limit, offset int) ([]*ActivityRow, int, error) {
	where := " WHERE instance_id=?"
	args := []any{instID}
	if player != "" {
		where += " AND player_name LIKE ?"
		args = append(args, "%"+player+"%")
	}
	if typ != "" {
		where += " AND activity_type=?"
		args = append(args, typ)
	}
	if from != "" {
		where += " AND created_at >= ?"
		args = append(args, from)
	}
	if to != "" {
		where += " AND created_at <= ?"
		args = append(args, to+"~") // RFC3339 前缀匹配到当天末尾
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM player_activity_log`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(`SELECT id, player_name, activity_type, content, created_at
		FROM player_activity_log`+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*ActivityRow
	for rows.Next() {
		r := &ActivityRow{}
		if err := rows.Scan(&r.ID, &r.PlayerName, &r.Type, &r.Content, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// ExistsActivity 判断活动事件是否已存在（历史回放去重用）。
func (s *Store) ExistsActivity(instID, player, typ, content, ts string) bool {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM player_activity_log
		WHERE instance_id=? AND player_name=? AND activity_type=? AND content IS ? AND created_at=?`,
		instID, player, typ, content, ts).Scan(&n)
	return err == nil && n > 0
}

func (s *Store) PruneActivity(days int) error {
	cut := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	_, err := s.db.Exec(`DELETE FROM player_activity_log WHERE created_at < ?`, cut)
	return err
}

// ---------- 配置 schema ----------

type SchemaRow struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Default     string   `json:"default_value"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	EnumValues  []string `json:"enum_values,omitempty"`
}

func (s *Store) ReplaceSchema(rows []SchemaRow) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM config_schema`); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO config_schema(key, type, default_value, description, category, enum_values) VALUES(?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.Exec(r.Key, r.Type, r.Default, r.Description, r.Category, strings.Join(r.EnumValues, "\n")); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListSchema() ([]*SchemaRow, error) {
	rows, err := s.db.Query(`SELECT key, type, default_value, description, category, enum_values FROM config_schema ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SchemaRow
	for rows.Next() {
		var r SchemaRow
		var enums string
		if err := rows.Scan(&r.Key, &r.Type, &r.Default, &r.Description, &r.Category, &enums); err != nil {
			return nil, err
		}
		if enums != "" {
			r.EnumValues = strings.Split(enums, "\n")
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

var ErrNotFound = errors.New("not found")

func (s *Store) IsNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func (s *Store) fmtErr(op string, err error) error { return fmt.Errorf("%s: %w", op, err) }
