package client

import (
	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/database/clients"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/internal/config"
	"github.com/komari-monitor/komari/utils"
	logger "github.com/komari-monitor/komari/utils/log"
	agent_runtime "github.com/komari-monitor/komari/web/agent"
	"github.com/komari-monitor/komari/web/api"
)

func RegisterClient(c *gin.Context) {
	auth := c.GetHeader("Authorization")
	if auth == "" {
		api.RespondError(c, 403, "Invalid AutoDiscovery Key")
		return
	}
	AutoDiscoveryKey, err := config.GetAs[string](config.AutoDiscoveryKeyKey, "")
	if err != nil {
		api.RespondError(c, 500, "Failed to get AutoDiscovery Key: "+err.Error())
		return
	}
	if AutoDiscoveryKey == "" ||
		len(AutoDiscoveryKey) < 12 ||
		"Bearer "+AutoDiscoveryKey != auth {

		api.RespondError(c, 403, "Invalid AutoDiscovery Key")
		return
	}
	host := c.Query("name")
	if host == "" {
		host = utils.GenerateRandomString(8)
	}

	// 幂等注册：Agent 重装/更新会丢失安装目录下的 auto-discovery.json，
	// 随后重新注册。若该主机仍有离线节点，则复用原节点身份，避免每次重装都
	// 多出一个 "Auto-<hostname>" 重复节点，并保留管理员自定义的节点名与历史数据。
	if existing, ok := reclaimAutoDiscoveryClient(host); ok {
		api.RespondSuccess(c, gin.H{"uuid": existing.UUID, "token": existing.Token, "reclaimed": true})
		return
	}

	uuid, token, err := clients.CreateAutoDiscoveryClient(host)
	if err != nil {
		api.RespondError(c, 500, "Failed to create client: "+err.Error())
		return
	}
	api.RespondSuccess(c, gin.H{"uuid": uuid, "token": token})
}

// reclaimAutoDiscoveryClient 查找可复用的自动发现节点。
// 仅当候选唯一、且当前不在线时才复用：在线的同名节点更可能是另一台主机
// （例如多台默认主机名相同的机器），此时保持原行为新建节点，避免抢占其身份。
func reclaimAutoDiscoveryClient(host string) (*models.Client, bool) {
	candidates, err := clients.FindAutoDiscoveryClients(host)
	if err != nil {
		logger.Warnf("client-api", "failed to query auto-discovery candidates for host %q: %v", host, err)
		return nil, false
	}
	var offline []models.Client
	for i := range candidates {
		if !agent_runtime.IsAgentOnline(candidates[i].UUID) {
			offline = append(offline, candidates[i])
		}
	}
	if len(offline) != 1 {
		return nil, false
	}
	logger.InfoArgs("client-api", "reclaimed auto-discovery node", offline[0].UUID, "for host", host)
	return &offline[0], true
}
