package agent

import (
	"os/exec"
	"strings"
	"testing"
)

func TestManualUpgradeCommand(t *testing.T) {
	for _, service := range []string{"", "komari-agent", "custom-agent.service", "komari@node"} {
		command, err := ManualAgentUpgradeCommand(service)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"sha256sum -c", "systemctl show", "MainPID", "old-agent", "auto-discovery.json", "net_static.json", "managed-traffic.json", "ROLLBACK", ManagedAgentRelease, "[6/6]",
			// 防止在 Agent 自己的终端/执行上下文里运行：停止 Agent 会因此杀掉脚本。
			"ControlGroup", "拒绝执行", "manual-upgrade.log", "trap '' PIPE", "trap 'exit 129' HUP", "rmdir '$lock'"} {
			if !strings.Contains(command, want) {
				t.Fatalf("missing %s", want)
			}
		}
		for _, bad := range []string{"$PPID", "@AMD64@", "@ARM64@", "@BASE@", "--auto-discovery ", "callback_token", "systemd-run", "curl -k", "rm -rf"} {
			if strings.Contains(command, bad) {
				t.Fatalf("not an SSH-only safe command: %s", bad)
			}
		}
		cmd := exec.Command("sh", "-n")
		cmd.Stdin = strings.NewReader(command)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("invalid shell: %s %v", out, err)
		}
	}
	for _, service := range []string{"-evil", "x; touch /tmp/bad", "x\nwhoami", "$(id)", "../x", "x'"} {
		if _, err := ManualAgentUpgradeCommand(service); err == nil {
			t.Fatalf("accepted injection %q", service)
		}
	}
}
