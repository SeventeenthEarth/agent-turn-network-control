package command

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"kkachi-agent-network-control/internal/protocol"
	"kkachi-agent-network-control/internal/registry"
	"kkachi-agent-network-control/internal/transport"
)

func (a App) runVersion(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintf(stdout, "%s bootstrap protocol_version=%s schema_version=%d\n", a.Name, protocol.ProtocolVersion, protocol.SchemaVersion)
		return protocol.ExitOK
	}
	features := false
	format := "text"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--features":
			features = true
		case "--format":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--format requires a value", protocol.ExitUsage, nil))
			}
			format = args[i+1]
			i++
		default:
			return writeProtocolError(stderr, protocol.UnsupportedFeature("version "+args[i]))
		}
	}
	if !features {
		return writeProtocolError(stderr, protocol.UnsupportedFeature("version options"))
	}
	if format != "json" {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "only --format json is supported for --features", protocol.ExitUsage, nil))
	}
	writeJSON(stdout, protocol.NewVersionFeatures())
	return protocol.ExitOK
}

func (a App) runStream(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s stream <session_id> --member <member> (--from-start|--since <cursor>) [--follow] [--follow-timeout-ms <ms>] [--follow-poll-ms <ms>] --format ndjson\n  %s stream ack <session_id> --member <member> --cursor <cursor> [--command-id <id>]\n  %s stream status <session_id>\n", a.Name, a.Name, a.Name)
		return protocol.ExitOK
	}
	if args[0] == "ack" {
		return a.runStreamAck(args[1:], stdout, stderr)
	}
	if args[0] == "status" {
		if len(args) != 2 {
			return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "stream status requires session_id", protocol.ExitUsage, nil))
		}
		return a.daemonRequestWithParams(stdout, stderr, protocol.FeatureStreamStatus, map[string]any{"session_id": args[1]})
	}
	sessionID := args[0]
	params := map[string]any{"session_id": sessionID}
	format := ""
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--member":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--member requires a value", protocol.ExitUsage, nil))
			}
			params["member"] = args[i+1]
			i++
		case "--from-start":
			params["from_start"] = true
		case "--since":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--since requires a cursor", protocol.ExitUsage, nil))
			}
			params["since"] = args[i+1]
			i++
		case "--follow":
			params["follow"] = true
		case "--follow-timeout-ms":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--follow-timeout-ms requires a value", protocol.ExitUsage, nil))
			}
			value, ok := positiveIntArg(args[i+1])
			if !ok {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--follow-timeout-ms must be a positive integer", protocol.ExitUsage, nil))
			}
			params["follow_timeout_ms"] = value
			i++
		case "--follow-poll-ms":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--follow-poll-ms requires a value", protocol.ExitUsage, nil))
			}
			value, ok := positiveIntArg(args[i+1])
			if !ok {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--follow-poll-ms must be a positive integer", protocol.ExitUsage, nil))
			}
			params["follow_poll_ms"] = value
			i++
		case "--format":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--format requires a value", protocol.ExitUsage, nil))
			}
			format = args[i+1]
			i++
		default:
			return writeProtocolError(stderr, protocol.UnsupportedFeature("stream "+args[i]))
		}
	}
	if params["member"] == nil {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--member is required", protocol.ExitUsage, nil))
	}
	if format != "ndjson" {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "stream requires --format ndjson", protocol.ExitUsage, nil))
	}
	response, exit := a.daemonCommand(protocol.FeatureStreamReplay, params, stderr)
	if exit != protocol.ExitOK {
		return exit
	}
	frames, _ := response.Result["frames"].([]any)
	for _, frame := range frames {
		data, err := json.Marshal(frame)
		if err != nil {
			return writeProtocolError(stderr, protocol.InternalError(err.Error()))
		}
		_, _ = stdout.Write(append(data, '\n'))
	}
	return protocol.ExitOK
}

func positiveIntArg(value string) (int, bool) {
	out, err := strconv.Atoi(value)
	if err != nil || out <= 0 {
		return 0, false
	}
	return out, true
}

func (a App) runStreamAck(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "stream ack requires session_id", protocol.ExitUsage, nil))
	}
	params := map[string]any{"session_id": args[0]}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--member":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--member requires a value", protocol.ExitUsage, nil))
			}
			params["member"] = args[i+1]
			i++
		case "--cursor":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--cursor requires a value", protocol.ExitUsage, nil))
			}
			params["cursor"] = args[i+1]
			i++
		case "--command-id":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--command-id requires a value", protocol.ExitUsage, nil))
			}
			params["command_id"] = args[i+1]
			i++
		default:
			return writeProtocolError(stderr, protocol.UnsupportedFeature("stream ack "+args[i]))
		}
	}
	if params["member"] == nil || params["cursor"] == nil {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "stream ack requires --member and --cursor", protocol.ExitUsage, nil))
	}
	return a.daemonRequestWithParams(stdout, stderr, protocol.FeatureStreamAck, params)
}

