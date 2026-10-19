# SSH 更换 Agent（替代面板远程发送）

`GET /api/admin/client/:uuid/replacement-command?service=komari-agent`（管理员RPC `getAgentReplacementCommand`）返回固定版本的SSH shell命令；GET不创建任务、不发送Agent RPC。版本与资产固定，不接受任意URL/版本/脚本输入。返回值不含节点Token/管理密钥。

页面步骤：编辑节点 → 更换 Agent → 默认服务名komari-agent（自定义才改）→ 复制SSH更换命令 → 登录目标服务器粘贴执行。无需猜版本号。命令会打印1～6步及备份目录，完成后等待面板“已确认支持”再设置重置日。

支持Linux amd64/arm64、持久化systemd运行中的Agent；root无需安装sudo，非root需已有sudo权限。以service MainPID解析binary/cwd，限定识别的Agent文件名。HTTPS固定资产下载、SHA-256校验后才停止服务，保留程序权限和所有启动参数；备份旧二进制、服务文本、auto-discovery.json/net_static.json/managed-traffic.json；原子替换后启动并验证正在运行的文件hash，失败恢复旧程序，不覆盖采集后的历史。

不适用Docker、Alpine/OpenRC、Windows；拒绝执行而不是猜测安装方式。脚本不会关闭自动更新或重写服务参数；替换的开发fork版未来版本来源为Hyphen-H/komari-agent。不要对非目标机器执行。

历史远程升级API/任务记录兼容保留，但新页面不再调用。面板复制不等于目标机已更换；目标版本和近期流量同步能力由现有upgrade-status只读轮询核对。
