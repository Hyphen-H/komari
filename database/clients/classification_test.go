package clients

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/komari-monitor/komari/database/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func classificationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "clients.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	if err := db.AutoMigrate(&models.Client{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := db.Create(&models.Client{UUID: id, Token: id, Group: "old", Tags: "ISP;NAT"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestClassificationBatchAtomicAndPreservesOtherFields(t *testing.T) {
	db := classificationDB(t)
	err := saveClientClassifications(db, []map[string]string{
		{"uuid": "a", "group": " new ", "expected_group": "old", "token": "must-not-change"},
		{"uuid": "b", "tags": "ISP;ISP;new;;", "expected_tags": "ISP;NAT"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var a, b models.Client
	db.First(&a, "uuid = ?", "a")
	db.First(&b, "uuid = ?", "b")
	if a.Group != "new" || a.Token != "a" || a.Tags != "ISP;NAT" || b.Tags != "ISP;new" {
		t.Fatalf("unexpected classification update: a=%+v b=%+v", a, b)
	}
}

func TestClassificationBatchRollsBackOnConflict(t *testing.T) {
	db := classificationDB(t)
	err := saveClientClassifications(db, []map[string]string{
		{"uuid": "a", "group": "new", "expected_group": "old"},
		{"uuid": "b", "tags": "new", "expected_tags": "stale snapshot"},
	})
	if err == nil {
		t.Fatal("expected concurrent modification conflict")
	}
	var a models.Client
	db.First(&a, "uuid = ?", "a")
	if a.Group != "old" {
		t.Fatal("first update was not rolled back")
	}
}

func TestClassificationManualRegionMode(t *testing.T) {
	db := classificationDB(t)
	if err := saveClientClassifications(db, []map[string]string{{"uuid": "a", "region": "", "expected_region": ""}}); err != nil {
		t.Fatal(err)
	}
	var client models.Client
	if err := db.First(&client, "uuid = ?", "a").Error; err != nil {
		t.Fatal(err)
	}
	if !client.RegionManual {
		t.Fatal("explicit clear should lock manual region")
	}
	if err := saveClientClassifications(db, []map[string]string{{"uuid": "a", "region": "US", "expected_region": "", "expected_region_manual": "false"}}); err == nil {
		t.Fatal("expected region mode conflict")
	}
}

func TestClassificationRejectsInvalidBatches(t *testing.T) {
	db := classificationDB(t)
	for _, updates := range [][]map[string]string{
		nil,
		{{"uuid": "a", "group": "new"}},
		{{"uuid": "a", "token": "bad"}},
		{{"uuid": "a", "group": strings.Repeat("长", 101), "expected_group": "old"}},
		{{"uuid": "missing", "group": "new", "expected_group": "old"}},
		{{"uuid": "a", "group": "new", "expected_group": "old"}, {"uuid": "a", "group": "other", "expected_group": "new"}},
	} {
		if err := saveClientClassifications(db, updates); err == nil {
			t.Fatalf("accepted invalid batch: %v", updates)
		}
	}
}
