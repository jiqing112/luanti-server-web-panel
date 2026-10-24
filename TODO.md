# TODO / 已知可改进项

欢迎 PR。以下为当前已知但尚未完成的小项，按影响面排序。

## 1. 实例名称校验（`internal/api/instance_handlers.go`）

目前创建实例只校验「非空且长度 ≤ 60」，没有过滤控制字符。控制字符会污染
HTTP 响应头、审计记录与页面展示。建议：

- 新增 `validDisplayName(s string) bool`：非空、≤60 字符、拒绝 `r < 0x20 || r == 0x7f`；
- 在 `handleCreateInstance` 与 `handleClone` 的名称校验处调用；
- 克隆分支同样加名称校验（当前克隆不校验名称长度）。

## 2. 创建/克隆失败时的孤儿目录（`internal/api/instance_handlers.go`）

`EnsureLayout` 之后若任一步失败（游戏缺失、写 conf 失败、入库失败），磁盘上会留下
无数据库记录的 `data/instances/<id>/` 目录。建议在目录创建成功后设置一个
`created := false` + `defer`，成功入库后才置 `true`，否则 `os.RemoveAll`。

## 3. 版本比较改为逐段数字（`web/src/pages/Settings.svelte`）

`web/src/lib/format.js` 已提供 `cmpVersion(a, b)`，但设置页仍用字符串比较
（`binary.version >= latest`），导致 `5.10.0` 被误判为小于 `5.9.0`，永远提示有更新。
改为 `cmpVersion(binary.version, latest) >= 0` 即可。

## 4. 用户管理接口的小加固（`internal/api/auth_handlers.go`）

- 登录成功后创建会话的错误目前被忽略，失败时应返回 500；
- `handleUpdateUser` / `handleDeleteUser` 解析出 `uid` 后应校验 `uid > 0`
  并确认用户存在（当前对不存在的 id 会静默返回成功）；
- `handleUpdateUser` 建议先完成全部校验再落库，避免「密码已改但角色校验失败」的半途状态。

## 5. 其他想法

- 世界备份改用 SQLite 一致性快照（当前为文件级拷贝，运行中备份前会先 `/save`）；
- 面板会话支持「查看并注销其他设备」；
- 多面板/远程实例托管（当前仅单机）。
