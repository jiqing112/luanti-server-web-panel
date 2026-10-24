package store

import "time"

// PruneActivityByDays 保留最近 days 天的活动记录。
//
// 修复说明：player_activity_log.created_at 与日志时间戳格式一致
// （"2006-01-02 15:04:05"，空格分隔）；旧实现的清理阈值用 RFC3339（带 "T"），
// 字符串比较中 ' ' < 'T'，导致边界日当天的记录整日被误判为过期而多删。
// 此处阈值改用与存储一致的格式。旧 PruneActivity 请删除（已无调用方）。
func (s *Store) PruneActivityByDays(days int) error {
	cut := time.Now().AddDate(0, 0, -days).Format("2006-01-02 15:04:05")
	_, err := s.db.Exec(`DELETE FROM player_activity_log WHERE created_at < ?`, cut)
	return err
}
