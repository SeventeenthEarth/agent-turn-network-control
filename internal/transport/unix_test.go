package transport_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/registry"
	"kkachi-agent-network-control/internal/transport"
)

func TestIntegrationSocketPathAndRuntimeDirAreOwnerOnly(t *testing.T) {
	dataHome := safeDataHome(t)
	dir, err := transport.EnsureRuntimeDir(dataHome, registry.DefaultRuntime())
	if err != nil {
		t.Fatalf("ensure runtime dir: %v", err)
	}
	if dir != filepath.Join(dataHome, "run") {
		t.Fatalf("unexpected runtime dir %q", dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat runtime dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("expected runtime dir 0700, got %04o", info.Mode().Perm())
	}
	if got := transport.SocketPath(dataHome); got != filepath.Join(dataHome, "run", "kkachi-agent-networkd.sock") {
		t.Fatalf("unexpected socket path %q", got)
	}
}

func TestIntegrationPrepareListenCleansOnlyProvenStaleSocket(t *testing.T) {
	dataHome := safeDataHome(t)
	dir, err := transport.EnsureRuntimeDir(dataHome, registry.DefaultRuntime())
	if err != nil {
		t.Fatalf("ensure runtime dir: %v", err)
	}
	socketPath := filepath.Join(dir, transport.SocketName)
	makeStaleSocket(t, socketPath)
	if status := transport.ClassifySocket(socketPath, 50*time.Millisecond); status.State != transport.SocketStale {
		t.Fatalf("expected stale socket, got %+v", status)
	}
	prepared, status, err := transport.PrepareListen(dataHome, registry.DefaultRuntime())
	if err != nil {
		t.Fatalf("prepare listen should remove stale socket, status=%+v err=%v", status, err)
	}
	defer func() { _ = prepared.Close() }()
	if status.State != transport.SocketStale {
		t.Fatalf("expected stale cleanup status, got %+v", status)
	}
}

func TestIntegrationPrepareListenFailsClosedOnAmbiguousSocketPath(t *testing.T) {
	dataHome := safeDataHome(t)
	dir, err := transport.EnsureRuntimeDir(dataHome, registry.DefaultRuntime())
	if err != nil {
		t.Fatalf("ensure runtime dir: %v", err)
	}
	socketPath := filepath.Join(dir, transport.SocketName)
	if err := os.WriteFile(socketPath, []byte("not a socket"), 0o600); err != nil {
		t.Fatalf("write ambiguous socket fixture: %v", err)
	}
	listener, status, err := transport.PrepareListen(dataHome, registry.DefaultRuntime())
	if err == nil {
		_ = listener.Close()
		t.Fatalf("expected ambiguous socket to fail closed")
	}
	if status.State != transport.SocketAmbiguous {
		t.Fatalf("expected ambiguous status, got %+v", status)
	}
	if protocol.ClassifyExit(err) != protocol.ExitUnsafe {
		t.Fatalf("expected unsafe exit classification, got %d", protocol.ClassifyExit(err))
	}
	if _, statErr := os.Stat(socketPath); statErr != nil {
		t.Fatalf("ambiguous socket path was removed: %v", statErr)
	}
}

func TestIntegrationDialTargetFailsClosedBeforeUnsafeSocketDial(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, dataHome string)
	}{
		{
			name: "unsafe data home",
			setup: func(t *testing.T, dataHome string) {
				t.Helper()
				if err := os.Chmod(dataHome, 0o777); err != nil {
					t.Fatalf("chmod unsafe data home: %v", err)
				}
			},
		},
		{
			name: "unsafe runtime directory",
			setup: func(t *testing.T, dataHome string) {
				t.Helper()
				dir := filepath.Join(dataHome, transport.RuntimeDirName)
				if err := os.Mkdir(dir, 0o777); err != nil {
					t.Fatalf("mkdir unsafe runtime directory: %v", err)
				}
				if err := os.Chmod(dir, 0o777); err != nil {
					t.Fatalf("chmod unsafe runtime directory: %v", err)
				}
			},
		},
		{
			name: "symlink socket path",
			setup: func(t *testing.T, dataHome string) {
				t.Helper()
				dir, err := transport.EnsureRuntimeDir(dataHome, registry.DefaultRuntime())
				if err != nil {
					t.Fatalf("ensure runtime dir: %v", err)
				}
				if err := os.Symlink("/tmp/not-a-kan-socket", filepath.Join(dir, transport.SocketName)); err != nil {
					t.Fatalf("symlink socket path: %v", err)
				}
			},
		},
		{
			name: "non socket path",
			setup: func(t *testing.T, dataHome string) {
				t.Helper()
				dir, err := transport.EnsureRuntimeDir(dataHome, registry.DefaultRuntime())
				if err != nil {
					t.Fatalf("ensure runtime dir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, transport.SocketName), []byte("spoof"), 0o600); err != nil {
					t.Fatalf("write spoof socket path: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := safeDataHome(t)
			tc.setup(t, dataHome)

			_, err := transport.Dial(dataHome, 20*time.Millisecond)

			if err == nil {
				t.Fatalf("expected dial target validation to fail closed")
			}
			if protocol.ClassifyExit(err) != protocol.ExitUnsafe {
				t.Fatalf("expected unsafe exit classification, got %d err=%v", protocol.ClassifyExit(err), err)
			}
		})
	}
}

func makeStaleSocket(t *testing.T, path string) {
	t.Helper()
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("create stale socket fd: %v", err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		_ = syscall.Close(fd)
		t.Fatalf("bind stale socket fixture: %v", err)
	}
	if err := syscall.Close(fd); err != nil {
		t.Fatalf("close stale socket fd: %v", err)
	}
}

func safeDataHome(t *testing.T) string {
	t.Helper()
	dataHome, err := os.MkdirTemp("/private/tmp", "kan-transport-")
	if err != nil {
		t.Fatalf("make short temp data home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dataHome) })
	if err := os.Chmod(dataHome, 0o700); err != nil {
		t.Fatalf("chmod data home: %v", err)
	}
	return dataHome
}
