package models

import "time"

type AgentUpgradeJob struct {
	ID        string    `json:"id" gorm:"primaryKey;type:varchar(36)"`
	UUID      string    `json:"uuid" gorm:"index;type:varchar(36)"`
	Stage     string    `json:"stage"`
	TokenHash string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AgentUpgradeEvent struct {
	ID        uint      `json:"-" gorm:"primaryKey"`
	JobID     string    `json:"-" gorm:"index;type:varchar(36)"`
	Stage     string    `json:"stage"`
	CreatedAt time.Time `json:"created_at"`
}
