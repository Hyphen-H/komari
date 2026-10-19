package clients

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TrafficReport struct {
	Revision       string `json:"revision"`
	Managed        bool   `json:"managed"`
	Day            int    `json:"day"`
	FailedRevision string `json:"failed_revision"`
}

func GetAgentTrafficConfig(id string) (*models.AgentTrafficConfig, error) {
	return getAgentTrafficConfig(dbcore.GetDBInstance(), id)
}

func getAgentTrafficConfig(db *gorm.DB, id string) (*models.AgentTrafficConfig, error) {
	var client models.Client
	if err := db.Select("uuid").First(&client, "uuid = ?", id).Error; err != nil {
		return nil, err
	}
	var config models.AgentTrafficConfig
	err := db.First(&config, "uuid = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &models.AgentTrafficConfig{UUID: id}, nil
	}
	return &config, err
}

func SetAgentTrafficConfig(id string, managed bool, day int) (*models.AgentTrafficConfig, error) {
	return setAgentTrafficConfig(dbcore.GetDBInstance(), id, managed, day)
}

func setAgentTrafficConfig(db *gorm.DB, id string, managed bool, day int) (*models.AgentTrafficConfig, error) {
	if day < 0 || day > 31 {
		return nil, fmt.Errorf("day must be an integer from 0 to 31")
	}
	if _, err := getAgentTrafficConfig(db, id); err != nil {
		return nil, err
	}
	if !managed {
		day = 0
	}
	config := models.AgentTrafficConfig{UUID: id, Revision: uuid.NewString(), Managed: managed, Day: day}
	err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "uuid"}}, DoUpdates: clause.AssignmentColumns([]string{"revision", "managed", "day", "error"})}).Create(&config).Error
	if err != nil {
		return nil, err
	}
	return getAgentTrafficConfig(db, id)
}

// Read desired state after recording a report. A late report can update the last
// observed value, but never acknowledges or replaces a newer desired revision.
func SyncAgentTrafficConfig(id string, report TrafficReport) (*models.AgentTrafficConfig, error) {
	return syncAgentTrafficConfig(dbcore.GetDBInstance(), id, report)
}

func syncAgentTrafficConfig(db *gorm.DB, id string, report TrafficReport) (*models.AgentTrafficConfig, error) {
	if report.Day < 0 || report.Day > 31 || len(report.Revision) > 36 || len(report.FailedRevision) > 36 {
		return nil, fmt.Errorf("invalid traffic report")
	}
	config, err := getAgentTrafficConfig(db, id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	initial := models.AgentTrafficConfig{UUID: id, SeenAt: &now, EffectiveDay: report.Day, EffectiveManaged: report.Managed}
	if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "uuid"}}, DoUpdates: clause.AssignmentColumns([]string{"seen_at", "effective_day", "effective_managed"})}).Create(&initial).Error; err != nil {
		return nil, err
	}
	if config.Revision != "" {
		query := db.Model(&models.AgentTrafficConfig{}).Where("uuid = ? AND revision = ?", id, config.Revision)
		if report.Revision == config.Revision && report.Managed == config.Managed && (!config.Managed || report.Day == config.Day) {
			if err := query.Updates(map[string]any{"applied_revision": config.Revision, "error": ""}).Error; err != nil {
				return nil, err
			}
		} else if report.FailedRevision == config.Revision {
			if err := query.Update("error", "Agent could not persist or apply traffic configuration").Error; err != nil {
				return nil, err
			}
		}
	}
	return getAgentTrafficConfig(db, id)
}
