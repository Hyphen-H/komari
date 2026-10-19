package agent

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"
)

//go:embed manual_upgrade.sh
var manualUpgradeTemplate string

var agentServiceName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.@-]{0,127}$`)

// SSH command only. It neither dispatches an RPC nor embeds node credentials.
func ManualAgentUpgradeCommand(service string) (string, error) {
	service = strings.TrimSpace(service)
	if service == "" {
		service = "komari-agent"
	}
	if !agentServiceName.MatchString(service) {
		return "", fmt.Errorf("invalid systemd service name")
	}
	if !strings.HasSuffix(service, ".service") {
		service += ".service"
	}
	script := strings.NewReplacer(
		"@BASE@", "https://github.com/Hyphen-H/komari-agent/releases/download/"+ManagedAgentRelease,
		"@AMD64@", "0b49417c5969d87b25e702d5b04de0d115420f5a74c1f6126177a3e3921dcb79",
		"@ARM64@", "bfc4ee24d0316bbbdc981fe0136681dad22137523104eed9e9aa330715a433cb",
	).Replace(manualUpgradeTemplate)
	// The script is a quoted here-document: SSH shell variables are not expanded
	// until the root shell runs. Non-root users require their usual sudo access.
	return "sh -c 'if [ \"$(id -u)\" -eq 0 ]; then exec sh \"$@\"; else exec sudo sh \"$@\"; fi' sh -s -- '" + service + "' <<'KOMARI_REPLACE_AGENT'\n" + script + "\nKOMARI_REPLACE_AGENT", nil
}
