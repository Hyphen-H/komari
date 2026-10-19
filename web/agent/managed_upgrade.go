package agent

import (
	_ "embed"
	"fmt"
	"strings"
)

const ManagedAgentVersion = "dev-managed-traffic-db3c5e8"
const ManagedAgentRelease = "managed-traffic-db3c5e8"

//go:embed managed_upgrade.sh
var managedUpgradeTemplate string

// Fixed, reviewed assets only; no URL or shell text is accepted from the client.
func ManagedAgentUpgradeCommand() string {
	return ManagedAgentUpgradeCommandWithProgress("", "")
}

func ManagedAgentUpgradeCommandWithProgress(callback, token string) string {
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	return strings.NewReplacer(
		"@BASE@", "https://github.com/Hyphen-H/komari-agent/releases/download/"+ManagedAgentRelease,
		"@AMD64@", "0b49417c5969d87b25e702d5b04de0d115420f5a74c1f6126177a3e3921dcb79",
		"@ARM64@", "bfc4ee24d0316bbbdc981fe0136681dad22137523104eed9e9aa330715a433cb",
		"@CALLBACK@", quote(callback), "@TOKEN@", quote(token),
	).Replace(managedUpgradeTemplate)
}

func ValidateManagedAgentConfig(config map[string]any) error {
	disabled, known := config["disable_web_ssh"].(bool)
	if !known {
		return fmt.Errorf("Agent 未提供远程控制能力状态，未发送升级命令")
	}
	if disabled {
		return fmt.Errorf("Agent 已禁用远程控制，无法远程升级")
	}
	return nil
}