func (a App) runDelegate(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s delegate escalation-delivered <session_id> --escalation <event_id> --delivery-target <target> --platform <platform> [--message-ref <ref>] [--command-id <id>]\n  %s delegate escalation-delivery-failed <session_id> --escalation <event_id> --target <target> --reason <reason> [--will-retry-target <target>]... [--command-id <id>]\n", a.Name, a.Name)
		return protocol.ExitOK
	}
	sub := args[0]
	if sub != "escalation-delivered" && sub != "escalation-delivery-failed" {
		return writeProtocolError(stderr, protocol.UnsupportedFeature("delegate "+sub))
	}
	if len(args) < 2 {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "delegate command requires session_id", protocol.ExitUsage, nil))
	}
	params := map[string]any{"session_id": args[1]}
	var retryTargets []string
	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "--escalation":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--escalation requires a value", protocol.ExitUsage, nil))
			}
			params["escalation"] = args[i+1]
			i++
		case "--delivery-target":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--delivery-target requires a value", protocol.ExitUsage, nil))
			}
			params["delivery_target"] = args[i+1]
			i++
		case "--platform":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--platform requires a value", protocol.ExitUsage, nil))
			}
			params["platform"] = args[i+1]
			i++
		case "--message-ref":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--message-ref requires a value", protocol.ExitUsage, nil))
			}
			params["message_ref"] = args[i+1]
			i++
		case "--target":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--target requires a value", protocol.ExitUsage, nil))
			}
			params["target"] = args[i+1]
			i++
		case "--reason":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--reason requires a value", protocol.ExitUsage, nil))
			}
			params["reason"] = args[i+1]
			i++
		case "--will-retry-target":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--will-retry-target requires a value", protocol.ExitUsage, nil))
			}
			retryTargets = append(retryTargets, args[i+1])
			i++
		case "--reporter":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--reporter requires a value", protocol.ExitUsage, nil))
			}
			params["reporter"] = args[i+1]
			i++
		case "--command-id":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--command-id requires a value", protocol.ExitUsage, nil))
			}
			params["command_id"] = args[i+1]
			i++
		default:
			return writeProtocolError(stderr, protocol.UnsupportedFeature("delegate "+args[i]))
		}
	}
	if len(retryTargets) > 0 {
		params["will_retry_targets"] = retryTargets
	}
	commandName := "delegate.escalation_delivered"
	if sub == "escalation-delivery-failed" {
		commandName = "delegate.escalation_delivery_failed"
	}
	return a.daemonRequestWithParams(stdout, stderr, commandName, params)
}

func (a App) runConformance(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s conformance fixtures --format json\n", a.Name)
		return protocol.ExitOK
	}
	if len(args) != 3 || args[0] != "fixtures" || args[1] != "--format" || args[2] != "json" {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "expected conformance fixtures --format json", protocol.ExitUsage, nil))
	}
	root, err := repoRootForConformance()
	if err != nil {
		return writeProtocolError(stderr, protocol.InternalError(err.Error()))
	}
	path := filepath.Join(root, "testdata", "conformance", "manifest.json")
	content, err := os.ReadFile(path)
	if err != nil {
		return writeProtocolError(stderr, protocol.InternalError(err.Error()))
	}
	var manifest map[string]any
	if err := json.Unmarshal(content, &manifest); err != nil {
		return writeProtocolError(stderr, protocol.InternalError(err.Error()))
	}
	writeJSON(stdout, manifest)
	return protocol.ExitOK
}

func (a App) daemonRequestWithParams(stdout io.Writer, stderr io.Writer, command string, params map[string]any) int {
	response, exit := a.daemonCommand(command, params, stderr)
	if exit != protocol.ExitOK {
		return exit
	}
	writeJSON(stdout, response.Result)
	return protocol.ExitOK
}

func (a App) daemonCommand(command string, params map[string]any, stderr io.Writer) (protocol.CommandResponse, int) {
	dataHome, err := registry.ResolveDataHome(a.Runtime)
	if err != nil {
		return protocol.CommandResponse{}, writeProtocolError(stderr, protocol.InternalError(err.Error()))
	}
	request := protocol.NewRequest("cli-"+strings.ReplaceAll(command, ".", "-")+"-"+fmt.Sprint(time.Now().UnixNano()), command, params)
	response, err := transport.RoundTripWithRuntime(dataHome, a.Runtime, request, time.Second)
	if err != nil {
		if response.Error != nil {
			return response, writeProtocolError(stderr, response.Error)
		}
		return response, writeProtocolError(stderr, protocol.ToStructuredError(err))
	}
	return response, protocol.ExitOK
}

func repoRootForConformance() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	var starts []string
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if ok {
		starts = append(starts, filepath.Dir(file))
	}
	for _, start := range starts {
		dir := filepath.Clean(start)
		for i := 0; i < 8; i++ {
			if _, err := os.Stat(filepath.Join(dir, "testdata", "conformance", "manifest.json")); err == nil {
				return dir, nil
			}
			next := filepath.Dir(dir)
			if next == dir {
				break
			}
			dir = next
		}
	}
	return "", fmt.Errorf("conformance manifest not found")
}
