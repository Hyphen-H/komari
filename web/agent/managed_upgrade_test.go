package agent

import (
	"os/exec"
	"strings"
	"testing"
)

func TestManagedUpgradeCommandSafety(t *testing.T) {
	command := ManagedAgentUpgradeCommand()
	if strings.Contains(command, "@BASE@") || strings.Contains(command, "@AMD64@") || strings.Contains(command, "@ARM64@") {
		t.Fatal("unexpanded release placeholders")
	}
	for _, required := range []string{"sha256sum -c", ManagedAgentRelease, "Service identity does not match Agent", "systemctl stop", "old-agent", "auto-discovery.json", "net_static.json", "managed-traffic.json", "restore", ".komari-managed-upgrade.lock"} {
		if !strings.Contains(command, required) {
			t.Fatalf("missing safety step: %s", required)
		}
	}
	for _, forbidden := range []string{"curl |", "curl -k", "--auto-discovery ", "rm -rf", "daemon-reload", "ExecStart="} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("unexpected unsafe reinstall behavior: %s", forbidden)
		}
	}
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(command)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("invalid shell: %v %s", err, out)
	}
}

func TestManagedUpgradeHonorsRemoteControl(t *testing.T) {
	if ValidateManagedAgentConfig(map[string]any{}) == nil {
		t.Fatal("unknown capability must fail closed")
	}
	if ValidateManagedAgentConfig(map[string]any{"disable_web_ssh": true}) == nil {
		t.Fatal("must reject disabled remote control")
	}
	if err := ValidateManagedAgentConfig(map[string]any{"disable_web_ssh": false}); err != nil {
		t.Fatal(err)
	}
}
