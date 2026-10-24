# Luanti Web Panel

**Luanti（原 Minetest）服务端 Web 管理面板** — Go + Svelte 5，单二进制部署，前端内嵌。
在浏览器里完成多实例的全生命周期管理：创建、配置、Mod/游戏安装、进程控制、实时日志、玩家管理、备份恢复与文件浏览。

[简体中文](#简体中文) · [English](#english) · [日本語](#日本語)

![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)
![Svelte](https://img.shields.io/badge/Svelte-5-FF3E00?logo=svelte&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-green)
![release](https://img.shields.io/github/v/release/jiqing112/luanti-server-web-panel?display_name=tag)
![release build](https://img.shields.io/github/actions/workflow/status/jiqing112/luanti-server-web-panel/release.yml?label=release)

---

## 简体中文

### 这是什么

Luanti 专用服务器没有官方管理界面。本项目提供一个自托管的 Web 面板：为每个实例生成
RUN_IN_PLACE 目录、托管进程、抓取日志、解析玩家活动，并通过随世界自动加载的
`panel_bridge` mod 实现控制台命令与在线玩家上报。

> **为什么不是 stdin？** 经源码核实，Luanti 5.x 专用服务器主循环**不读取 stdin**
> （`dedicated_server_loop` 无输入处理，`--terminal` 是 ncurses TUI 不适用管道）。
> 因此控制台命令走 bridge mod：每 2 秒向面板轮询、以 `minetest.conf` 的 `name`
> （管理员，引擎授予全部权限）身份执行 `core.registered_chatcommands`，并回传输出。

### 功能

| 模块 | 说明 |
|---|---|
| 实例管理 | 多实例隔离（独立目录/端口/世界），端口冲突检测，克隆实例 |
| 进程控制 | 启动/停止/重启；停止优先经 bridge `/shutdown` 优雅存盘 → SIGTERM 进程组 → SIGKILL；异常自动重启（连续崩溃 5 次熔断 + webhook 告警） |
| 实时日志 | WebSocket 推送（stdout/stderr 双管道、无行长限制、退出前尾部日志不丢），断线自动重连 |
| Web 控制台 | 命令历史、快捷按钮，经 bridge 以管理员身份执行并实时回显 |
| 配置管理 | 解析 `minetest.conf.example` 生成动态表单 + 原始编辑；保存前自动快照、历史 diff、一键回滚 |
| Mod / 游戏 | ContentDB 搜索与一键安装、硬依赖递归解析、`world.mt` 启用/禁用、全局游戏库与更新检查 |
| 玩家 | 在线列表（bridge 心跳 + 日志双通道）、踢出、权限开关、密码重置、管理员账户托管 |
| 活动审计 | 聊天/命令/进出/放置/挖掘入库，按玩家与时间筛选分页 |
| 方块审计 | 读取 blockwatcher 的方块日志（JSON 与逐日 txt 两种格式） |
| 备份恢复 | 手动/定时 zip 备份（运行中先 `/save`）、保留轮转、单份锁定、恢复（自动停服）、下载 |
| 文件浏览 | 树形浏览、在线编辑文本文件、上传下载、`../` 与符号链接逃逸防护 |
| 用户认证 | bcrypt + Cookie 会话，admin/operator 双角色，登录限速（仅内网反代头采信），操作审计 |
| 资源监控 | gopsutil 采样 CPU/内存，WebSocket 推送 + 迷你曲线 |

### 环境要求

- **Linux**（推荐生产）：x86_64 / aarch64，systemd
- **Go 1.26+**（`go.mod` 要求；`CGO_ENABLED=0` 可静态编译）
- **Node.js 18+**（仅构建前端时需要；仓库已附带构建好的 `web/dist`）
- 出网访问 `api.github.com` 与 `content.luanti.org`（下载服务端二进制 / 模组；不可达时可手动放置）

### 从 Release 部署（推荐，开箱即用）

每个 Release 的 Linux 包**已内置对应架构的 Luanti 无头服务端**（来自
`ROllerozxa/luantiserver`，含 `builtin/` 运行时与 `minetest.conf.example`），
解压即用，无需联网下载二进制。

```bash
# 1) 下载并校验（1.0.0 换成 Releases 页显示的最新版本）
VER=1.0.0
BASE=https://github.com/jiqing112/luanti-server-web-panel/releases/download/v${VER}
curl -fLO ${BASE}/luanti-panel_${VER}_linux_amd64.tar.gz   # arm64 机器用 _linux_arm64
curl -fLO ${BASE}/SHA256SUMS.txt
sha256sum -c SHA256SUMS.txt --ignore-missing

# 2) 解压安装（包内结构：panel + data/bin 内置服务端 + README/LICENSE）
sudo useradd -r -m -d /opt/luanti-panel -s /usr/sbin/nologin luanti || true
sudo mkdir -p /opt/luanti-panel
sudo tar -xzf luanti-panel_${VER}_linux_amd64.tar.gz -C /opt/luanti-panel --strip-components=1
sudo chown -R luanti:luanti /opt/luanti-panel

# 3) 首次运行（直接用包内 data/，服务端二进制已在 data/bin）
cd /opt/luanti-panel
sudo -u luanti ./panel -addr 127.0.0.1:8080 -data ./data
```

首次启动打印初始管理员密码（同时写入 `data/initial_admin_password.txt`）。登录后：
改密码 → **设置**页确认已识别内置二进制版本 → **刷新配置 schema** → 创建实例 →
防火墙放行实例的 UDP 端口 → 启动。

> 想把数据放到 `/var/lib`：`sudo cp -a data /var/lib/luanti-panel`，之后用
> `-data /var/lib/luanti-panel` 启动（`-data` 指向的目录必须包含 `bin/`）。
>
> **升级**：下载新 Release，覆盖 `panel` 与 `data/bin`，**保留** `panel.db` 与 `instances/`。
>
> **Windows 包只含 `panel.exe`**（上游不发布 Windows 服务端），用法见包内
> `README-Windows.txt` 与下文「Windows 部署现状与不足」。

### 从源码构建部署（详细步骤）

#### 1. 准备系统用户与目录

```bash
sudo useradd -r -m -d /opt/luanti-panel -s /usr/sbin/nologin luanti || true
sudo mkdir -p /opt/luanti-panel/bin /var/lib/luanti-panel
sudo chown -R luanti:luanti /opt/luanti-panel /var/lib/luanti-panel
```

> 面板需要**运行用户**对数据目录有写权限（实例目录、世界、备份都在其中）。

#### 2. 构建

```bash
git clone https://github.com/jiqing112/luanti-server-web-panel.git
cd luanti-server-web-panel

# 前端（已附带 dist，可跳过）
cd web && npm ci && npm run build && cd ..

# 后端：静态单二进制（modernc.org/sqlite 免 CGO）
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o panel ./cmd/panel
```

在其它机器上交叉编译 Linux 版：

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o panel-amd64 ./cmd/panel
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o panel-arm64 ./cmd/panel
```

#### 3. 安装并首次运行

```bash
sudo install -m 0755 panel /opt/luanti-panel/bin/panel
sudo -u luanti /opt/luanti-panel/bin/panel -addr 127.0.0.1:8080 -data /var/lib/luanti-panel
```

首次启动会创建管理员 `admin`，**初始密码**打印到控制台并写入
`/var/lib/luanti-panel/initial_admin_password.txt`（权限 0600）。登录后请立即修改。

#### 4. 注册为 systemd 服务

```ini
# /etc/systemd/system/luanti-panel.service
[Unit]
Description=Luanti Web Panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=luanti
Group=luanti
WorkingDirectory=/opt/luanti-panel
ExecStart=/opt/luanti-panel/bin/panel -addr 127.0.0.1:8080 -data /var/lib/luanti-panel
Restart=on-failure
RestartSec=3
LimitNOFILE=65535
# 面板退出时向各实例（独立进程组）发送 SIGTERM 优雅停服；
# 实例并行停止，单实例最坏约 60s（bridge /shutdown 6s + 等待退出 30s + 信号宽限 20s）
KillMode=control-group
TimeoutStopSec=120

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now luanti-panel
sudo journalctl -u luanti-panel -n 30 --no-pager   # 查看启动日志与初始密码
```

> `-addr 127.0.0.1:8080` 表示只监听本机，配合下文反代对外提供 HTTPS。
> 若不加反代直接对外，请改为 `:8080` 并自行评估风险（面板无内建 TLS）。

#### 5. 安装 Luanti 服务端二进制

登录面板 → **设置 → Luanti 服务端二进制**：

1. **立即下载**（推荐）：从 `ROllerozxa/luantiserver` 的 GitHub Releases 拉取匹配架构的
   预编译无头服务端，解压到 `/var/lib/luanti-panel/bin/luantiserver`；
2. **手动路径**：服务器上已有二进制时直接填写绝对路径（优先级最高）；
3. 点 **刷新配置 schema**（解析 `minetest.conf.example` 生成配置表单）。

> 每个实例启动时会把共享二进制与 `builtin/` 等运行时镜像到实例目录；
> 共享二进制更新后，实例在下次启动时自动替换（按大小与修改时间判断）。

#### 6. 创建实例与放行端口

**实例 → 创建实例**：填名称、UDP 端口（默认自动挑选）、世界名、**管理员玩家名**
（拥有全部权限，面板控制台以此身份执行命令），选择游戏（库里为空时可从 ContentDB 安装）。

```bash
# ufw
sudo ufw allow 30000:30100/udp     # 游戏端口（按实际分配）
sudo ufw allow 80,443/tcp          # 反代
sudo ufw reload

# firewalld
sudo firewall-cmd --permanent --add-port=30000-30100/udp
sudo firewall-cmd --permanent --add-service=https
sudo firewall-cmd --reload
```

> 云服务器还需在控制台安全组放行对应 **UDP** 端口。玩家连的是游戏端口，不是面板端口。

#### 7. 反向代理 + HTTPS

**Nginx**

```nginx
server {
    listen 443 ssl http2;
    server_name panel.example.com;

    ssl_certificate     /etc/letsencrypt/live/panel.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/panel.example.com/privkey.pem;

    client_max_body_size 100m;      # 文件上传上限（面板默认 100MB）

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # 日志流 WebSocket：/api/instances/<id>/logs
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600s;   # 日志空闲时不要被代理断开
    }
}
```

**Caddy**（自动 HTTPS，WebSocket 开箱即用）

```
panel.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

> 反代需与面板同机或经可信内网访问：面板只采信来自回环/内网地址的
> `X-Real-IP` / `X-Forwarded-For`（防止公网伪造头绕过登录限速）。

#### 8. 升级

```bash
sudo systemctl stop luanti-panel
sudo install -m 0755 panel-new /opt/luanti-panel/bin/panel
sudo systemctl start luanti-panel
```

前端已内嵌在二进制内。数据库结构自动迁移，无需手工操作。
实例侧二进制在各自下次启动时自动刷新。

#### 9. 备份与恢复

- **面板内**：备份页手动/定时（按小时）zip 备份世界 → `data/instances/<id>/backups/world/`；
  可锁定单份不被轮转；恢复会自动停服。
- **整机备份**：直接打包 `/var/lib/luanti-panel`（含 `panel.db`、实例、备份）。
  建议停服后打包，或至少确认无备份任务在跑。

#### 10. 故障排查

| 现象 | 排查 |
|---|---|
| 控制台命令无响应 | 实例是否 running；`worlds/<w>/worldmods/panel_bridge/` 是否存在；conf 中 `secure.http_mods` 是否包含 `panel_bridge`；bridge 走 `127.0.0.1:<面板端口>` 直连，与反代无关 |
| 玩家连不上 | UDP 防火墙 / 云安全组；端口是否为实例配置的端口 |
| 配置表单为空 | 设置页下载二进制（自带 example）或手动放置 `minetest.conf.example` 后刷新 schema |
| 面板端口变更后命令失效 | 面板在实例**启动时**自愈 bridge 地址，改端口后需重启实例 |
| 自动重启不生效 | 确认实例「自动重启」开关；连续崩溃 5 次会熔断（状态 failed），需手动启动复位 |
| 服务端日志 | `journalctl -u luanti-panel -f` 与 `data/instances/<id>/logs/debug.txt` |

### Windows 部署现状与不足

面板可以编译运行在 Windows（`go build -o panel.exe ./cmd/panel`），适合**本地开发与联调**
（配合 `tools/mock-luanti` 模拟服务端），但**不建议用于生产**。已知不足：

1. **没有 SIGTERM**：Linux 上停止进程是「bridge `/shutdown` → SIGTERM 进程组 → SIGKILL」，
   引擎有多次机会优雅保存世界；Windows 无 SIGTERM，bridge 未就绪（首次启动、端口未通、
   mod 未加载）时只能**直接强杀**，自上次自动保存以来的世界改动会丢失。
2. **没有进程组**：无法像 Linux `Setpgid` 那样整组终止子进程树；若引擎或 mod 派生了
   外部进程，强杀父进程后子进程会残留。
3. **没有原生服务托管**：需用 nssm / 任务计划程序包装；缺少 systemd 的 `KillMode`、
   `TimeoutStopSec`、日志收集能力，开机自启与崩溃拉起都要额外配置。
4. **可执行文件被锁定**：实例运行时无法替换共享二进制（Windows 独占锁），
   「下载并更新二进制」可能失败或留下「新 `builtin` + 旧二进制」的半更新状态；
   Linux 允许替换运行中的文件，无此问题。
5. **硬链接受卷限制**：`data/bin` 与实例目录不在同一磁盘卷时，二进制与运行时镜像
   退化为复制——启动更慢、占用更多空间。
6. **权限模型不同**：POSIX 属主/权限位在 Windows 上无效，多用户隔离弱；
   建议专用账户运行并限制目录 ACL。
7. **监控精度**：gopsutil 在 Windows 上部分指标（如 CPU% 的采样方式）与 Linux 有差异。
8. **结论**：Windows 用于开发/演示；生产请用 Linux，或 WSL2 / Docker（容器内按 Linux 语义运行）。

### 数据目录

```
data/
├── panel.db                     # SQLite：实例/用户/会话/审计/活动/schema
├── initial_admin_password.txt   # 首次启动生成，改密后请删除
├── bin/luantiserver             # 共享二进制（启动时硬链接/复制到实例）
├── bin/minetest.conf.example    # 配置 schema 来源
├── games/<gameid>/              # 全局游戏库
└── instances/<id>/              # 每实例 RUN_IN_PLACE 布局
    ├── bin/luantiserver
    ├── config/minetest.conf
    ├── games/  mods/
    ├── worlds/<world>/          # 世界（world.mt、auth.sqlite、map.sqlite、worldmods/panel_bridge）
    ├── logs/debug.txt
    └── backups/{world,config}/
```

### 安全建议

- 面板只监听 `127.0.0.1`，对外仅暴露反代的 443；不要把 8080 直接暴露公网。
- 首次登录立即改密；`initial_admin_password.txt` 用完删除。
- 角色最小化：`operator` 可启停、看日志与玩家，但**不能**删实例、装 Mod、管用户、
  查看实例管理员密码。
- 游戏端口用 UDP 直连，面板不代理游戏流量。
- 定期备份并锁定关键快照；`panel.db` 与实例目录同等重要。
- 登录已限速（同 IP 连续 5 次失败锁 1 分钟）；建议再加 fail2ban 于反代层。

### 已知限制

- 运行中备份会先发 `/save`，但仍是**文件级拷贝**，不保证 SQLite 原子一致——重要节点请停服备份。
- 单面板单机部署，无集群/多租户；面板用户仅 admin/operator 两种角色。
- ContentDB 与 GitHub 下载依赖外网；受限网络可手动放置二进制与模组包。
- 世界数据库为 SQLite，超大世界（数十 GB）的备份与恢复耗时明显。

### 开发与测试

```bash
go test ./...                          # 含进程生命周期回归测试（自动重启/优雅停止）
go run ./cmd/panel -addr :8080 -data ./data

cd web && npm run dev                  # Vite 5173，/api 代理到 8080

go build -o mock-luanti ./tools/mock-luanti
# 设置页把二进制路径指向 mock-luanti，创建实例并启动：
# - 模拟玩家 join/leave/CHAT（真实日志格式）
# - 实现 bridge 协议（/status /list /grant /privs /kick /shutdown 等）
```

### 许可

MIT，见 [LICENSE](LICENSE)。

---

## English

### What it is

Luanti (formerly Minetest) ships no management UI. This is a self-hosted web panel that
gives every instance its own RUN_IN_PLACE directory, supervises the process, streams logs,
parses player activity, and executes console commands through a `panel_bridge` mod that is
auto-installed into the world.

> **Why not stdin?** Verified against the engine source: the Luanti 5.x dedicated server
> **does not read stdin** (`dedicated_server_loop` has no input handling; `--terminal` is an
> ncurses TUI). Commands therefore go through a bridge mod that polls the panel every 2 s and
> invokes `core.registered_chatcommands` as the `name` from `minetest.conf` (the admin player,
> which the engine grants all privileges).

### Features

Multiple instances with isolated dirs/ports/worlds · start/stop/restart with graceful
`/shutdown` → SIGTERM → SIGKILL escalation and crash auto-restart (5 consecutive crashes
trip the breaker, webhook alerts) · WebSocket log streaming and web console · config form
generated from `minetest.conf.example` with snapshots, diffs and rollback · ContentDB search
with hard-dependency resolution · world.mt mod toggles · player list, kick, privileges,
password reset · activity & block audit · scheduled zip backups with retention and locking ·
file browser with path-traversal protection · bcrypt sessions with admin/operator roles,
login rate limiting and audit log · CPU/RSS sampling.

### Requirements

- **Linux** (recommended for production), x86_64 / aarch64, systemd
- **Go 1.26+** (`CGO_ENABLED=0` builds a static binary)
- **Node.js 18+** (frontend only; a prebuilt `web/dist` is committed)
- Outbound access to `api.github.com` and `content.luanti.org` (or place binaries/mods manually)

### Deploy from a release (recommended, batteries included)

Every Linux asset already bundles the matching **Luanti headless server** from
`ROllerozxa/luantiserver` (including the `builtin/` runtime and `minetest.conf.example`),
so first run needs no binary download at all.

```bash
# 1) download and verify (replace 1.0.0 with the latest tag)
VER=1.0.0
BASE=https://github.com/jiqing112/luanti-server-web-panel/releases/download/v${VER}
curl -fLO ${BASE}/luanti-panel_${VER}_linux_amd64.tar.gz   # use _linux_arm64 on arm64
curl -fLO ${BASE}/SHA256SUMS.txt
sha256sum -c SHA256SUMS.txt --ignore-missing

# 2) unpack (layout: panel + data/bin with the bundled server + README/LICENSE)
sudo useradd -r -m -d /opt/luanti-panel -s /usr/sbin/nologin luanti || true
sudo mkdir -p /opt/luanti-panel
sudo tar -xzf luanti-panel_${VER}_linux_amd64.tar.gz -C /opt/luanti-panel --strip-components=1
sudo chown -R luanti:luanti /opt/luanti-panel

# 3) first run (the bundled server already lives in data/bin)
cd /opt/luanti-panel
sudo -u luanti ./panel -addr 127.0.0.1:8080 -data ./data
```

The first start prints the initial admin credential and stores it in
`data/initial_admin_password.txt`. Then: change it → check that **Settings** shows the bundled
server version → **Refresh config schema** → create an instance → open its UDP port → start.

> To keep data under `/var/lib`: `sudo cp -a data /var/lib/luanti-panel` and run with
> `-data /var/lib/luanti-panel` (the directory passed to `-data` must contain `bin/`).
>
> **Upgrade:** unpack a newer release over `panel` and `data/bin`, keep `panel.db` and
> `instances/` untouched.
>
> **The Windows asset contains `panel.exe` only** (no upstream Windows server); see
> `README-Windows.txt` in the zip and the Windows section below.

### Build and deploy from source

```bash
# 1) service user + dirs
sudo useradd -r -m -d /opt/luanti-panel -s /usr/sbin/nologin luanti || true
sudo mkdir -p /opt/luanti-panel/bin /var/lib/luanti-panel
sudo chown -R luanti:luanti /opt/luanti-panel /var/lib/luanti-panel

# 2) build
git clone https://github.com/jiqing112/luanti-server-web-panel.git
cd luanti-server-web-panel
(cd web && npm ci && npm run build)          # optional, dist is committed
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o panel ./cmd/panel

# cross-compile from another OS
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o panel-amd64 ./cmd/panel

# 3) install + first run
sudo install -m 0755 panel /opt/luanti-panel/bin/panel
sudo -u luanti /opt/luanti-panel/bin/panel -addr 127.0.0.1:8080 -data /var/lib/luanti-panel
# The initial admin credential is printed to stdout and stored in
# /var/lib/luanti-panel/initial_admin_password.txt
```

systemd unit:

```ini
[Unit]
Description=Luanti Web Panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=luanti
Group=luanti
WorkingDirectory=/opt/luanti-panel
ExecStart=/opt/luanti-panel/bin/panel -addr 127.0.0.1:8080 -data /var/lib/luanti-panel
Restart=on-failure
RestartSec=3
LimitNOFILE=65535
# Instances run in their own process groups; stopping sends SIGTERM (graceful save).
# Parallel stop, worst case ~60s per instance (bridge /shutdown 6s + exit wait 30s + signal grace 20s).
KillMode=control-group
TimeoutStopSec=120

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload && sudo systemctl enable --now luanti-panel
sudo journalctl -u luanti-panel -n 30 --no-pager
```

Then, in the UI: change the initial credential → **Settings → Luanti server binary →
Download** (or set a manual path) → **Refresh config schema** → **Instances → Create instance**.

Open firewall ports — the **game** ports are UDP, the panel is TCP behind your proxy:

```bash
sudo ufw allow 30000:30100/udp
sudo ufw allow 80,443/tcp && sudo ufw reload
```

Reverse proxy (Nginx):

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 3600s;     # keep idle log streams alive
    client_max_body_size 100m;    # file upload limit
}
```

Caddy:

```
panel.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Upgrade: stop the service, replace the binary, start it again. Instance binaries refresh on
their next start (size/mtime comparison). Backups: per-instance zip backups with retention
and locking, or tar the whole data directory while the panel is stopped.

### Windows: current state and shortcomings

The panel compiles and runs on Windows (`go build -o panel.exe ./cmd/panel`) and is fine for
**local development** (pair it with `tools/mock-luanti`), but it is **not recommended for
production**:

1. **No SIGTERM.** On Linux stopping is `bridge /shutdown` → SIGTERM process group → SIGKILL,
   giving the engine repeated chances to save the world. Windows has no SIGTERM, so if the
   bridge is not ready (fresh start, port unreachable, mod not loaded) the process is **killed
   outright** and world changes since the last autosave are lost.
2. **No process groups.** There is no `Setpgid` equivalent: children spawned by the engine or a
   mod are not terminated with the parent.
3. **No native service manager.** You need nssm or Task Scheduler; `KillMode`, `TimeoutStopSec`
   and journal collection have no equivalent, so autostart and crash recovery need extra setup.
4. **Executable is locked while running.** The shared binary cannot be replaced while any
   instance runs — "download & update binary" can fail or leave a half-updated state
   (new `builtin`, old binary). Linux allows replacing files in use.
5. **Hard links are volume-bound.** If `data/bin` and the instance dirs live on different
   volumes, binary and runtime mirroring fall back to copying (slower starts, more disk).
6. **Different permission model.** POSIX ownership/mode bits are meaningless; multi-user
   isolation is weaker — use a dedicated account and directory ACLs.
7. **Monitoring fidelity.** Some gopsutil metrics (e.g. CPU%) are sampled differently than on Linux.
8. **Bottom line:** use Windows for development/demos; run production on Linux, WSL2 or Docker.

### Data layout, security, limits

`data/panel.db` (SQLite), `data/bin`, `data/games`, and `data/instances/<id>/` in RUN_IN_PLACE
layout (`bin/`, `config/`, `games/`, `mods/`, `worlds/<w>/`, `logs/debug.txt`, `backups/`).

- Bind the panel to `127.0.0.1`; expose only the HTTPS proxy. Never publish port 8080.
- Change the initial credential immediately and delete `initial_admin_password.txt`.
- `operator` can start/stop instances and view logs/players but cannot delete instances,
  install mods, manage users, or read the instance admin credential.
- Backups taken while running first issue `/save` but remain file-level copies — stop the
  instance for important snapshots.
- Single-panel, single-host; no clustering or multi-tenancy. ContentDB/GitHub downloads need
  outbound internet.

### Development & license

```bash
go test ./...                       # includes process-lifecycle regression tests
cd web && npm run dev               # Vite on 5173, proxies /api to :8080
go build -o mock-luanti ./tools/mock-luanti   # fake server for end-to-end testing
```

MIT — see [LICENSE](LICENSE).

---

## 日本語

### 概要

Luanti（旧 Minetest）には管理 UI がありません。本プロジェクトはセルフホスト型の Web
パネルで、インスタンスごとに RUN_IN_PLACE ディレクトリを生成し、プロセス管理・ログ配信・
プレイヤー解析を行い、ワールドに自動配置される `panel_bridge` mod 経由でコンソール
コマンドを実行します。

> **なぜ stdin ではないのか。** Luanti 5.x の専用サーバは**stdin を読みません**
> （`dedicated_server_loop` に入力処理がなく、`--terminal` は ncurses TUI）。そこで
> 2 秒ごとにパネルへポーリングする bridge mod が、`minetest.conf` の `name`
> （管理者プレイヤー）として `core.registered_chatcommands` を呼び出します。

### 主な機能

複数インスタンス管理（ディレクトリ/ポート/ワールド分離）· 起動/停止/再起動
（`/shutdown` → SIGTERM → SIGKILL の段階的停止、連続 5 回クラッシュで自動再起動を停止）·
WebSocket ログ配信と Web コンソール · `minetest.conf.example` から生成する設定フォーム
（スナップショット/diff/ロールバック）· ContentDB 検索と依存解決 · world.mt の Mod 切替 ·
プレイヤー一覧・キック・権限・パスワード再設定 · 行動/ブロック監査 · 定期 zip バックアップ
（ローテーション/ロック/復元）· ファイルブラウザ · bcrypt セッションと管理者/オペレーター
の二役割・ログイン試行制限・監査ログ · CPU/メモリ監視。

### 必要環境

- **Linux**（本番推奨）x86_64 / aarch64、systemd
- **Go 1.26+**（`CGO_ENABLED=0` で静的ビルド可）
- **Node.js 18+**（フロントエンドのビルド時のみ。ビルド済み `web/dist` を同梱）
- `api.github.com` と `content.luanti.org` への外向き通信（不可の場合は手動配置）

### リリースからデプロイ（推奨・すぐ使える）

Linux 向けの各アセットには、対応アーキテクチャの **Luanti ヘッドレスサーバー**
（`ROllerozxa/luantiserver`、`builtin/` ランタイムと `minetest.conf.example` を含む）が
同梱されています。初回起動時にバイナリをダウンロードする必要はありません。

```bash
# 1) ダウンロードと検証（1.0.0 は Releases の最新タグに置換）
VER=1.0.0
BASE=https://github.com/jiqing112/luanti-server-web-panel/releases/download/v${VER}
curl -fLO ${BASE}/luanti-panel_${VER}_linux_amd64.tar.gz   # arm64 は _linux_arm64
curl -fLO ${BASE}/SHA256SUMS.txt
sha256sum -c SHA256SUMS.txt --ignore-missing

# 2) 展開（構成: panel + data/bin の同梱サーバー + README/LICENSE）
sudo useradd -r -m -d /opt/luanti-panel -s /usr/sbin/nologin luanti || true
sudo mkdir -p /opt/luanti-panel
sudo tar -xzf luanti-panel_${VER}_linux_amd64.tar.gz -C /opt/luanti-panel --strip-components=1
sudo chown -R luanti:luanti /opt/luanti-panel

# 3) 初回起動（同梱サーバーは data/bin にあります）
cd /opt/luanti-panel
sudo -u luanti ./panel -addr 127.0.0.1:8080 -data ./data
```

初回起動時に管理者の初期資格情報が表示され、`data/initial_admin_password.txt` にも
保存されます。その後: 資格情報の変更 → **設定** で同梱サーバーのバージョンを確認 →
**設定スキーマを更新** → インスタンス作成 → UDP ポート開放 → 起動。

> データを `/var/lib` に置く場合: `sudo cp -a data /var/lib/luanti-panel` として
> `-data /var/lib/luanti-panel` で起動します（`-data` のディレクトリに `bin/` が必要）。
>
> **アップグレード:** 新しいリリースを展開して `panel` と `data/bin` を上書きし、
> `panel.db` と `instances/` はそのまま残します。
>
> **Windows 版は `panel.exe` のみ**（上流に Windows サーバーがないため）。zip 内の
> `README-Windows.txt` と後述の Windows セクションを参照してください。

### ソースからビルドしてデプロイ

```bash
# 1) サービスユーザーとディレクトリ
sudo useradd -r -m -d /opt/luanti-panel -s /usr/sbin/nologin luanti || true
sudo mkdir -p /opt/luanti-panel/bin /var/lib/luanti-panel
sudo chown -R luanti:luanti /opt/luanti-panel /var/lib/luanti-panel

# 2) ビルド
git clone https://github.com/jiqing112/luanti-server-web-panel.git
cd luanti-server-web-panel
(cd web && npm ci && npm run build)        # 任意（dist 同梱）
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o panel ./cmd/panel

# 3) 配置と初回起動
sudo install -m 0755 panel /opt/luanti-panel/bin/panel
sudo -u luanti /opt/luanti-panel/bin/panel -addr 127.0.0.1:8080 -data /var/lib/luanti-panel
# 初期の管理者資格情報は標準出力と
# /var/lib/luanti-panel/initial_admin_password.txt に出力されます
```

systemd ユニット:

```ini
[Unit]
Description=Luanti Web Panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=luanti
Group=luanti
WorkingDirectory=/opt/luanti-panel
ExecStart=/opt/luanti-panel/bin/panel -addr 127.0.0.1:8080 -data /var/lib/luanti-panel
Restart=on-failure
RestartSec=3
LimitNOFILE=65535
# 各インスタンスは独立したプロセスグループで動作。停止は SIGTERM（保存して終了）。
# 並列停止、1 インスタンス最悪約 60 秒（bridge /shutdown 6s + 終了待ち 30s + 猶予 20s）
KillMode=control-group
TimeoutStopSec=120

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload && sudo systemctl enable --now luanti-panel
sudo journalctl -u luanti-panel -n 30 --no-pager
```

UI での初期設定: 資格情報の変更 → **設定 → Luanti サーバーバイナリ → ダウンロード**
（または手動パス）→ **設定スキーマを更新** → **インスタンス → 作成**。

ファイアウォール（ゲームポートは **UDP**、パネルは TCP）:

```bash
sudo ufw allow 30000:30100/udp
sudo ufw allow 80,443/tcp && sudo ufw reload
```

Nginx 逆プロキシ:

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 3600s;     # アイドル時のログストリームを維持
    client_max_body_size 100m;    # アップロード上限
}
```

Caddy:

```
panel.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

アップグレード: サービス停止 → バイナリ差し替え → 起動。インスタンス側のバイナリは
次回起動時に自動更新（サイズ/更新時刻で判定）。バックアップはインスタンス単位の zip
（保持数/ロック）またはパネル停止中のデータディレクトリ全体の tar。

### Windows の現状と不足

Windows でもビルド・実行できます（`go build -o panel.exe ./cmd/panel`）。**ローカル開発**
（`tools/mock-luanti` と併用）には十分ですが、**本番利用は非推奨**です。

1. **SIGTERM がない。** Linux は `bridge /shutdown` → SIGTERM（プロセスグループ）→ SIGKILL
   の順で、エンジンが世界を保存する機会があります。Windows では bridge が未準備
   （初回起動・ポート不通・mod 未読込）だと**即時強制終了**となり、最後の自動保存以降の
   変更が失われます。
2. **プロセスグループがない。** `Setpgid` 相当がなく、エンジンや mod が起動した子プロセスは
   親と一緒に終了しません。
3. **サービス管理がない。** nssm やタスクスケジューラで包む必要があり、`KillMode` /
   `TimeoutStopSec` / journal 相当の機能がありません。
4. **実行中はファイルがロックされる。** インスタンス稼働中は共有バイナリを置換できず、
   更新が失敗したり「新しい `builtin` + 古いバイナリ」の半端な状態になり得ます。
5. **ハードリンクはボリューム依存。** `data/bin` とインスタンスが別ボリュームだとコピーに
   退化し、起動が遅くディスクを余分に消費します。
6. **権限モデルが異なる。** POSIX の所有者/モードが無効で分離が弱い。専用アカウントと
   ACL を推奨。
7. **監視精度。** gopsutil の一部指標（CPU% など）は Linux と挙動が異なります。
8. **結論:** Windows は開発・デモ用。本番は Linux、WSL2、Docker を利用してください。

### セキュリティと制限

- パネルは `127.0.0.1` にバインドし、外部へは HTTPS 逆プロキシのみ公開（8080 を直接公開しない）。
- 初期の資格情報は直ちに変更し、`initial_admin_password.txt` を削除。
- `operator` は起動/停止・ログ閲覧のみ。インスタンス削除、Mod 導入、ユーザー管理、
  管理者の資格情報の閲覧は不可。
- 稼働中バックアップは `/save` を先に送りますがファイル単位のコピーであり、SQLite の
  原子的スナップショットではありません。重要時は停止して取得してください。
- 単一ホスト・単一パネル構成（クラスタ/マルチテナント非対応）。ContentDB/GitHub 取得には
  外向き通信が必要です。

### 開発とライセンス

```bash
go test ./...                       # プロセスライフサイクルの回帰テストを含む
cd web && npm run dev               # Vite 5173（/api は :8080 へプロキシ）
go build -o mock-luanti ./tools/mock-luanti   # E2E 用の疑似サーバー
```

MIT — [LICENSE](LICENSE) を参照。
