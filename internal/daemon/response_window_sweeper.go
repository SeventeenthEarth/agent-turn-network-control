package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"atn-control/internal/storage"
)

const defaultResponseWindowSweepInterval = time.Second

func (s *Server) runResponseWindowSweeper(ctx context.Context, done <-chan struct{}) {
	_ = s.sweepCouncilResponseWindowTimeouts()
	interval := s.ResponseWindowSweepInterval
	if interval <= 0 {
		interval = defaultResponseWindowSweepInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			_ = s.sweepCouncilResponseWindowTimeouts()
		}
	}
}

func (s *Server) sweepCouncilResponseWindowTimeouts() error {
	root := filepath.Join(filepath.Clean(s.DataHome), storage.SessionsDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionID := entry.Name()
		if err := storage.ValidateSessionID(sessionID); err != nil {
			continue
		}
		sessionDir, err := storage.SessionDir(s.DataHome, sessionID)
		if err != nil {
			continue
		}
		metadata, err := storage.LoadSessionYAML(sessionDir)
		if err != nil || metadata.SessionType != storage.SessionTypeCouncil {
			continue
		}
		status, err := storage.CouncilStatusFromLogAt(sessionDir, metadata, s.now())
		if err != nil || status["status"] == string(storage.StatusTerminal) {
			continue
		}
		turn, _ := status["current_turn"].(int)
		if turn <= 0 {
			continue
		}
		accounting, _ := status["response_window_accounting"].(map[string]any)
		if accounting["state"] != "closed" || accounting["closed_reason"] != "timeout" {
			continue
		}
		if _, _, err := storage.RecordCouncilResponseWindowTimeout(sessionDir, metadata, turn, s.now()); err != nil {
			return err
		}
	}
	return nil
}
