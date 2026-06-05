package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/registry"
	"kkachi-agent-network-control/internal/storage"
	"kkachi-agent-network-control/internal/transport"
)

type Server struct {
	DataHome string
	Runtime  registry.Runtime

	// StreamFollowAfterReplay is a local test seam used to append durable
	// channel.jsonl events after replay has been snapshotted and before the
	// bounded follow poll starts. Production servers leave it nil.
	StreamFollowAfterReplay func() error

	mu       sync.RWMutex
	ready    bool
	started  time.Time
	listener net.Listener
}

func NewServer(dataHome string, runtime registry.Runtime) *Server {
	return &Server{DataHome: filepath.Clean(dataHome), Runtime: runtime}
}

func (s *Server) Run(ctx context.Context) error {
	if err := s.preflight(); err != nil {
		RecordPreSessionViolation(s.DataHome, s.Runtime, "security_violation", "daemon_start_rejected", err)
		return err
	}
	listener, _, err := transport.PrepareListen(s.DataHome, s.Runtime)
	if err != nil {
		if protocol.ClassifyExit(err) == protocol.ExitOK {
			return err
		}
		RecordPreSessionViolation(s.DataHome, s.Runtime, "security_violation", "daemon_start_rejected", err)
		return err
	}
	s.mu.Lock()
	s.listener = listener
	s.ready = true
	s.started = time.Now().UTC()
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-done:
		}
	}()
	defer func() {
		close(done)
		_ = listener.Close()
		_ = removeOwnedSocket(s.DataHome)
		s.mu.Lock()
		s.ready = false
		s.mu.Unlock()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *Server) preflight() error {
	if err := registry.ValidateDataHome(s.DataHome, s.Runtime); err != nil {
		return err
	}
	if _, err := registry.Load(s.DataHome, s.Runtime); err != nil {
		return err
	}
	return nil
}

func (s *Server) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ready
}

func (s *Server) Shutdown() {
	s.mu.RLock()
	listener := s.listener
	s.mu.RUnlock()
	if listener != nil {
		_ = listener.Close()
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	var request protocol.CommandRequest
	if err := json.NewDecoder(conn).Decode(&request); err != nil {
		request = protocol.NewRequest("", "decode", nil)
		_ = json.NewEncoder(conn).Encode(protocol.ErrorResponse(request, protocol.InternalError("decode request: "+err.Error())))
		return
	}
	response := s.Handle(request)
	_ = json.NewEncoder(conn).Encode(response)
	if request.Command == "shutdown" && response.OK {
		go s.Shutdown()
	}
}

func (s *Server) Handle(request protocol.CommandRequest) protocol.CommandResponse {
	if request.SchemaVersion != 0 && request.SchemaVersion != protocol.SchemaVersion {
		return protocol.ErrorResponse(request, protocol.NewError(protocol.ErrorValidation, "unsupported command schema_version", protocol.ExitUsage, map[string]any{"schema_version": request.SchemaVersion}))
	}
	switch request.Command {
	case "ping":
		return protocol.SuccessResponse(request, map[string]any{"ready": s.Ready()})
	case "status":
		return protocol.SuccessResponse(request, s.statusResult())
	case "health":
		return protocol.SuccessResponse(request, s.healthResult())
	case "shutdown":
		return protocol.SuccessResponse(request, map[string]any{"stopping": true})
	default:
		if response, ok := s.handleDAEMN002(request); ok {
			return response
		}
		return protocol.ErrorResponse(request, protocol.UnsupportedFeature(request.Command))
	}
}

func (s *Server) statusResult() map[string]any {
	s.mu.RLock()
	ready := s.ready
	started := s.started
	s.mu.RUnlock()
	result := map[string]any{
		"daemon":    "running",
		"ready":     ready,
		"socket":    transport.SocketPath(s.DataHome),
		"data_home": s.DataHome,
	}
	if !started.IsZero() {
		result["started_at"] = started.Format(time.RFC3339Nano)
	}
	return result
}

func (s *Server) healthResult() map[string]any {
	categories := map[string]any{}
	ready := true

	if err := registry.ValidateDataHome(s.DataHome, s.Runtime); err != nil {
		ready = false
		categories["data_home"] = redactedCategory("invalid", err.Error())
	} else {
		categories["data_home"] = redactedCategory("valid", "")
	}

	if loaded, err := registry.Load(s.DataHome, s.Runtime); err != nil {
		ready = false
		categories["registry"] = redactedCategory("invalid", err.Error())
	} else {
		categories["registry"] = map[string]any{"status": "valid", "schema_version": loaded.Registry.EffectiveSchemaVersion(), "enabled_members": enabledCount(loaded)}
	}

	if report, err := storage.VerifyStorage(s.DataHome, storage.VerifyOptions{Runtime: s.Runtime}); err != nil {
		status := "invalid"
		if report != nil && report.Status != "" {
			status = report.Status
		}
		categories["storage"] = redactedCategory(status, err.Error())
	} else {
		categories["storage"] = map[string]any{"status": report.Status, "source_events": report.Expected.EventCount}
	}

	socketStatus := transport.ClassifySocket(transport.SocketPath(s.DataHome), 50*time.Millisecond)
	categories["socket"] = map[string]any{"status": string(socketStatus.State)}
	categories["liveness"] = map[string]any{"status": "alive"}
	categories["readiness"] = map[string]any{"status": readinessStatus(ready)}

	return map[string]any{
		"ready":      ready,
		"categories": categories,
	}
}

func redactedCategory(status, detail string) map[string]any {
	out := map[string]any{"status": status}
	if detail != "" {
		out["detail"] = "[REDACTED]"
	}
	return out
}

func readinessStatus(ready bool) string {
	if ready {
		return "ready"
	}
	return "not_ready"
}

func enabledCount(loaded *registry.LoadedRegistry) int {
	count := 0
	for _, member := range loaded.Registry.Members {
		if member.Enabled {
			count++
		}
	}
	return count
}

func removeOwnedSocket(dataHome string) error {
	path := transport.SocketPath(dataHome)
	status := transport.ClassifySocket(path, 20*time.Millisecond)
	if status.State == transport.SocketStale {
		return os.Remove(path)
	}
	if status.State == transport.SocketMissing {
		return nil
	}
	return nil
}
