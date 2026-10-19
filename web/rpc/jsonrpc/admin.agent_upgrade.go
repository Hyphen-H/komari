package jsonrpc

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/komari-monitor/komari/database/auditlog"
	"github.com/komari-monitor/komari/database/clients"
	"github.com/komari-monitor/komari/database/tasks"
	"github.com/komari-monitor/komari/pkg/rpc"
	v2 "github.com/komari-monitor/komari/protocol/v2"
	agent "github.com/komari-monitor/komari/web/agent"
)

func init() {
	RegisterWithGroupAndMeta("getAgentUpgradeStatus", rpc.RoleAdmin, adminGetAgentUpgradeStatus, &rpc.MethodMeta{Name: "admin:getAgentUpgradeStatus", Summary: "Get version and remote traffic capability without credentials"})
	RegisterWithGroupAndMeta("upgradeManagedAgent", rpc.RoleAdmin, adminUpgradeManagedAgent, &rpc.MethodMeta{Name: "admin:upgradeManagedAgent", Summary: "Send a verified fixed-release upgrade command to one online Agent"})
	rpc.MarkSensitive("admin:upgradeManagedAgent")
}

func adminGetAgentUpgradeStatus(_ context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var p struct {
		UUID string `json:"uuid"`
	}
	if req.BindParams(&p) != nil || p.UUID == "" {
		return nil, rpc.MakeError(rpc.InvalidParams, "uuid is required", nil)
	}
	client, err := clients.GetClientByUUID(p.UUID)
	if err != nil {
		return nil, rpc.MakeError(rpc.NotFound, "node not found", nil)
	}
	traffic, _ := clients.GetAgentTrafficConfig(p.UUID)
	supported := traffic != nil && traffic.SeenAt != nil && time.Since(*traffic.SeenAt) < time.Minute
	response := map[string]any{"version": client.Version, "online": agent.IsAgentOnline(p.UUID), "traffic_supported": supported, "managed_version": agent.ManagedAgentVersion, "managed_release": agent.ManagedAgentRelease, "repository": "Hyphen-H/komari-agent"}
	if job, err := agent.LatestUpgradeProgress(p.UUID); err == nil {
		if job.Stage == "service_active" && client.Version == agent.ManagedAgentVersion && supported && traffic.SeenAt.After(job.CreatedAt) {
			agent.MarkUpgradeStage(job.ID, "confirmed")
			job.Stage = "confirmed"
		}
		if result, err := tasks.GetSpecificTaskResult(job.ID, p.UUID); err == nil && result.ExitCode != nil && *result.ExitCode != 0 && job.Stage != "confirmed" && job.Stage != "rolled_back" {
			agent.MarkUpgradeStage(job.ID, "failed")
			job.Stage = "failed"
		}
		response["upgrade_job"] = job
		response["stage_label"] = agent.UpgradeStageLabels[job.Stage]
		response["upgrade_events"] = agent.UpgradeEvents(job.ID)
		if result, err := tasks.GetSpecificTaskResult(job.ID, p.UUID); err == nil && result.ExitCode != nil {
			output := result.Result
			if len(output) > 2000 {
				output = output[:2000]
			}
			response["task_output"] = output
			response["task_exit_code"] = *result.ExitCode
		}
	}
	return response, nil
}

func adminUpgradeManagedAgent(ctx context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var p struct {
		UUID    string `json:"uuid"`
		Confirm bool   `json:"confirm"`
	}
	if req.BindParams(&p) != nil || p.UUID == "" || !p.Confirm {
		return nil, rpc.MakeError(rpc.InvalidParams, "confirm=true and uuid are required", nil)
	}
	if !agent.IsAgentOnline(p.UUID) {
		return nil, rpc.MakeError(rpc.Unavailable, "Agent 离线，未发送升级命令", nil)
	}
	if previous, err := agent.LatestUpgradeProgress(p.UUID); err == nil && time.Since(previous.CreatedAt) < 5*time.Minute && previous.Stage != "confirmed" && previous.Stage != "failed" && previous.Stage != "rolled_back" {
		return nil, rpc.MakeError(rpc.InvalidParams, "此节点已有升级进行中，请查看升级控制台，不要重复发送", nil)
	}
	config, err := agent.GetStartupConfig(ctx, p.UUID)
	if err != nil {
		return nil, rpc.MakeError(rpc.Unavailable, "无法读取 Agent 能力，请确认 Agent 在线且支持启动配置查询", nil)
	}
	if err = agent.ValidateManagedAgentConfig(config); err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, err.Error(), nil)
	}
	client, err := clients.GetClientByUUID(p.UUID)
	if err != nil {
		return nil, rpc.MakeError(rpc.NotFound, "node not found", nil)
	}
	arch := strings.ToLower(client.Arch)
	osName := strings.ToLower(client.OS)
	if strings.Contains(osName, "windows") || strings.Contains(osName, "darwin") || strings.Contains(osName, "macos") || strings.Contains(osName, "freebsd") {
		return nil, rpc.MakeError(rpc.InvalidParams, "定制远程升级仅支持 Linux systemd 服务", nil)
	}
	if arch != "amd64" && arch != "arm64" && arch != "x86_64" && arch != "aarch64" {
		return nil, rpc.MakeError(rpc.InvalidParams, "定制升级仅支持 Linux amd64/arm64 systemd 服务", nil)
	}
	id := uuid.NewString()
	endpoint, _ := config["endpoint"].(string)
	parsed, parseErr := url.Parse(endpoint)
	if parseErr != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, "Agent 面板地址无效，无法接收升级进度", nil)
	}
	callback := strings.TrimRight(endpoint, "/") + "/api/clients/upgrade-progress/" + id
	progressToken, err := agent.NewUpgradeProgress(id, p.UUID)
	if err != nil {
		return nil, rpc.MakeError(rpc.InternalError, "failed to create progress job", nil)
	}
	command := agent.ManagedAgentUpgradeCommandWithProgress(callback, progressToken)
	// Persist the auditable template, not the short-lived callback capability.
	if err = tasks.CreateTask(id, []string{p.UUID}, agent.ManagedAgentUpgradeCommand()); err != nil {
		agent.MarkUpgradeStage(id, "failed")
		return nil, rpc.MakeError(rpc.InternalError, "failed to create upgrade task", nil)
	}
	if !agent.DispatchV2Event(p.UUID, v2.MethodAgentExec, v2.ExecParams{TaskID: id, Command: command}) {
		agent.MarkUpgradeStage(id, "failed")
		tasks.SaveTaskResult(id, p.UUID, "Agent disconnected before dispatch", -1, time.Now().UTC())
		return nil, rpc.MakeError(rpc.Unavailable, "Agent 已断开，未发送升级", nil)
	}
	actor, ip := auditActor(ctx)
	auditlog.Log(ip, actor, "managed Agent upgrade:"+p.UUID+" task:"+id, "warn")
	return map[string]any{"queued": true, "task_id": id, "target_version": agent.ManagedAgentVersion}, nil
}
