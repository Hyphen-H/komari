set -eu
trap '' PIPE
unit=$1
echo '[1/6] Checking Linux, systemd service and current Agent' || true
[ "$(uname -s)" = Linux ] || { echo 'Only Linux systemd is supported'; exit 1; }
[ "$(id -u)" = 0 ] || { echo 'Run this command as root or with sudo'; exit 1; }
for tool in systemctl readlink curl sha256sum mktemp; do
  command -v "$tool" >/dev/null || { echo "Missing required tool: $tool"; exit 1; }
done
[ ! -f /.dockerenv ] && [ ! -f /.komari-agent-container ] || { echo 'Container: update the container image instead'; exit 1; }
systemctl is-active --quiet "$unit" || { echo "Service is not running: $unit. Check its name and start it first."; exit 1; }
# 面板 Web 终端 / 远程执行都由目标 Agent 自己提供，停止 Agent 会当场杀掉本脚本，
# 可能留下“服务已停、程序未替换、未回滚”的半完成状态。检测到这种执行环境时直接拒绝。
self_cgroup=$(cat /proc/self/cgroup 2>/dev/null || true)
unit_cgroup=$(systemctl show "$unit" -p ControlGroup --value 2>/dev/null || true)
if [ -n "$unit_cgroup" ]; then
  case "$self_cgroup" in
    *"$unit_cgroup"*)
      echo '拒绝执行：当前会话由目标 Agent 自身提供（例如面板 Web 终端或远程执行）。'
      echo '停止 Agent 会立即中断此会话，升级会半途而废。'
      echo '请改用你自己电脑上的 SSH 客户端登录本机后重新执行本命令。'
      exit 1 ;;
  esac
fi
[ "$(systemctl show "$unit" -p Transient --value)" = no ] || { echo 'Transient systemd units are not supported'; exit 1; }
agent_pid=$(systemctl show "$unit" -p MainPID --value)
binary=$(readlink -f "/proc/$agent_pid/exe")
workdir=$(readlink -f "/proc/$agent_pid/cwd")
[ -f "$binary" ] && [ -x "$binary" ] && [ -d "$workdir" ] || { echo 'Cannot resolve running Agent executable/workdir'; exit 1; }
case $(basename "$binary") in
  agent|komari-agent|komari-agent-*) ;;
  *) echo 'Service executable is not recognized as Komari Agent; refusing replacement'; exit 1 ;;
esac
case $(uname -m) in
  x86_64) asset=komari-agent-linux-amd64; digest='@AMD64@' ;;
  aarch64|arm64) asset=komari-agent-linux-arm64; digest='@ARM64@' ;;
  *) echo 'Only amd64/arm64 is supported'; exit 1 ;;
esac
umask 077
lock="$(dirname "$binary")/.komari-managed-upgrade.lock"
logout="$(dirname "$binary")/manual-upgrade.log"
mkdir "$lock" 2>/dev/null || {
  echo "An upgrade lock exists: $lock"
  echo "若确认没有正在进行的升级（pgrep -af komari-agent 无其它升级进程），"
  echo "查看上轮日志 $logout 后执行 rmdir '$lock' 再重试。"
  exit 1
}
: > "$logout"
stopped=0
backup=''
finish() {
  rc=$?
  trap - EXIT INT TERM HUP
  # 终端可能在升级中途断开（SSH 掉线等）。从这里开始输出只写日志文件，
  # 保证回滚逻辑绝不会再向已经关闭的终端写数据而被 SIGPIPE 杀死。
  exec >>"$logout" 2>&1 || true
  echo "[$(date -u '+%Y-%m-%dT%H:%M:%SZ')] finish rc=$rc stopped=$stopped"
  if [ "$rc" -ne 0 ] && [ "$stopped" = 1 ]; then
    echo '[ROLLBACK] Restoring previous binary (identity/history are not reverted)'
    systemctl stop "$unit" >/dev/null 2>&1 || true
    if cp -p "$backup/old-agent" "$backup/restore-agent" && mv -f "$backup/restore-agent" "$binary"; then
      systemctl start "$unit" >/dev/null 2>&1 || true
      echo "[ROLLBACK] Old binary restored; check: systemctl status $unit"
    else
      echo "Restore failed. Old binary is at: $backup/old-agent"
    fi
  fi
  rmdir "$lock" 2>/dev/null || true
  if [ -n "$backup" ]; then echo "Backup directory: $backup"; fi
  exit "$rc"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP
backup=$(mktemp -d "$(dirname "$binary")/.komari-upgrade-XXXXXXXX")
echo '[2/6] Downloading pinned fork build (maximum 180 seconds)' || true
curl --fail --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 10 --max-time 180 '@BASE@/'"$asset" -o "$backup/new-agent"
printf '%s  %s\n' "$digest" "$backup/new-agent" | sha256sum -c -
chmod --reference="$binary" "$backup/new-agent"
chown --reference="$binary" "$backup/new-agent"
echo '[3/6] Backing up program and service; startup parameters are unchanged' || true
cp -p "$binary" "$backup/old-agent"
systemctl cat "$unit" > "$backup/service.backup"
[ "$(systemctl show "$unit" -p MainPID --value)" = "$agent_pid" ] || { echo 'Agent changed during preparation; aborting without replacement'; exit 1; }
echo '[4/6] Stopping Agent and saving identity/traffic snapshots' || true
stopped=1
systemctl stop "$unit"
for f in net_static.json managed-traffic.json; do
  if [ -f "$workdir/$f" ]; then cp -p "$workdir/$f" "$backup/$f"; fi
done
if [ -f "$(dirname "$binary")/auto-discovery.json" ]; then cp -p "$(dirname "$binary")/auto-discovery.json" "$backup/auto-discovery.json"; fi
echo '[5/6] Replacing binary and restarting the same service' || true
mv -f "$backup/new-agent" "$binary"
systemctl start "$unit"
sleep 10
systemctl is-active --quiet "$unit"
new_pid=$(systemctl show "$unit" -p MainPID --value)
actual=$(sha256sum "/proc/$new_pid/exe" | cut -d ' ' -f 1)
[ "$actual" = "$digest" ] || { echo 'Running binary does not match target; rolling back'; exit 1; }
stopped=0
rmdir "$lock" 2>/dev/null || true
echo '[6/6] Target binary is running. Return to the panel and wait for version/traffic capability confirmation.' || true
echo 'No reinstall or registration was performed. Local reset day, identity and traffic history are preserved.' || true
echo 'Future version source: Hyphen-H/komari-agent; existing auto-update options are unchanged.' || true
