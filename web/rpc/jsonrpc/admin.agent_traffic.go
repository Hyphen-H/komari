package jsonrpc

import (
	"context"
	"github.com/komari-monitor/komari/database/auditlog"
	"github.com/komari-monitor/komari/database/clients"
	"github.com/komari-monitor/komari/pkg/rpc"
)

func init() {
	RegisterWithGroupAndMeta("getAgentTrafficConfig", rpc.RoleAdmin, adminGetAgentTrafficConfig, &rpc.MethodMeta{Name: "admin:getAgentTrafficConfig", Summary: "Get desired and last confirmed Agent traffic reset configuration"})
	RegisterWithGroupAndMeta("setAgentTrafficConfig", rpc.RoleAdmin, adminSetAgentTrafficConfig, &rpc.MethodMeta{Name: "admin:setAgentTrafficConfig", Summary: "Persist desired Agent traffic reset configuration; apply is asynchronous"})
}

func adminGetAgentTrafficConfig(_ context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var p struct {
		UUID string `json:"uuid"`
	}
	if req.BindParams(&p) != nil || p.UUID == "" {
		return nil, rpc.MakeError(rpc.InvalidParams, "uuid is required", nil)
	}
	config, err := clients.GetAgentTrafficConfig(p.UUID)
	if err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, "node not found", nil)
	}
	return config, nil
}

func adminSetAgentTrafficConfig(ctx context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var p struct {
		UUID    string `json:"uuid"`
		Managed *bool  `json:"managed"`
		Day     *int   `json:"day"`
	}
	if req.BindParams(&p) != nil || p.UUID == "" || p.Managed == nil || p.Day == nil {
		return nil, rpc.MakeError(rpc.InvalidParams, "uuid, managed and day are required", nil)
	}
	config, err := clients.SetAgentTrafficConfig(p.UUID, *p.Managed, *p.Day)
	if err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, "invalid node or reset day (0–31)", nil)
	}
	actor, ip := auditActor(ctx)
	auditlog.Log(ip, actor, "set agent traffic reset configuration:"+p.UUID, "info")
	return config, nil
}
