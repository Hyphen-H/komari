set -eu
unit=$1
echo '[1/6] Checking Linux, systemd service and current Agent'
[ "$(uname -s)" = Linux ] || { echo 'Only Linux systemd is supported'; exit 1; }
[ "$(id -u)" = 0 ] || { echo 'Run this command as root or with sudo'; exit 1; }
for tool in systemctl readlink curl sha256sum mktemp; do
  command -v "$tool" >/dev/null || { echo "Missing required tool: $tool"; exit 1; }
done
[ ! -f /.dockerenv ] && [ ! -f /.komari-agent-container ] || { echo 'Container: update the container image instead'; exit 1; }
systemctl is-active --quiet "$unit" || { echo "Service is not running: $unit. Check its name and start it first."; exit 1; }
[ "$(systemctl show "$unit" -p Transient --value)" = no ] || { echo 'Transient units are not supported'; exit 1; }
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
mkdir "$lock" 2>/dev/null || { echo "An upgrade lock exists: $lock. Inspect the previous upgrade before retrying."; exit 1; }
stopped=0
backup=''
finish() {
  rc=$?
  trap - EXIT INT TERM
  if [ "$rc" -ne 0 ] && [ "$stopped" = 1 ]; then
    echo '[ROLLBACK] Restoring previous binary (identity/history are not reverted)'
    systemctl stop "$unit" || true
    if cp -p "$backup/old-agent" "$backup/restore-agent" && mv -f "$backup/restore-agent" "$binary"; then
      systemctl start "$unit" || true
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
backup=$(mktemp -d "$(dirname "$binary")/.komari-upgrade-XXXXXXXX")
echo '[2/6] Downloading pinned fork build (maximum 180 seconds)'
curl --fail --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 10 --max-time 180 '@BASE@/'"$asset" -o "$backup/new-agent"
printf '%s  %s\n' "$digest" "$backup/new-agent" | sha256sum -c -
chmod --reference="$binary" "$backup/new-agent"
chown --reference="$binary" "$backup/new-agent"
echo '[3/6] Backing up program and service; startup parameters are unchanged'
cp -p "$binary" "$backup/old-agent"
systemctl cat "$unit" > "$backup/service.backup"
[ "$(systemctl show "$unit" -p MainPID --value)" = "$agent_pid" ] || { echo 'Agent changed during preparation; aborting without replacement'; exit 1; }
echo '[4/6] Stopping Agent and saving identity/traffic snapshots'
stopped=1
systemctl stop "$unit"
for f in net_static.json managed-traffic.json; do
  if [ -f "$workdir/$f" ]; then cp -p "$workdir/$f" "$backup/$f"; fi
done
if [ -f "$(dirname "$binary")/auto-discovery.json" ]; then cp -p "$(dirname "$binary")/auto-discovery.json" "$backup/auto-discovery.json"; fi
echo '[5/6] Replacing binary and restarting the same service'
mv -f "$backup/new-agent" "$binary"
systemctl start "$unit"
sleep 10
systemctl is-active --quiet "$unit"
new_pid=$(systemctl show "$unit" -p MainPID --value)
actual=$(sha256sum "/proc/$new_pid/exe" | cut -d ' ' -f 1)
[ "$actual" = "$digest" ] || { echo 'Running binary does not match target; rolling back'; exit 1; }
stopped=0
echo '[6/6] Target binary is running. Return to the panel and wait for version/traffic capability confirmation.'
echo 'No reinstall or registration was performed. Local reset day, identity and traffic history are preserved.'
echo 'Future version source: Hyphen-H/komari-agent; existing auto-update options are unchanged.'
