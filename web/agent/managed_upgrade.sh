set -eu
callback=@CALLBACK@
callback_token=@TOKEN@
report() {
  if [ -n "$callback" ] && command -v curl >/dev/null; then
    printf 'header = "Authorization: Bearer %s"\n' "$callback_token" | curl --config - --fail --silent --show-error --connect-timeout 3 --max-time 5 -H 'Content-Type: application/json' --data "{\"stage\":\"$1\"}" "$callback" >/dev/null 2>&1 || true
  fi
}
trap 'report failed' EXIT
report received
[ "$(uname -s)" = Linux ] || { echo 'Only Linux systemd is supported'; exit 1; }
[ "$(id -u)" = 0 ] || { echo 'Root service is required'; exit 1; }
for tool in systemctl systemd-run readlink curl sha256sum mktemp; do
  command -v "$tool" >/dev/null || { echo "Missing required tool: $tool"; exit 1; }
done
[ ! -f /.dockerenv ] && [ ! -f /.komari-agent-container ] || { echo 'Container self-upgrade is not supported'; exit 1; }
agent_pid=$PPID
binary=$(readlink -f "/proc/$agent_pid/exe")
workdir=$(readlink -f "/proc/$agent_pid/cwd")
[ -f "$binary" ] && [ -x "$binary" ] || { echo 'Cannot resolve Agent executable'; exit 1; }
unit=$(sed -n 's@.*/\([^/]*\.service\)\(/.*\)\?$@\1@p' "/proc/$agent_pid/cgroup" | head -n 1)
[ -n "$unit" ] || { echo 'Agent must run in a systemd service'; exit 1; }
[ "$(systemctl show "$unit" -p MainPID --value)" = "$agent_pid" ] || { echo 'Service identity does not match Agent'; exit 1; }
[ "$(systemctl show "$unit" -p Transient --value)" = no ] || { echo 'Transient systemd units cannot be restarted safely'; exit 1; }
lock="$(dirname "$binary")/.komari-managed-upgrade.lock"
mkdir "$lock" 2>/dev/null || { echo 'Another upgrade is already staged; inspect existing upgrade before retrying'; exit 1; }
trap 'report failed; rmdir "$lock" 2>/dev/null || true' EXIT
case $(uname -m) in
  x86_64) asset=komari-agent-linux-amd64; digest='@AMD64@' ;;
  aarch64|arm64) asset=komari-agent-linux-arm64; digest='@ARM64@' ;;
  *) echo 'Only amd64/arm64 is supported'; exit 1 ;;
esac
umask 077
backup=$(mktemp -d "$(dirname "$binary")/.komari-upgrade-XXXXXXXX")
report downloading
curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 --connect-timeout 10 --max-time 180 '@BASE@/'"$asset" -o "$backup/new-agent"
printf '%s  %s\n' "$digest" "$backup/new-agent" | sha256sum -c - >/dev/null || { echo 'SHA256 verification failed; nothing replaced'; exit 1; }
report verified
chmod --reference="$binary" "$backup/new-agent"
chown --reference="$binary" "$backup/new-agent"
cp -p "$binary" "$backup/old-agent"
systemctl cat "$unit" > "$backup/service.backup"
printf '%s\n' "$callback" > "$backup/progress-url"
printf '%s\n' "$callback_token" > "$backup/progress-token"
cat > "$backup/apply.sh" <<'KOMARI_APPLY'
set -eu
unit=$1; binary=$2; backup=$3; workdir=$4
callback=$(cat "$backup/progress-url")
callback_token=$(cat "$backup/progress-token")
report() {
  if [ -n "$callback" ]; then
    printf 'header = "Authorization: Bearer %s"\n' "$callback_token" | curl --config - --fail --silent --show-error --connect-timeout 3 --max-time 5 -H 'Content-Type: application/json' --data "{\"stage\":\"$1\"}" "$callback" >/dev/null 2>&1 || true
  fi
}
lock="$(dirname "$binary")/.komari-managed-upgrade.lock"
sleep 3
report restarting
restore() {
  systemctl stop "$unit" || true
  cp -p "$backup/old-agent" "$backup/restore-agent"
  mv -f "$backup/restore-agent" "$binary"
  systemctl start "$unit" || true
  rmdir "$lock" 2>/dev/null || true
  echo 'Upgrade failed; original binary restored'
  report rolled_back
}
trap restore EXIT
systemctl stop "$unit"
for f in net_static.json managed-traffic.json; do
  if [ -f "$workdir/$f" ]; then cp -p "$workdir/$f" "$backup/$f"; fi
done
if [ -f "$(dirname "$binary")/auto-discovery.json" ]; then cp -p "$(dirname "$binary")/auto-discovery.json" "$backup/auto-discovery.json"; fi
mv -f "$backup/new-agent" "$binary"
systemctl start "$unit"
sleep 10
systemctl is-active --quiet "$unit"
report service_active
trap - EXIT
rmdir "$lock" 2>/dev/null || true
echo 'Binary replaced and service active; verify version and traffic capability in panel'
KOMARI_APPLY
job="komari-upgrade-$(date +%s)-$$"
report staged
systemd-run --quiet --unit="$job" --property=Type=exec -- /bin/sh -c 'exec /bin/sh "$1/apply.sh" "$2" "$3" "$1" "$4" >"$1/upgrade.log" 2>&1' sh "$backup" "$unit" "$binary" "$workdir"
trap - EXIT
echo "Upgrade scheduled; backup: $backup. Wait for Agent version/capability confirmation."
