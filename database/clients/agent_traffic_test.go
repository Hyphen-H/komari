package clients

import (
	"github.com/komari-monitor/komari/database/models"
	"testing"
)

func TestTrafficConfigDesiredConfirmationAndRestart(t *testing.T) {
	db := classificationDB(t)
	if err := db.AutoMigrate(&models.AgentTrafficConfig{}); err != nil {
		t.Fatal(err)
	}
	initial, err := getAgentTrafficConfig(db, "a")
	if err != nil {
		t.Fatal(err)
	}
	if initial.Revision != "" || initial.Managed || initial.SeenAt != nil {
		t.Fatal("old node incorrectly marked supported or managed")
	}
	first, err := setAgentTrafficConfig(db, "a", true, 15)
	if err != nil {
		t.Fatal(err)
	}
	if first.AppliedRevision != "" {
		t.Fatal("save incorrectly reported as applied")
	}
	second, err := setAgentTrafficConfig(db, "a", true, 20)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := syncAgentTrafficConfig(db, "a", TrafficReport{Revision: first.Revision, Managed: true, Day: 15})
	if err != nil {
		t.Fatal(err)
	}
	if stale.Revision != second.Revision || stale.AppliedRevision == second.Revision {
		t.Fatal("late report acknowledged newer desired state")
	}
	failed, err := syncAgentTrafficConfig(db, "a", TrafficReport{Revision: first.Revision, Managed: true, Day: 15, FailedRevision: second.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if failed.Error == "" {
		t.Fatal("application failure not surfaced")
	}
	confirmed, err := syncAgentTrafficConfig(db, "a", TrafficReport{Revision: second.Revision, Managed: true, Day: 20})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.AppliedRevision != second.Revision || confirmed.Error != "" || confirmed.SeenAt == nil {
		t.Fatal("valid application not confirmed")
	}
	local, err := setAgentTrafficConfig(db, "a", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err = syncAgentTrafficConfig(db, "a", TrafficReport{Revision: local.Revision, Managed: false, Day: 7})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.AppliedRevision != local.Revision || confirmed.EffectiveDay != 7 {
		t.Fatal("local fallback confirmation failed")
	}
	if _, err := setAgentTrafficConfig(db, "a", true, 32); err == nil {
		t.Fatal("accepted invalid day")
	}
	if _, err := setAgentTrafficConfig(db, "missing", true, 15); err == nil {
		t.Fatal("accepted missing node")
	}
}
