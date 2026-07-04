package memberruntime

import (
	"testing"
	"time"
)

func TestPRSLR004ParticipantSessionsReuseWithinCouncilAndIsolateAcrossCouncils(t *testing.T) {
	now := time.Date(2026, 7, 4, 18, 0, 0, 0, time.UTC)
	registry := NewParticipantSessionRegistry()

	first, err := registry.OpenCouncilSessions("council-a", []string{"agent-1", "agent-2"}, now)
	if err != nil {
		t.Fatalf("OpenCouncilSessions council-a: %v", err)
	}
	second, err := registry.OpenCouncilSessions("council-a", []string{"agent-2", "agent-1"}, now.Add(time.Second))
	if err != nil {
		t.Fatalf("OpenCouncilSessions repeat council-a: %v", err)
	}
	other, err := registry.OpenCouncilSessions("council-b", []string{"agent-1", "agent-2"}, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("OpenCouncilSessions council-b: %v", err)
	}

	if len(first) != 2 || len(second) != 2 || len(other) != 2 {
		t.Fatalf("expected two participant sessions per council: first=%#v second=%#v other=%#v", first, second, other)
	}
	if first["agent-1"].Handle == "" || first["agent-2"].Handle == "" {
		t.Fatalf("participant handles must be non-empty: %#v", first)
	}
	if first["agent-1"].Handle != second["agent-1"].Handle || first["agent-2"].Handle != second["agent-2"].Handle {
		t.Fatalf("same council/member must reuse participant session handles: first=%#v second=%#v", first, second)
	}
	if first["agent-1"].Generation != 1 || second["agent-1"].Generation != 1 {
		t.Fatalf("same council/member reuse must not bump generation: first=%#v second=%#v", first["agent-1"], second["agent-1"])
	}
	if first["agent-1"].Handle == other["agent-1"].Handle || first["agent-2"].Handle == other["agent-2"].Handle {
		t.Fatalf("participant session handles must not be reused across councils: first=%#v other=%#v", first, other)
	}
}

func TestPRSLR004ParticipantSessionCursorObserveRejectsStaleAndWrongEvent(t *testing.T) {
	now := time.Date(2026, 7, 4, 18, 0, 0, 0, time.UTC)
	registry := NewParticipantSessionRegistry()
	if _, err := registry.OpenCouncilSessions("council-a", []string{"agent-1"}, now); err != nil {
		t.Fatalf("OpenCouncilSessions: %v", err)
	}
	if err := registry.ObserveCursor("council-a", "agent-1", "cur_000000000003_evt_speech", "evt_speech", "observe_delta", now.Add(time.Second)); err != nil {
		t.Fatalf("ObserveCursor fresh: %v", err)
	}
	if err := registry.ObserveCursor("council-a", "agent-1", "cur_000000000002_evt_old", "evt_old", "observe_delta", now.Add(2*time.Second)); err == nil {
		t.Fatalf("stale cursor regression must fail closed")
	}
	if err := registry.ObserveCursor("council-a", "agent-1", "cur_000000000004_evt_other", "evt_speech", "observe_delta", now.Add(3*time.Second)); err == nil {
		t.Fatalf("cursor/event mismatch must fail closed")
	}
}
