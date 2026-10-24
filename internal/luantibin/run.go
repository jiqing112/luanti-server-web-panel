package luantibin

import (
	"context"
	"os/exec"
	"time"
)

// runCmd 运行 <path> --version（限时 10 秒）。
func runCmd(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	out, err := cmd.Output()
	if err != nil {
		// 有些版本把 --version 打到 stderr
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return string(ee.Stderr), nil
		}
		return "", err
	}
	return string(out), nil
}
