package api

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"luantipanel/internal/auth"
	"luantipanel/internal/backup"
	"luantipanel/internal/luanti"
	"luantipanel/internal/store"
)

// ---- 配置读写 ----

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	d := s.RT.Dirs(i.ID)
	content, err := luanti.ReadConfFile(d.Config())
	if err != nil {
		writeErr(w, 404, "配置文件不存在")
		return
	}
	_, kv, err := luanti.ParseConf(content)
	if err != nil {
		// 解析失败也返回原文，由前端提示
		kv = map[string]string{}
	}
	writeJSON(w, 200, map[string]any{"content": content, "values": kv})
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	d := s.RT.Dirs(i.ID)
	var body struct {
		Content *string           `json:"content"`
		Values  map[string]string `json:"values"`
	}
	if err := readBody(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	current, err := luanti.ReadConfFile(d.Config())
	if err != nil {
		current = ""
	}

	var newContent string
	if body.Content != nil {
		// 原始模式：逐行校验
		if _, _, err := luanti.ParseConf(*body.Content); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		newContent = *body.Content
	} else if len(body.Values) > 0 {
		// 表单模式：类型校验 + 合并
		schema, _ := s.st.ListSchema()
		schemaMap := map[string]*store.SchemaRow{}
		for _, row := range schema {
			schemaMap[row.Key] = row
		}
		for k, v := range body.Values {
			// key 必须是合法配置键：否则写入后 ParseConf 会拒绝整个文件，
			// 造成后续配置读写连锁故障
			if !luanti.IsValidConfKey(k) {
				writeErr(w, 400, fmt.Sprintf("不合法的配置键: %s", k))
				return
			}
			if row, ok := schemaMap[k]; ok {
				if err := luanti.ValidateValue(row.Type, v, row.EnumValues); err != nil {
					writeErr(w, 400, fmt.Sprintf("%s: %v", k, err))
					return
				}
			} else if strings.ContainsAny(v, "\r\n") {
				writeErr(w, 400, fmt.Sprintf("%s: 不允许换行", k))
				return
			}
		}
		var err error
		newContent, err = luanti.MergeValues(current, body.Values)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
	} else {
		writeErr(w, 400, "缺少 content 或 values")
		return
	}

	// 保存前自动备份
	if name, err := backup.SnapshotConf(d); err == nil && name != "" {
		backup.RotateDir(d.BackupsConfig(), 30, ".conf")
	}
	if err := luanti.WriteConfFile(d.Config(), newContent); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "config.save", i.ID, "")
	writeJSON(w, 200, map[string]any{"ok": true, "hint": "配置已保存，重启实例后生效"})
}

// ---- schema ----

func (s *Server) handleConfigSchema(w http.ResponseWriter, r *http.Request) {
	rows, err := s.st.ListSchema()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if len(rows) == 0 {
		writeErr(w, 404, "配置 schema 为空：请先在设置页下载 Luanti 二进制（内含 minetest.conf.example），或刷新 schema")
		return
	}
	writeJSON(w, 200, rows)
}

// ---- 配置历史 / diff / 回滚 ----

func (s *Server) handleConfigHistory(w http.ResponseWriter, r *http.Request) {
	d := s.RT.Dirs(inst(r).ID)
	writeJSON(w, 200, backup.ListConfigHistory(d))
}

func (s *Server) handleConfigDiff(w http.ResponseWriter, r *http.Request) {
	d := s.RT.Dirs(inst(r).ID)
	q := r.URL.Query()
	a, b := q.Get("a"), q.Get("b")

	load := func(name string) ([]string, error) {
		if name == "current" {
			content, err := luanti.ReadConfFile(d.Config())
			if err != nil {
				return nil, err
			}
			return strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n"), nil
		}
		p, err := backup.ConfigHistoryPath(d, name)
		if err != nil {
			return nil, err
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		return strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n"), nil
	}

	la, err := load(a)
	if err != nil {
		writeErr(w, 400, "a: "+err.Error())
		return
	}
	lb, err := load(b)
	if err != nil {
		writeErr(w, 400, "b: "+err.Error())
		return
	}
	writeJSON(w, 200, diffLines(la, lb))
}

// diffLines 简单 LCS 行对比。
func diffLines(a, b []string) []map[string]any {
	type op struct {
		kind string // same | del | add
		line string
		no   int
	}
	n, m := len(a), len(b)
	// LCS DP（限制规模防止内存爆炸）
	if n*m > 4_000_000 {
		// 退化：全量标删/增
		var out []map[string]any
		for i, l := range a {
			out = append(out, map[string]any{"kind": "del", "line": l, "no": i + 1})
		}
		for i, l := range b {
			out = append(out, map[string]any{"kind": "add", "line": l, "no": i + 1})
		}
		return out
	}
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{"same", a[i], i + 1})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, op{"del", a[i], i + 1})
			i++
		default:
			ops = append(ops, op{"add", b[j], j + 1})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{"del", a[i], i + 1})
	}
	for ; j < m; j++ {
		ops = append(ops, op{"add", b[j], j + 1})
	}

	// 压缩 same 行（上下文保留 2 行）
	out := make([]map[string]any, 0, len(ops))
	lastKept := -1
	for idx, o := range ops {
		if o.kind != "same" {
			for k := max(lastKept+1, idx-2); k < idx; k++ {
				out = append(out, map[string]any{"kind": "same", "line": ops[k].line, "no": ops[k].no})
			}
			out = append(out, map[string]any{"kind": o.kind, "line": o.line, "no": o.no})
			lastKept = idx
		}
	}
	if len(out) == 0 {
		out = append(out, map[string]any{"kind": "same", "line": "（两个版本内容相同）", "no": 0})
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *Server) handleConfigRestore(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	d := s.RT.Dirs(i.ID)
	var body struct {
		Name string `json:"name"`
	}
	if err := readBody(r, &body); err != nil || body.Name == "" {
		writeErr(w, 400, "缺少 name")
		return
	}
	p, err := backup.ConfigHistoryPath(d, body.Name)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	// 回滚前先备份当前，并同步轮转历史
	if _, err := backup.SnapshotConf(d); err == nil {
		backup.RotateDir(d.BackupsConfig(), 30, ".conf")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := luanti.WriteConfFile(d.Config(), string(b)); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "config.restore", i.ID, "回滚到 "+body.Name)
	writeJSON(w, 200, map[string]any{"ok": true, "hint": "已回滚，重启实例后生效"})
}
