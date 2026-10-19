# Agent 流量重置日同步协议

管理员端：

- `GET /api/admin/client/:uuid/traffic-config`（`admin:getAgentTrafficConfig`）读取主控目标和最后确认状态。
- `POST /api/admin/client/:uuid/traffic-config`（`admin:setAgentTrafficConfig`）发送 `{managed:boolean, day:integer}`，day 必须为 0～31。
- managed=false 表示恢复本地，不强制 Agent 使用 day=0；实际本地值由 Agent 回报。
- 保存返回持久化目标，不是“已应用”。每次保存生成独立 revision，离线也允许保存。

Agent 端：

通过现有 token 认证 `/api/clients/v2/rpc` 发出 `agent.trafficConfig.sync`，参数为 `{revision, managed, day, failed_revision}`，其中 revision/managed/day 为当前实际运行值，failed_revision 仅标识无法应用的目标版本。不接收节点 UUID，服务端从认证上下文绑定身份。

返回 AgentTrafficConfig 对象，其中 revision/managed/day 为主控目标；空 revision 表示尚未接管，Agent 不做修改。Agent 串行拉取、持久化并应用后立即再次同步确认。主控在当前 revision、模式及目标日期全部匹配时才更新 applied_revision。旧回报不确认新配置，失败报告不携带任意 Agent 错误文本。

能力及状态：seen_at 只有调用新协议的 Agent 才更新；缺失表示离线或需要升级。超过60秒未回报仅显示上次确认数据，不能声称仍在线生效。配置表独立于公开节点元数据和通用 editClient，避免泄露管理状态或任意更新。

测试：`CGO_ENABLED=1 go test ./database/clients ./web/rpc/jsonrpc ./web/api/client`；独立真实双端 E2E 脚本在开发机器 `/tmp/opencode/komari-traffic-e2e.py`，仅使用本地端口25891和临时数据库。
