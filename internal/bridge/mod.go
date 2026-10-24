package bridge

// LuaModFiles 返回写入 <world>/worldmods/panel_bridge/ 的文件内容。
// mod 读取 minetest.conf 中的 panel_bridge.url / panel_bridge.token（由面板写入）。
func LuaModFiles() map[string]string {
	return map[string]string{
		"mod.conf": `name = panel_bridge
title = Luanti Web Panel Bridge
description = Luanti Web Panel 管理桥：轮询面板命令、上报在线玩家。由面板自动安装。`,
		"init.lua": bridgeLua,
	}
}

const bridgeLua = `-- Luanti Web Panel Bridge（由面板自动安装到 worldmods/，请勿手改）
-- 职责：
--   1. 每 2 秒向面板上报在线玩家并领取待执行命令
--   2. 以管理员（minetest.conf 的 name 设置）身份执行命令并回传输出
-- 安全：token 在 minetest.conf 的 panel_bridge.token，引擎要求
--       secure.http_mods 包含 panel_bridge 才会开放 HTTP API。

local http = core.request_http_api()
local token = core.settings:get("panel_bridge.token") or ""
local base = (core.settings:get("panel_bridge.url") or ""):gsub("/+$", "")

if not http then
	core.log("error", "[panel_bridge] 未获得 HTTP API 权限，请在 minetest.conf 中设置 secure.http_mods = panel_bridge")
	return
end
if base == "" or token == "" then
	core.log("error", "[panel_bridge] 缺少 panel_bridge.url / panel_bridge.token 配置")
	return
end

local function connected_players()
	local list = {}
	for _, p in ipairs(core.get_connected_players()) do
		local n = p:get_player_name()
		if n ~= "" then
			-- is_creative_enabled：引擎读 creative_mode 设置，游戏（如 Mineclonia
			-- 的 mcl_gamemode）会覆盖为读玩家 gamemode，即"无限物品"是否生效
			local ok_c, creative = pcall(core.is_creative_enabled, n)
			table.insert(list, { name = n, creative = ok_c and creative == true })
		end
	end
	table.sort(list, function(a, b) return a.name < b.name end)
	return list
end

-- 以管理员身份执行聊天命令（与 builtin/game/chat.lua 的分发逻辑一致）
local function exec_command(text)
	local cmd, param = text:match("^%s*/?([^%s]+)%s*(.-)%s*$")
	if not cmd or cmd == "" then
		return "空命令"
	end
	local def = core.registered_chatcommands[cmd]
	if not def then
		return "未知命令: " .. cmd
	end
	local name = core.settings:get("name")
	if not name or name == "" then
		name = "@console"
	end
	local ok, ret = def.func(name, param or "")
	if ret == nil then
		ret = ok and "执行成功" or "执行失败"
	end
	if type(ret) ~= "string" then
		ret = tostring(ret)
	end
	return (ok and "" or "[失败] ") .. ret
end

local function post(path, data, cb)
	http.fetch({
		url = base .. path,
		method = "POST",
		data = core.write_json(data),
		extra_headers = { "Content-Type: application/json", "X-Bridge-Token: " .. token },
		timeout = 6,
	}, cb)
end

local function heartbeat()
	local players = connected_players()
	post("/api/bridge/poll", {
		players = players,
		time = os.time(),
	}, function(res)
		if res.succeeded and res.code == 200 and res.data and res.data ~= "" then
			local ok, body = pcall(core.parse_json, res.data)
			if ok and type(body) == "table" and type(body.commands) == "table" then
				for _, c in ipairs(body.commands) do
					if type(c) == "table" and c.id and c.text then
						core.log("action", "[panel_bridge] 执行命令: " .. tostring(c.text))
						local out = exec_command(tostring(c.text))
						post("/api/bridge/result", { id = c.id, output = out }, function() end)
					end
				end
			end
		end
		core.after(2, heartbeat)
	end)
end

core.log("action", "[panel_bridge] 已启动")
heartbeat()
`
