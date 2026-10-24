// mock-luanti：模拟 Luanti 无头服务端，用于面板端到端测试（非生产代码）。
// 行为：
//   - 按 luantiserver 的 CLI 约定接受 --config/--gameid/--world/--port/--logfile
//   - 从配置文件读取 panel_bridge.url / panel_bridge.token，像真实 bridge mod 一样
//     每 2 秒轮询面板、上报玩家、执行命令（status/list/grant/revoke/privs/kick/shutdown…）
//   - 按真实日志格式输出 join/leave/CHAT 行
//   - --crash-test 参数：随机秒后以非零码退出（测试自动重启）
package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"net/http"
	"bytes"
	"os"
	"strings"
	"time"
)

var (
	panelURL string
	token    string
	port     string
	world    string
)

var players = map[string]time.Time{}

var names = []string{"Steve", "Alex", "Herobrine", "xiaoming", "testcat", "builder99"}

func logf(format string, a ...any) {
	fmt.Printf("%s: %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, a...))
}

func parseConf(path string) map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

func playerList() []string {
	out := make([]string, 0, len(players))
	for n := range players {
		out = append(out, n)
	}
	return out
}

func execCommand(cmdline string) string {
	cmdline = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cmdline), "/"))
	cmd, params, _ := strings.Cut(cmdline, " ")
	params = strings.TrimSpace(params)
	switch cmd {
	case "status":
		return fmt.Sprintf("status: mock server | players: %d/%d | uptime ok", len(players), 20)
	case "list":
		return "Online players: " + strings.Join(playerList(), ", ")
	case "privs":
		return fmt.Sprintf("Privileges of %s: interact shout fast fly", params)
	case "grant":
		return "Granted privilege"
	case "revoke":
		return "Revoked privilege"
	case "setpassword":
		return "Password set"
	case "clearpassword":
		return "Password cleared"
	case "last-login":
		return "Last login was 2026-10-01 12:00:00"
	case "kick":
		return fmt.Sprintf("Kicked %s", params)
	case "save":
		logf("Server: Saving map...")
		return "Saved"
	case "shutdown":
		logf("Server: Shutting down")
		go func() {
			time.Sleep(300 * time.Millisecond)
			os.Exit(0)
		}()
		return "Shutting down"
	default:
		return "Unknown command: " + cmd
	}
}

func heartbeat() {
	for {
		body, _ := jsonMarshal(map[string]any{
			"players": playerList(),
			"time":    time.Now().Unix(),
		})
		req, _ := http.NewRequest("POST", panelURL+"/api/bridge/poll", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Bridge-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			var cmds struct {
				Commands []struct {
					ID   string `json:"id"`
					Text string `json:"text"`
				} `json:"commands"`
			}
			decodeJSON(resp, &cmds)
			resp.Body.Close()
			for _, c := range cmds.Commands {
				logf("ACTION[Main]: [panel_bridge] 执行命令: %s", c.Text)
				out := execCommand(c.Text)
				rb, _ := jsonMarshal(map[string]any{"id": c.ID, "output": out})
				req2, _ := http.NewRequest("POST", panelURL+"/api/bridge/result", bytes.NewReader(rb))
				req2.Header.Set("Content-Type", "application/json")
				req2.Header.Set("X-Bridge-Token", token)
				r2, err := http.DefaultClient.Do(req2)
				if err == nil {
					r2.Body.Close()
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
}

func simulation() {
	// 模拟玩家进出与聊天
	go func() {
		for {
			time.Sleep(time.Duration(4+rand.Intn(6)) * time.Second)
			if len(players) < 4 {
				n := names[rand.Intn(len(names))]
				if _, ok := players[n]; !ok {
					players[n] = time.Now()
					logf("ACTION[Main]: %s [127.0.0.1:%d] joins game. List of players: %s",
						n, 40000+rand.Intn(1000), strings.Join(playerList(), " "))
				}
			}
		}
	}()
	go func() {
		for {
			time.Sleep(time.Duration(6+rand.Intn(8)) * time.Second)
			if len(players) > 0 {
				var who string
				for n := range players {
					who = n
					break
				}
				if who == "" {
					continue
				}
				delete(players, who)
				logf("ACTION[Main]: %s leaves game. List of players: %s", who, strings.Join(playerList(), " "))
			}
		}
	}()
	go func() {
		for {
			time.Sleep(time.Duration(5+rand.Intn(9)) * time.Second)
			for n := range players {
				msgs := []string{"大家好", "有人一起去挖矿吗", "/status", "今天天气不错", "help!"}
				m := msgs[rand.Intn(len(msgs))]
				logf("ACTION[Main]: CHAT: <%s> %s", n, m)
				break
			}
		}
	}()
}

func main() {
	args := os.Args[1:]
	crashTest := false
	for i, a := range args {
		switch a {
		case "--version":
			fmt.Println("Luanti 5.17.0 (mock-server)")
			os.Exit(0)
		case "--config":
			if i+1 < len(args) {
				conf := parseConf(args[i+1])
				panelURL = conf["panel_bridge.url"]
				token = conf["panel_bridge.token"]
			}
		case "--port":
			if i+1 < len(args) {
				port = args[i+1]
			}
		case "--world":
			if i+1 < len(args) {
				world = args[i+1]
			}
		case "--crash-test":
			crashTest = true
		}
	}

	fmt.Printf("Luanti 5.17.0 (mock-server)\n")
	logf("ACTION[Main]: World at [%s]", world)
	logf("ACTION[Main]: Server for gameid=\"mockgame\" listening on port %s.", port)
	logf("ACTION[Server]: Server listening on 0.0.0.0:%s.", port)
	logf("ACTION[Main]: [panel_bridge] 已启动 (url=%s)", panelURL)

	if crashTest {
		n := 3 + rand.Intn(4)
		logf("WARNING: mock crash test: exiting with code 1 in %d seconds", n)
		time.Sleep(time.Duration(n) * time.Second)
		fmt.Fprintf(os.Stderr, "%s: ERROR[Main]: mock crash!\n", time.Now().Format("2006-01-02 15:04:05"))
		os.Exit(1)
	}

	if panelURL != "" && token != "" {
		go heartbeat()
	}
	simulation()

	// 读 stdin（尽管真实 Luanti 不读，这里保持行为简单）
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			logf("ACTION[Main]: CHAT: <mock-stdin> %s", line)
		}
	}
	select {} // 阻塞直到被杀
}
