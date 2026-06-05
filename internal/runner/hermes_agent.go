package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type HermesAgentAdapter struct{}

func NewHermesAgentAdapter() *HermesAgentAdapter { return &HermesAgentAdapter{} }

func (a *HermesAgentAdapter) Kind() string       { return HermesAgentKind }
func (a *HermesAgentAdapter) CostSource() string { return HermesAgentCostSource }

func (a *HermesAgentAdapter) Send(ctx context.Context, req Request) (Result, error) {
	return a.invoke(ctx, "send", req)
}

func (a *HermesAgentAdapter) Resume(ctx context.Context, req Request) (Result, error) {
	return a.invoke(ctx, "resume", req)
}

func (a *HermesAgentAdapter) Cancel(ctx context.Context, handle SessionHandle) error {
	return nil
}

func (a *HermesAgentAdapter) ParseSessionHandle(stdout []byte) (*SessionHandle, error) {
	for _, line := range strings.Split(string(stdout), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "session_handle=") {
			handle := SessionHandle(strings.TrimPrefix(line, "session_handle="))
			return &handle, nil
		}
	}
	return nil, nil
}

func (a *HermesAgentAdapter) invoke(ctx context.Context, mode string, req Request) (Result, error) {
	wrapper := strings.TrimSpace(req.ResolvedWrapper)
	if wrapper == "" && req.Member.ResolvedWrapper != nil {
		wrapper = req.Member.ResolvedWrapper.ResolvedPath
	}
	if wrapper == "" || !filepath.IsAbs(wrapper) {
		return Result{ErrorClass: "wrapper_unresolved"}, fmt.Errorf("resolved wrapper path is required")
	}
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	args := append([]string(nil), req.Args...)
	if len(args) == 0 {
		args = []string{mode}
		if req.SessionHandle != nil {
			args = append(args, "--session", string(*req.SessionHandle))
		}
		if req.Prompt != "" {
			args = append(args, req.Prompt)
		}
	}
	cmd := exec.CommandContext(ctx, wrapper, args...)
	cmd.Dir = req.Member.Workspace
	cmd.Env = append([]string(nil), req.Env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	started := time.Now()
	err := cmd.Run()
	duration := time.Since(started)
	result := Result{
		OK:             err == nil,
		Stdout:         stdout.Bytes(),
		Stderr:         stderr.Bytes(),
		ExitCode:       exitCode(err),
		Duration:       duration,
		Cost:           ParseHermesCost(stderr.Bytes()),
		SemanticStatus: "succeeded",
	}
	if handle, parseErr := a.ParseSessionHandle(stdout.Bytes()); parseErr == nil {
		result.SessionHandle = handle
	}
	if ctx.Err() != nil {
		result.OK = false
		result.ErrorClass = "timeout"
		return result, ctx.Err()
	}
	if err != nil {
		result.ErrorClass = "nonzero_exit"
		return result, err
	}
	return result, nil
}
