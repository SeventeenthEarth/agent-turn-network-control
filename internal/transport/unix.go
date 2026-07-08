package transport

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/SeventeenthEarth/agent-turn-network-control/internal/protocol"
	"github.com/SeventeenthEarth/agent-turn-network-control/internal/registry"
)

const (
	RuntimeDirName = "run"
	SocketName     = "atn-controld.sock"
)

type SocketState string

const (
	SocketMissing   SocketState = "missing"
	SocketLive      SocketState = "live"
	SocketStale     SocketState = "stale"
	SocketAmbiguous SocketState = "ambiguous"
)

type SocketStatus struct {
	Path   string
	State  SocketState
	Detail string
}

func RuntimeDir(dataHome string) string {
	return filepath.Join(filepath.Clean(dataHome), RuntimeDirName)
}

func SocketPath(dataHome string) string {
	return filepath.Join(RuntimeDir(dataHome), SocketName)
}

func EnsureRuntimeDir(dataHome string, runtime registry.Runtime) (string, error) {
	clean := filepath.Clean(dataHome)
	if err := registry.ValidateDataHome(clean, runtime); err != nil {
		return "", err
	}
	dir := RuntimeDir(clean)
	if info, err := os.Lstat(dir); err == nil {
		if err := validateRuntimeDirInfo(dir, info, runtime); err != nil {
			return "", err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return "", err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return "", err
		}
	} else {
		return "", err
	}
	return dir, nil
}

func ValidateDialTarget(dataHome string, runtime registry.Runtime, timeout time.Duration) (string, error) {
	clean := filepath.Clean(dataHome)
	if err := registry.ValidateDataHome(clean, runtime); err != nil {
		return "", protocol.UnsafeRuntime("data home is unsafe", map[string]any{"path": clean, "detail": "[REDACTED]"})
	}
	dir := RuntimeDir(clean)
	info, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", protocol.DaemonUnavailable("daemon runtime directory is missing")
		}
		return "", protocol.UnsafeRuntime("runtime directory is ambiguous", map[string]any{"path": dir, "detail": "[REDACTED]"})
	}
	if err := validateRuntimeDirInfo(dir, info, runtime); err != nil {
		return "", err
	}
	path := filepath.Join(dir, SocketName)
	status := ClassifySocket(path, timeout)
	switch status.State {
	case SocketLive:
		return path, nil
	case SocketMissing:
		return "", protocol.DaemonUnavailable("daemon socket is missing")
	case SocketStale:
		return "", protocol.DaemonUnavailable("daemon socket is stale")
	default:
		return "", protocol.UnsafeRuntime("socket path is ambiguous", map[string]any{"socket": path, "detail": status.Detail})
	}
}

func validateRuntimeDirInfo(path string, info os.FileInfo, runtime registry.Runtime) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return protocol.UnsafeRuntime("runtime directory must not be a symlink", map[string]any{"path": path})
	}
	if !info.IsDir() {
		return protocol.UnsafeRuntime("runtime path is not a directory", map[string]any{"path": path})
	}
	uid, ok := ownerUID(info)
	if !ok {
		return protocol.UnsafeRuntime("runtime directory owner is ambiguous", map[string]any{"path": path})
	}
	if uid != currentUID(runtime) {
		return protocol.UnsafeRuntime("runtime directory owner is unsafe", map[string]any{"path": path, "owner_uid": uid})
	}
	if info.Mode().Perm()&0o077 != 0 {
		return protocol.UnsafeRuntime("runtime directory must be owner-only", map[string]any{"path": path, "mode": fmt.Sprintf("%04o", info.Mode().Perm())})
	}
	return nil
}

func ClassifySocket(path string, timeout time.Duration) SocketStatus {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SocketStatus{Path: path, State: SocketMissing}
		}
		return SocketStatus{Path: path, State: SocketAmbiguous, Detail: err.Error()}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return SocketStatus{Path: path, State: SocketAmbiguous, Detail: "socket path is a symlink"}
	}
	if info.Mode()&os.ModeSocket == 0 {
		return SocketStatus{Path: path, State: SocketAmbiguous, Detail: "socket path is not a socket"}
	}
	conn, err := net.DialTimeout("unix", path, timeout)
	if err == nil {
		_ = conn.Close()
		return SocketStatus{Path: path, State: SocketLive}
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return SocketStatus{Path: path, State: SocketStale, Detail: err.Error()}
	}
	return SocketStatus{Path: path, State: SocketAmbiguous, Detail: err.Error()}
}

func PrepareListen(dataHome string, runtime registry.Runtime) (net.Listener, SocketStatus, error) {
	dir, err := EnsureRuntimeDir(dataHome, runtime)
	if err != nil {
		return nil, SocketStatus{Path: SocketPath(dataHome), State: SocketAmbiguous, Detail: err.Error()}, err
	}
	path := filepath.Join(dir, SocketName)
	status := ClassifySocket(path, 200*time.Millisecond)
	switch status.State {
	case SocketMissing:
	case SocketStale:
		if err := os.Remove(path); err != nil {
			return nil, status, err
		}
	case SocketLive:
		return nil, status, protocol.NewError("daemon_already_running", "daemon is already running", protocol.ExitOK, map[string]any{"socket": path})
	default:
		return nil, status, protocol.UnsafeRuntime("socket path is ambiguous", map[string]any{"socket": path, "detail": status.Detail})
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, status, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, status, err
	}
	return listener, status, nil
}

func Dial(dataHome string, timeout time.Duration) (net.Conn, error) {
	return DialWithRuntime(dataHome, registry.DefaultRuntime(), timeout)
}

func DialWithRuntime(dataHome string, runtime registry.Runtime, timeout time.Duration) (net.Conn, error) {
	path, err := ValidateDialTarget(dataHome, runtime, timeout)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", path, timeout)
	if err != nil {
		return nil, protocol.DaemonUnavailable(err.Error())
	}
	return conn, nil
}

func RoundTrip(dataHome string, request protocol.CommandRequest, timeout time.Duration) (protocol.CommandResponse, error) {
	return RoundTripWithRuntime(dataHome, registry.DefaultRuntime(), request, timeout)
}

func RoundTripWithRuntime(dataHome string, runtime registry.Runtime, request protocol.CommandRequest, timeout time.Duration) (protocol.CommandResponse, error) {
	conn, err := DialWithRuntime(dataHome, runtime, timeout)
	if err != nil {
		return protocol.CommandResponse{}, err
	}
	defer func() { _ = conn.Close() }()
	if deadline := time.Now().Add(timeout); timeout > 0 {
		_ = conn.SetDeadline(deadline)
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return protocol.CommandResponse{}, err
	}
	var response protocol.CommandResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return protocol.CommandResponse{}, err
	}
	if !response.OK && response.Error != nil {
		return response, response.Error
	}
	return response, nil
}

func ownerUID(info os.FileInfo) (int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Uid), true
}

func currentUID(runtime registry.Runtime) int {
	if runtime.CurrentUID != nil {
		return runtime.CurrentUID()
	}
	return os.Getuid()
}
