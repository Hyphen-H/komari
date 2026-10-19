package agent

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/komari-monitor/komari/database/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpgradeProgressCapabilityAndTransitions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "progress.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err = db.AutoMigrate(&models.AgentUpgradeJob{}, &models.AgentUpgradeEvent{}); err != nil {
		t.Fatal(err)
	}
	job := models.AgentUpgradeJob{ID: "job-a", UUID: "node-a", Stage: "queued", TokenHash: "hash-secret"}
	if err = db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, hash, stage string }{
		{"job-a", "wrong", "received"}, {"job-other", "hash-secret", "received"}, {"job-a", "hash-secret", "confirmed"}, {"job-a", "hash-secret", "unknown"},
	} {
		if recordUpgradeProgress(db, tc.id, tc.hash, tc.stage) == nil {
			t.Fatalf("accepted invalid callback: %+v", tc)
		}
	}
	if err = recordUpgradeProgress(db, "job-a", "hash-secret", "downloading"); err != nil {
		t.Fatal(err)
	} // skipped callback allowed
	if recordUpgradeProgress(db, "job-a", "hash-secret", "received") == nil {
		t.Fatal("allowed backwards progress")
	}
	if err = recordUpgradeProgress(db, "job-a", "hash-secret", "failed"); err != nil {
		t.Fatal(err)
	}
	if recordUpgradeProgress(db, "job-a", "hash-secret", "service_active") == nil {
		t.Fatal("reopened failed job")
	}
	var events []models.AgentUpgradeEvent
	db.Find(&events)
	if len(events) != 2 {
		t.Fatalf("invalid callbacks generated events: %d", len(events))
	}
	raw, _ := json.Marshal(job)
	if strings.Contains(string(raw), "hash-secret") {
		t.Fatal("callback secret exposed")
	}
	old := models.AgentUpgradeJob{ID: "expired", UUID: "node-a", Stage: "queued", TokenHash: "hash-secret", CreatedAt: time.Now().UTC().Add(-16 * time.Minute)}
	db.Create(&old)
	if recordUpgradeProgress(db, "expired", "hash-secret", "received") == nil {
		t.Fatal("expired capability accepted")
	}
}
