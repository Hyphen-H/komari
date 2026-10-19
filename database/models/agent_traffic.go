package models

import "time"

// Kept separate from public node metadata and generic client update maps.
type AgentTrafficConfig struct {
	UUID             string     `json:"uuid" gorm:"type:varchar(36);primaryKey"`
	Revision         string     `json:"revision" gorm:"type:varchar(36)"`
	Managed          bool       `json:"managed"`
	Day              int        `json:"day"`
	AppliedRevision  string     `json:"applied_revision" gorm:"type:varchar(36)"`
	EffectiveDay     int        `json:"effective_day"`
	EffectiveManaged bool       `json:"effective_managed"`
	Error            string     `json:"error"`
	SeenAt           *time.Time `json:"seen_at"`
}
