# SSH 更换 Agent（替代面板远程发送）

`GET /api/admin/client/:uuid/replacement-command?service=komari-agent`（管理员RPC `getAgentReplacementCommand`）返回固定版本的SSH shell命令；GET不创建任务、不发送Agent RPC。版本与资产固定，不接受任意URL/版本/脚本输入。返回值不含节点Token/管理密钥。

页面步骤：编辑节点 → 更换 Agent → 默认服务名komari-agent（自定义才改）→ 复制SSH更换命令 → 登录目标服务器粘贴执行。无需猜版本号。命令会打印1～6步及备份目录，完成后等待面板“已确认支持”再设置重置日。

支持Linux amd64/arm64、持久化systemd运行中的Agent；root无需安装sudo，非root需已有sudo权限。以service MainPID解析binary/cwd，限定识别的Agent文件名。HTTPS固定资产下载、SHA-256校验后才停止服务，保留程序权限和所有启动参数；备份旧二进制、服务文本、auto-discovery.json/net_static.json/managed-traffic.json；原子替换后启动并验证正在运行的文件hash，失败恢复旧程序，不覆盖采集后的历史。

**执行环境必须是你自己电脑的SSH客户端。** 面板Web终端（及历史远程执行）由目标Agent自身提供，其进程在该服务的cgroup内；执行到`systemctl stop`会当场杀掉终端和脚本，留下“服务已停、程序未替换、未回滚”的半完成状态（2026-10-19 tri节点实际发生过）。脚本现在会先对比`/proc/self/cgroup`与目标服务的`ControlGroup`，命中即用中文提示拒绝执行且不产生任何副作用。其他加固：HUP/INT/TERM都会触发回滚；回滚输出只写日志（`<agent目录>/manual-upgrade.log`）避免终端断开时SIGPIPE杀死回滚；成功路径在确认运行二进制后立刻释放锁，不依赖退出陷阱。

残留恢复：若升级被强行中断且提示“An upgrade lock exists”，确认没有升级进程（`pgrep -af komari-agent`）后查看`manual-upgrade.log`，执行`rmdir <agent目录>/.komari-managed-upgrade.lock`再重试；未完成替换时旧程序与备份都还在，服务可按原参数随时`systemctl start`。

不适用Docker、Alpine/OpenRC、Windows；拒绝执行而不是猜测安装方式。脚本不会关闭自动更新或重写服务参数；替换的开发fork版未来版本来源为Hyphen-H/komari-agent（内置自动更新源仍是官方仓库，如担心被官方版覆盖可自行决定是否加`--disable-auto-update`，脚本不会代为修改）。不要对非目标机器执行。

历史远程升级API/任务记录兼容保留，但新页面不再调用。面板复制不等于目标机已更换；目标版本和近期流量同步能力由现有upgrade-status只读轮询核对。
