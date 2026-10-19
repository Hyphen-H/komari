package agent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"gorm.io/gorm"
)

var UpgradeStageLabels = map[string]string{
	"queued": "命令已下发，等待目标机接收", "received": "目标机已接收，检查运行环境",
	"downloading": "正在下载固定版本（最多180秒）", "verified": "SHA-256校验通过",
	"staged": "旧程序已备份，准备独立升级任务", "restarting": "正在停止Agent并替换程序",
	"service_active": "服务已启动，等待目标版本与能力确认", "confirmed": "目标版本和流量同步能力已确认",
	"failed": "升级失败，请查看任务输出；未确认成功", "rolled_back": "启动失败，已尝试恢复旧程序",
}

var upgradePredecessors = map[string][]string{
	"received": {"queued"}, "downloading": {"received"}, "verified": {"downloading"}, "staged": {"verified"}, "restarting": {"staged"}, "service_active": {"restarting"},
	"failed": {"queued", "received", "downloading", "verified", "staged", "restarting", "service_active"}, "rolled_back": {"staged", "restarting", "service_active"},
}

func init() {
	// Network loss may drop one callback. Allow later real stages to advance,
	// but never regress, fabricate confirmation, or reopen a terminal job.
	stages := []string{"queued", "received", "downloading", "verified", "staged", "restarting", "service_active"}
	for i := 1; i < len(stages); i++ {
		upgradePredecessors[stages[i]] = stages[:i]
	}
}

func NewUpgradeProgress(id, uuid string) (string, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(random[:])
	hash := sha256.Sum256([]byte(token))
	err := dbcore.GetDBInstance().Create(&models.AgentUpgradeJob{ID: id, UUID: uuid, Stage: "queued", TokenHash: hex.EncodeToString(hash[:]), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}).Error
	if err == nil {
		dbcore.GetDBInstance().Create(&models.AgentUpgradeEvent{JobID: id, Stage: "queued", CreatedAt: time.Now().UTC()})
	}
	return token, err
}

func UpgradeProgressCallback(c *gin.Context) {
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if len(token) != 64 {
		c.Status(http.StatusUnauthorized)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512)
	var body struct {
		Stage string `json:"stage"`
	}
	if json.NewDecoder(c.Request.Body).Decode(&body) != nil || len(upgradePredecessors[body.Stage]) == 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	hash := sha256.Sum256([]byte(token))
	err := recordUpgradeProgress(dbcore.GetDBInstance(), c.Param("id"), hex.EncodeToString(hash[:]), body.Stage)
	if err != nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	c.Status(http.StatusNoContent)
}

func recordUpgradeProgress(db *gorm.DB, id, hash, stage string) error {
	if len(upgradePredecessors[stage]) == 0 {
		return fmt.Errorf("invalid stage")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.AgentUpgradeJob{}).
			Where("id = ? AND token_hash = ? AND created_at > ? AND stage IN ?", id, hash, time.Now().UTC().Add(-15*time.Minute), upgradePredecessors[stage]).
			Updates(map[string]any{"stage": stage, "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("invalid or expired callback")
		}
		return tx.Create(&models.AgentUpgradeEvent{JobID: id, Stage: stage, CreatedAt: time.Now().UTC()}).Error
	})
}

func LatestUpgradeProgress(uuid string) (*models.AgentUpgradeJob, error) {
	var job models.AgentUpgradeJob
	if err := dbcore.GetDBInstance().Where("uuid = ?", uuid).Order("created_at desc").First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func MarkUpgradeStage(id, stage string) error {
	if UpgradeStageLabels[stage] == "" {
		return fmt.Errorf("unknown upgrade stage")
	}
	return dbcore.GetDBInstance().Transaction(func(tx *gorm.DB) error {
		r := tx.Model(&models.AgentUpgradeJob{}).Where("id = ? AND stage <> ?", id, stage).Updates(map[string]any{"stage": stage, "updated_at": time.Now().UTC()})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected == 0 {
			return nil
		}
		return tx.Create(&models.AgentUpgradeEvent{JobID: id, Stage: stage, CreatedAt: time.Now().UTC()}).Error
	})
}

func UpgradeEvents(id string) []models.AgentUpgradeEvent {
	var events []models.AgentUpgradeEvent
	dbcore.GetDBInstance().Where("job_id = ?", id).Order("id asc").Limit(50).Find(&events)
	return events
}
