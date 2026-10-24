package luanti

// IsValidConfKey 导出的配置键校验（供 API 层在写入前拦截非法键）。
// 规则与 conf.go 内部的 isConfKey 一致；conf.go 因静态检查对
// BaseConf 中桥接键名模板的误报无法就地修改，故在此单独导出。
func IsValidConfKey(k string) bool {
	if k == "" {
		return false
	}
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '.' || r == '-':
		default:
			return false
		}
	}
	return true
}
