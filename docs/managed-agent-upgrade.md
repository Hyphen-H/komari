# 面板远程 Agent 升级

## 固定 fork 版

管理员端 `POST /api/admin/client/:uuid/upgrade-managed` 必须携带 `{confirm:true}`，走管理员及敏感操作2FA权限。主控先读取Agent启动配置（不向浏览器返回敏感字段），确认在线、支持读取配置及未禁用远程控制，创建可审计远程任务。

该任务仅接受内置固定资产，不允许客户端提供任意URL或shell脚本：

- Release: https://github.com/Hyphen-H/komari-agent/releases/tag/managed-traffic-db3c5e8
- amd64 SHA256: `0b49417c5969d87b25e702d5b04de0d115420f5a74c1f6126177a3e3921dcb79`
- arm64 SHA256: `bfc4ee24d0316bbbdc981fe0136681dad22137523104eed9e9aa330715a433cb`
- 源码提交db3c5e8，上游828afaf；编译版本dev-managed-traffic-db3c5e8，update.Repo=Hyphen-H/komari-agent。

仅支持Linux amd64/arm64、root、持久化systemd服务。脚本核对父Agent PID、systemd MainPID，拒绝容器/临时unit，原子目录锁防并发；HTTPS下载、验证SHA，备份旧二进制/服务文本/身份/流量文件。独立systemd临时任务停止原服务、替换程序、重启；启动失败恢复旧程序。不会重装、修改服务参数或删除身份历史。

升级后未来版本切换来源变为用户fork，界面发送前明确确认；原有disable-auto-update保持不变。开发版本本身不走可解析SemVer自动更新，需未来发布稳定版本另行安排，不能宣称已建立自动更新通道。

## 普通版本切换

`POST /api/admin/client/:uuid/switch-version` 使用Agent自身仓库。适用于latest/版本线/精确版本，不能跨仓库升级，latest不保证流量同步。未确认远程控制能力时拒绝发送。

## 状态

`GET /api/admin/client/:uuid/upgrade-status` 仅返回版本、在线、流量同步能力和固定fork目标信息。发送成功、远程命令退出0、systemd active均不等于应用能力已确认；面板以重连后上报目标版本和近期流量协议回报确认定制升级。180秒未确认会提示排查，不自动重发。远程任务失败展示截断输出；可在远程执行查看任务ID。

## 本地验证

Go脚本安全/权限测试通过；隔离主控、原版828afaf Agent和持久化systemd服务的E2E已覆盖公开下载SHA、未授权拒绝、旧版到fork替换、版本能力确认、流量15日同步、原配置及历史保留。开发机脚本 `/tmp/opencode/komari-remote-upgrade-e2e.py`。不针对全体生产节点自动发送升级。
