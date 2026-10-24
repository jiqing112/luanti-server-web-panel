## Luanti Web Panel {{TAG}}

内置 Luanti 无头服务端：**{{UPSTREAM}}**（来自 ROllerozxa/luantiserver，开箱即用，无需联网下载）

### Linux 快速开始

```bash
curl -fLO https://github.com/{{REPO}}/releases/download/{{TAG}}/luanti-panel_{{VERSION}}_linux_amd64.tar.gz
curl -fLO https://github.com/{{REPO}}/releases/download/{{TAG}}/SHA256SUMS.txt
sha256sum -c SHA256SUMS.txt --ignore-missing

sudo mkdir -p /opt/luanti-panel
sudo tar -xzf luanti-panel_{{VERSION}}_linux_amd64.tar.gz -C /opt/luanti-panel --strip-components=1
cd /opt/luanti-panel && ./panel -addr 127.0.0.1:8080 -data ./data
```

首次启动会打印初始管理员密码（同时写入 `data/initial_admin_password.txt`）。
arm64 机器把文件名里的 `amd64` 换成 `arm64`。systemd、反向代理、防火墙等完整部署见仓库 README。

### 资产

| 文件 | 说明 |
| --- | --- |
| `luanti-panel_{{VERSION}}_linux_amd64.tar.gz` | Linux x86_64，内置 luantiserver {{UPSTREAM}} |
| `luanti-panel_{{VERSION}}_linux_arm64.tar.gz` | Linux aarch64，内置 luantiserver {{UPSTREAM}} |
| `luanti-panel_{{VERSION}}_windows_amd64.zip` | Windows x64 面板本体（需自备服务端二进制，见包内说明） |
| `SHA256SUMS.txt` | 全部资产的 SHA-256 校验和 |

---

## English

Bundled Luanti headless server: **{{UPSTREAM}}** (from ROllerozxa/luantiserver) — ready to run,
no binary download needed.

### Quick start (Linux)

```bash
curl -fLO https://github.com/{{REPO}}/releases/download/{{TAG}}/luanti-panel_{{VERSION}}_linux_amd64.tar.gz
curl -fLO https://github.com/{{REPO}}/releases/download/{{TAG}}/SHA256SUMS.txt
sha256sum -c SHA256SUMS.txt --ignore-missing

sudo mkdir -p /opt/luanti-panel
sudo tar -xzf luanti-panel_{{VERSION}}_linux_amd64.tar.gz -C /opt/luanti-panel --strip-components=1
cd /opt/luanti-panel && ./panel -addr 127.0.0.1:8080 -data ./data
```

The first start prints the initial admin credential (also stored in `data/initial_admin_password.txt`).
On arm64, replace `amd64` in the file name with `arm64`. systemd, reverse proxy and firewall setup
are documented in the repository README.

### Assets

| File | Notes |
| --- | --- |
| `luanti-panel_{{VERSION}}_linux_amd64.tar.gz` | Linux x86_64, bundles luantiserver {{UPSTREAM}} |
| `luanti-panel_{{VERSION}}_linux_arm64.tar.gz` | Linux aarch64, bundles luantiserver {{UPSTREAM}} |
| `luanti-panel_{{VERSION}}_windows_amd64.zip` | Windows x64 panel only (bring your own server binary, see the note inside) |
| `SHA256SUMS.txt` | SHA-256 checksums for all assets |

---

## 日本語

同梱 Luanti ヘッドレスサーバー: **{{UPSTREAM}}**（ROllerozxa/luantiserver より）— すぐに起動でき、
バイナリのダウンロードは不要です。

### クイックスタート（Linux）

```bash
curl -fLO https://github.com/{{REPO}}/releases/download/{{TAG}}/luanti-panel_{{VERSION}}_linux_amd64.tar.gz
curl -fLO https://github.com/{{REPO}}/releases/download/{{TAG}}/SHA256SUMS.txt
sha256sum -c SHA256SUMS.txt --ignore-missing

sudo mkdir -p /opt/luanti-panel
sudo tar -xzf luanti-panel_{{VERSION}}_linux_amd64.tar.gz -C /opt/luanti-panel --strip-components=1
cd /opt/luanti-panel && ./panel -addr 127.0.0.1:8080 -data ./data
```

初回起動時に管理者の初期資格情報が表示され、`data/initial_admin_password.txt` にも保存されます。
arm64 の場合はファイル名の `amd64` を `arm64` に置き換えてください。systemd・リバースプロキシ・
ファイアウォールなどの詳細はリポジトリの README を参照してください。

### アセット

| ファイル | 説明 |
| --- | --- |
| `luanti-panel_{{VERSION}}_linux_amd64.tar.gz` | Linux x86_64、luantiserver {{UPSTREAM}} 同梱 |
| `luanti-panel_{{VERSION}}_linux_arm64.tar.gz` | Linux aarch64、luantiserver {{UPSTREAM}} 同梱 |
| `luanti-panel_{{VERSION}}_windows_amd64.zip` | Windows x64 パネル本体（サーバーバイナリは別途用意、zip 内の説明参照） |
| `SHA256SUMS.txt` | 全アセットの SHA-256 チェックサム |
