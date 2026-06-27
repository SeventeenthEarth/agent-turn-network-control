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

	"atn-control/internal/protocol"
	"atn-control/internal/registry"
	"atn-control/internal/storage"
	"atn-control/internal/transport"
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

func (a App) runTranscript(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s transcript <session_id> --format md|jsonl [--output <path>]\n", a.Name)
		return protocol.ExitOK
	}
	params := map[string]any{"session_id": args[0]}
	output := ""
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--format requires a value", protocol.ExitUsage, nil))
			}
			params["format"] = args[i+1]
			i++
		case "--output":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--output requires a value", protocol.ExitUsage, nil))
			}
			output = args[i+1]
			i++
		default:
			return writeProtocolError(stderr, protocol.UnsupportedFeature("transcript "+args[i]))
		}
	}
	if params["format"] == nil {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "transcript requires --format md or --format jsonl", protocol.ExitUsage, nil))
	}
	if params["format"] != storage.TranscriptMarkdownFormat && params["format"] != storage.TranscriptJSONLFormat {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "transcript format must be md or jsonl", protocol.ExitUsage, nil))
	}
	response, exit := a.daemonCommand("transcript.render", params, stderr)
	if exit != protocol.ExitOK {
		return exit
	}
	content, ok := response.Result["content"].(string)
	if !ok {
		return writeProtocolError(stderr, protocol.InternalError("transcript.render response missing content"))
	}
	if output == "" {
		_, _ = io.WriteString(stdout, content)
		return protocol.ExitOK
	}
	path, err := safeOperatorOutputPath(output)
	if err != nil {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, err.Error(), protocol.ExitUsage, nil))
	}
	if err := writeOperatorFile(path, []byte(content)); err != nil {
		return writeProtocolError(stderr, protocol.InternalError(err.Error()))
	}
	writeJSON(stdout, map[string]any{"session_id": args[0], "format": params["format"], "output_path": path})
	return protocol.ExitOK
}

func (a App) runExport(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s export <session_id> --bundle [--output <directory>]\n", a.Name)
		return protocol.ExitOK
	}
	params := map[string]any{"session_id": args[0]}
	bundle := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--bundle":
			bundle = true
		case "--output":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--output requires a value", protocol.ExitUsage, nil))
			}
			path, err := safeOperatorOutputPath(args[i+1])
			if err != nil {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, err.Error(), protocol.ExitUsage, nil))
			}
			params["output_path"] = path
			i++
		default:
			return writeProtocolError(stderr, protocol.UnsupportedFeature("export "+args[i]))
		}
	}
	if !bundle {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "export requires --bundle", protocol.ExitUsage, nil))
	}
	return a.daemonRequestWithParams(stdout, stderr, "export.bundle", params)
}

func (a App) runTail(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s tail <session_id> [--limit <n>] --format ndjson\n", a.Name)
		return protocol.ExitOK
	}
	params := map[string]any{"session_id": args[0]}
	format := ""
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--limit":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--limit requires a value", protocol.ExitUsage, nil))
			}
			value, ok := positiveIntArg(args[i+1])
			if !ok {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--limit must be a positive integer", protocol.ExitUsage, nil))
			}
			params["limit"] = value
			i++
		case "--format":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--format requires a value", protocol.ExitUsage, nil))
			}
			format = args[i+1]
			i++
		default:
			return writeProtocolError(stderr, protocol.UnsupportedFeature("tail "+args[i]))
		}
	}
	if format != "ndjson" {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "tail requires --format ndjson", protocol.ExitUsage, nil))
	}
	response, exit := a.daemonCommand("tail.session", params, stderr)
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

func (a App) runDelegate(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s delegate new <session_id> --moderator <member> --assignee <member> --title <title> --task <task>\n  %s delegate <action> <session_id> [--actor <member>] [--payload key=value]... [--command-id <id>]\n  %s delegate escalation-delivered <session_id> --escalation <event_id> --delivery-target <target> --platform <platform> [--message-ref <ref>] [--command-id <id>]\n  %s delegate escalation-delivery-failed <session_id> --escalation <event_id> --target <target> --reason <reason> [--will-retry-target <target>]... [--command-id <id>]\n", a.Name, a.Name, a.Name, a.Name)
		return protocol.ExitOK
	}
	sub := args[0]
	if sub == "new" {
		return a.runDelegateNew(args[1:], stdout, stderr)
	}
	if sub == "escalation-batches" {
		if len(args) != 2 {
			return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "delegate escalation-batches requires session_id", protocol.ExitUsage, nil))
		}
		return a.daemonRequestWithParams(stdout, stderr, "delegate.escalation_batches", map[string]any{"session_id": args[1]})
	}
	if sub != "escalation-delivered" && sub != "escalation-delivery-failed" {
		return a.runDelegationEvent(sub, args[1:], stdout, stderr)
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

func (a App) runDelegateNew(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "delegate new requires session_id", protocol.ExitUsage, nil))
	}
	params := map[string]any{"session_id": args[0]}
	var participants []string
	var acceptance []string
	var expected []string
	limits := map[string]any{}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--moderator", "--assignee", "--title", "--task", "--context", "--event-id", "--assignment-event-id", "--command-id":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
			}
			params[strings.TrimPrefix(strings.ReplaceAll(args[i], "-", "_"), "__")] = args[i+1]
			i++
		case "--participant":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--participant requires a value", protocol.ExitUsage, nil))
			}
			participants = append(participants, args[i+1])
			i++
		case "--acceptance":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--acceptance requires a value", protocol.ExitUsage, nil))
			}
			acceptance = append(acceptance, args[i+1])
			i++
		case "--expected-output":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--expected-output requires a value", protocol.ExitUsage, nil))
			}
			expected = append(expected, args[i+1])
			i++
		case "--limit":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--limit requires key=value", protocol.ExitUsage, nil))
			}
			key, value, ok := splitKeyValue(args[i+1])
			if !ok {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--limit requires key=value", protocol.ExitUsage, nil))
			}
			limits[key] = parseScalar(value)
			i++
		default:
			return writeProtocolError(stderr, protocol.UnsupportedFeature("delegate new "+args[i]))
		}
	}
	if len(participants) > 0 {
		params["participants"] = participants
	}
	if len(acceptance) > 0 {
		params["acceptance"] = acceptance
	}
	if len(expected) > 0 {
		params["expected_outputs"] = expected
	}
	if len(limits) > 0 {
		params["limits"] = limits
	}
	return a.daemonRequestWithParams(stdout, stderr, "delegate.new", params)
}

func (a App) runDelegationEvent(sub string, args []string, stdout io.Writer, stderr io.Writer) int {
	commandName, ok := delegationCommandName(sub)
	if !ok {
		return writeProtocolError(stderr, protocol.UnsupportedFeature("delegate "+sub))
	}
	if len(args) == 0 {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "delegate "+sub+" requires session_id", protocol.ExitUsage, nil))
	}
	params := map[string]any{"session_id": args[0]}
	payload := map[string]any{}
	var recipients []string
	var artifacts []string
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--actor", "--command-id", "--causation-event-id", "--in-reply-to", "--escalation":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
			}
			params[strings.TrimPrefix(strings.ReplaceAll(args[i], "-", "_"), "__")] = args[i+1]
			i++
		case "--to", "--recipient":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
			}
			recipients = append(recipients, args[i+1])
			i++
		case "--artifact":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--artifact requires a path", protocol.ExitUsage, nil))
			}
			artifacts = append(artifacts, args[i+1])
			i++
		case "--payload":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--payload requires key=value", protocol.ExitUsage, nil))
			}
			key, value, ok := splitKeyValue(args[i+1])
			if !ok {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--payload requires key=value", protocol.ExitUsage, nil))
			}
			payload[key] = parseScalar(value)
			i++
		case "--understanding", "--kind", "--message", "--question", "--answer", "--source", "--progress-status", "--summary", "--reason", "--verdict", "--final-summary", "--urgency":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
			}
			payload[payloadKey(args[i])] = args[i+1]
			i++
		case "--required-change", "--review-focus", "--expected-output", "--included-event-id":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
			}
			appendPayloadString(payload, payloadListKey(args[i]), args[i+1])
			i++
		default:
			return writeProtocolError(stderr, protocol.UnsupportedFeature("delegate "+sub+" "+args[i]))
		}
	}
	if len(recipients) > 0 {
		params["recipients"] = recipients
	}
	if len(artifacts) > 0 {
		params["artifact_source_paths"] = artifacts
	}
	if len(payload) > 0 {
		params["payload"] = payload
	}
	return a.daemonRequestWithParams(stdout, stderr, commandName, params)
}

func (a App) runCancel(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s cancel <session_id> --reason <reason> [--actor <member>] [--cause <cause>] [--command-id <id>]\n", a.Name)
		return protocol.ExitOK
	}
	params := map[string]any{"session_id": args[0]}
	if errExit := parseSimpleFlags(params, args[1:], stderr, map[string]bool{"--reason": true, "--actor": true, "--cause": true, "--command-id": true}); errExit != protocol.ExitOK {
		return errExit
	}
	return a.daemonRequestWithParams(stdout, stderr, "cancel", params)
}

func (a App) runBlock(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s block <session_id> --category <category> --reason <reason> [--actor <member>] [--command-id <id>]\n", a.Name)
		return protocol.ExitOK
	}
	params := map[string]any{"session_id": args[0]}
	if errExit := parseSimpleFlags(params, args[1:], stderr, map[string]bool{"--category": true, "--reason": true, "--actor": true, "--command-id": true}); errExit != protocol.ExitOK {
		return errExit
	}
	return a.daemonRequestWithParams(stdout, stderr, "block", params)
}

func (a App) runResume(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s resume <session_id> --blocked-event <event_id> --reason <reason> [--actor <member>] [--command-id <id>]\n", a.Name)
		return protocol.ExitOK
	}
	params := map[string]any{"session_id": args[0]}
	if errExit := parseSimpleFlags(params, args[1:], stderr, map[string]bool{"--blocked-event": true, "--reason": true, "--actor": true, "--command-id": true}); errExit != protocol.ExitOK {
		return errExit
	}
	if params["blocked_event"] != nil {
		params["blocked_event_id"] = params["blocked_event"]
		delete(params, "blocked_event")
	}
	return a.daemonRequestWithParams(stdout, stderr, "resume", params)
}

func (a App) runLimits(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s limits show <session_id>\n  %s limits extend <session_id> --blocked-event <event_id> --key <name> --value <value> --authorized-by user [--reason <reason>]\n", a.Name, a.Name)
		return protocol.ExitOK
	}
	switch args[0] {
	case "show":
		if len(args) != 2 {
			return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "limits show requires session_id", protocol.ExitUsage, nil))
		}
		return a.daemonRequestWithParams(stdout, stderr, "limits.show", map[string]any{"session_id": args[1]})
	case "extend":
		return a.runLimitsExtend(args[1:], stdout, stderr)
	default:
		return writeProtocolError(stderr, protocol.UnsupportedFeature("limits "+args[0]))
	}
}

func (a App) runLimitsExtend(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "limits extend requires session_id", protocol.ExitUsage, nil))
	}
	params := map[string]any{"session_id": args[0]}
	changes := map[string]any{}
	var pendingKey string
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--blocked-event", "--authorized-by", "--reason", "--actor", "--command-id":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
			}
			params[strings.TrimPrefix(strings.ReplaceAll(args[i], "-", "_"), "__")] = args[i+1]
			i++
		case "--key":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--key requires a value", protocol.ExitUsage, nil))
			}
			pendingKey = args[i+1]
			i++
		case "--value":
			if i+1 >= len(args) || pendingKey == "" {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--value requires a preceding --key", protocol.ExitUsage, nil))
			}
			changes[pendingKey] = parseScalar(args[i+1])
			pendingKey = ""
			i++
		default:
			return writeProtocolError(stderr, protocol.UnsupportedFeature("limits extend "+args[i]))
		}
	}
	if params["blocked_event"] != nil {
		params["blocked_event_id"] = params["blocked_event"]
		delete(params, "blocked_event")
	}
	if len(changes) > 0 {
		params["changes"] = changes
	}
	return a.daemonRequestWithParams(stdout, stderr, "limits.extend", params)
}

func delegationCommandName(sub string) (string, bool) {
	allowed := map[string]struct{}{
		"ack": {}, "message": {}, "clarify": {}, "answer-clarification": {},
		"update": {}, "request-update": {}, "submit": {}, "review": {},
		"review-question": {}, "review-answer": {}, "review-submit": {},
		"revise": {}, "accept": {}, "escalate": {}, "escalation-flush": {},
		"resolve-escalation": {},
	}
	if _, ok := allowed[sub]; !ok {
		return "", false
	}
	return "delegate." + strings.ReplaceAll(sub, "-", "_"), true
}

func parseSimpleFlags(params map[string]any, args []string, stderr io.Writer, allowed map[string]bool) int {
	for i := 0; i < len(args); i++ {
		if !allowed[args[i]] {
			return writeProtocolError(stderr, protocol.UnsupportedFeature(args[i]))
		}
		if i+1 >= len(args) {
			return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
		}
		params[strings.TrimPrefix(strings.ReplaceAll(args[i], "-", "_"), "__")] = args[i+1]
		i++
	}
	return protocol.ExitOK
}

func splitKeyValue(value string) (string, string, bool) {
	key, val, ok := strings.Cut(value, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return "", "", false
	}
	return key, val, true
}

func parseBoolArg(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "y":
		return true, true
	case "false", "0", "no", "n":
		return false, true
	default:
		return false, false
	}
}

func parseScalar(value string) any {
	if parsed, ok := parseBoolArg(value); ok {
		return parsed
	}
	if i, err := strconv.Atoi(value); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return f
	}
	return value
}

func normalizeCouncilSurfaceKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case "discord-thread":
		return "discord_thread"
	case "discord-channel":
		return "discord_channel"
	case "discord-parent-channel":
		return "discord_parent_channel"
	default:
		return strings.TrimSpace(kind)
	}
}

func setCouncilRequestedOutputModeArg(requestContext map[string]any, value string) error {
	newValue := normalizeCouncilRequestedOutputModeForCLI(value)
	if existingRaw, ok := requestContext["requested_output_mode"].(string); ok && existingRaw != "" {
		existing := normalizeCouncilRequestedOutputModeForCLI(existingRaw)
		if existing != newValue {
			return protocol.NewError(protocol.ErrorValidation, "council new output-mode aliases must not conflict", protocol.ExitUsage, nil)
		}
	}
	requestContext["requested_output_mode"] = newValue
	return nil
}

func normalizeCouncilRequestedOutputModeForCLI(mode string) string {
	switch strings.TrimSpace(mode) {
	case "transcript/export-only", "transcript_export_only":
		return "artifact_only"
	case "local-daemon-only", "local_daemon_only":
		return "activation_planning_only"
	default:
		return strings.TrimSpace(mode)
	}
}

func payloadKey(flag string) string {
	return strings.TrimPrefix(strings.ReplaceAll(flag, "-", "_"), "__")
}

func payloadListKey(flag string) string {
	switch flag {
	case "--required-change":
		return "required_changes"
	case "--review-focus":
		return "review_focus"
	case "--expected-output":
		return "expected_outputs"
	case "--included-event-id":
		return "included_event_ids"
	default:
		return payloadKey(flag)
	}
}

func appendPayloadString(payload map[string]any, key, value string) {
	current, _ := payload[key].([]string)
	payload[key] = append(current, value)
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
	response, err := transport.RoundTripWithRuntime(dataHome, a.Runtime, request, daemonCommandTimeout(command))
	if err != nil {
		if response.Error != nil {
			return response, writeProtocolError(stderr, response.Error)
		}
		return response, writeProtocolError(stderr, protocol.ToStructuredError(err))
	}
	return response, protocol.ExitOK
}

func daemonCommandTimeout(command string) time.Duration {
	switch command {
	case "council.grant":
		return 3 * time.Minute
	default:
		return time.Second
	}
}

func safeOperatorOutputPath(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("output path is required")
	}
	clean := filepath.Clean(value)
	if strings.Contains(clean, "\x00") || strings.Contains(clean, "..") {
		return "", fmt.Errorf("output path must not contain NUL or dot-dot segments")
	}
	if filepath.IsAbs(clean) {
		return clean, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	path := filepath.Join(cwd, clean)
	if !strings.HasPrefix(path, cwd+string(os.PathSeparator)) && path != cwd {
		return "", fmt.Errorf("output path escapes current directory")
	}
	return path, nil
}

func writeOperatorFile(path string, content []byte) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("output path must be a regular non-symlink file")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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

func (a App) runCouncil(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		_, _ = fmt.Fprintf(stdout, "Usage:\n  %s council new <title> --members <member[,member...]> --moderator <member> --requested-output-mode live_visible_thread --surface discord_thread --surface-platform discord --thread-id <id> [--request-source discord_thread] [--visible-output-required true]\n  %s council new <title> --members <member[,member...]> --moderator <member> --requested-output-mode local-daemon-only --explicit-non-visible-override true --override-reason <reason>\n  %s council <action> <session_id> [--from <member>] [--command-id <id>] [action flags]\n", a.Name, a.Name, a.Name)
		return protocol.ExitOK
	}
	sub := args[0]
	if sub == "new" {
		return a.runCouncilNew(args[1:], stdout, stderr)
	}
	if _, ok := councilCommandName(sub); !ok {
		return writeProtocolError(stderr, protocol.UnsupportedFeature("council "+sub))
	}
	return a.runCouncilEvent(sub, args[1:], stdout, stderr)
}

func (a App) runCouncilNew(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "council new requires title", protocol.ExitUsage, nil))
	}
	params := map[string]any{"title": args[0]}
	var members []string
	surface := map[string]any{}
	requestContext := map[string]any{}
	linked := map[string]any{}
	limits := map[string]any{}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--session-id":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--session-id requires a value", protocol.ExitUsage, nil))
			}
			params["session_id"] = args[i+1]
			i++
		case "--members":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--members requires a value", protocol.ExitUsage, nil))
			}
			members = append(members, splitCommaList(args[i+1])...)
			i++
		case "--member":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--member requires a value", protocol.ExitUsage, nil))
			}
			members = append(members, args[i+1])
			i++
		case "--moderator", "--event-id", "--command-id", "--turn-mode", "--correlation-id":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
			}
			params[payloadKey(args[i])] = args[i+1]
			i++
		case "--request-source", "--source":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
			}
			requestContext["source"] = args[i+1]
			i++
		case "--requested-output-mode", "--output-mode", "--requested-output":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
			}
			if err := setCouncilRequestedOutputModeArg(requestContext, args[i+1]); err != nil {
				return writeProtocolError(stderr, err)
			}
			i++
		case "--override-reason":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--override-reason requires a value", protocol.ExitUsage, nil))
			}
			requestContext["override_reason"] = args[i+1]
			i++
		case "--explicit-non-visible-override", "--visible-output-required":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires true or false", protocol.ExitUsage, nil))
			}
			value, ok := parseBoolArg(args[i+1])
			if !ok {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires true or false", protocol.ExitUsage, nil))
			}
			requestContext[payloadKey(args[i])] = value
			i++
		case "--surface":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--surface requires a value", protocol.ExitUsage, nil))
			}
			surface["kind"] = normalizeCouncilSurfaceKind(args[i+1])
			i++
		case "--surface-platform":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--surface-platform requires a value", protocol.ExitUsage, nil))
			}
			surface["platform"] = args[i+1]
			i++
		case "--channel-id":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--channel-id requires a value", protocol.ExitUsage, nil))
			}
			surface["channel_id"] = args[i+1]
			i++
		case "--thread-id":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--thread-id requires a value", protocol.ExitUsage, nil))
			}
			surface["thread_id"] = args[i+1]
			i++
		case "--kanban-card":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--kanban-card requires a value", protocol.ExitUsage, nil))
			}
			linked["kanban_card_id"] = args[i+1]
			i++
		case "--vault-decision-note":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--vault-decision-note requires a value", protocol.ExitUsage, nil))
			}
			linked["vault_decision_note"] = args[i+1]
			i++
		case "--limit":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--limit requires key=value", protocol.ExitUsage, nil))
			}
			key, value, ok := splitKeyValue(args[i+1])
			if !ok {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--limit requires key=value", protocol.ExitUsage, nil))
			}
			limits[key] = parseScalar(value)
			i++
		default:
			return writeProtocolError(stderr, protocol.UnsupportedFeature("council new "+args[i]))
		}
	}
	if len(members) > 0 {
		params["members"] = members
	}
	if len(surface) > 0 {
		params["surface"] = surface
	}
	if len(requestContext) > 0 {
		params["request_context"] = requestContext
	}
	if len(linked) > 0 {
		params["linked_authority"] = linked
	}
	if len(limits) > 0 {
		params["limits"] = limits
	}
	return a.daemonRequestWithParams(stdout, stderr, "council.new", params)
}

func (a App) runCouncilEvent(sub string, args []string, stdout io.Writer, stderr io.Writer) int {
	commandName, _ := councilCommandName(sub)
	if len(args) == 0 {
		return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "council "+sub+" requires session_id", protocol.ExitUsage, nil))
	}
	params := map[string]any{"session_id": args[0]}
	payload := map[string]any{}
	linkedResult := map[string]any{}
	var evidence []string
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--from", "--actor":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
			}
			params["actor"] = args[i+1]
			i++
		case "--command-id", "--causation-event-id":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
			}
			params[payloadKey(args[i])] = args[i+1]
			i++
		case "--timeout", "--research-timeout":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
			}
			sec, ok := durationSecondsArg(args[i+1])
			if !ok {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" must be a positive duration", protocol.ExitUsage, nil))
			}
			key := "timeout_sec"
			if args[i] == "--research-timeout" {
				key = "research_timeout_sec"
			}
			payload[key] = sec
			i++
		case "--round":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--round requires a value", protocol.ExitUsage, nil))
			}
			value, ok := positiveIntArg(args[i+1])
			if !ok {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--round must be positive", protocol.ExitUsage, nil))
			}
			payload["turn"] = value
			i++
		case "--relevance", "--urgency", "--max-rounds", "--draft-version":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
			}
			value, ok := positiveIntArg(args[i+1])
			if !ok {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" must be positive", protocol.ExitUsage, nil))
			}
			payload[payloadKey(args[i])] = value
			i++
		case "--to":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--to requires a value", protocol.ExitUsage, nil))
			}
			payload["member"] = args[i+1]
			i++
		case "--mode":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--mode requires a value", protocol.ExitUsage, nil))
			}
			payload["selection_mode"] = args[i+1]
			i++
		case "--auto":
			payload["auto"] = true
		case "--from-file":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--from-file requires a path", protocol.ExitUsage, nil))
			}
			content, err := os.ReadFile(args[i+1])
			if err != nil {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, err.Error(), protocol.ExitUsage, nil))
			}
			key := "draft"
			if sub == "speak" {
				key = "speech"
			}
			payload[key] = string(content)
			i++
		case "--decision-question", "--out-of-scope-policy", "--status", "--summary", "--notes", "--reason", "--intent", "--message", "--speech", "--vote", "--required-change", "--final-summary", "--failure-reason", "--followup-card-id", "--timeout-evidence":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
			}
			payload[payloadKey(args[i])] = args[i+1]
			i++
		case "--authority-return-status":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--authority-return-status requires a value", protocol.ExitUsage, nil))
			}
			linkedResult["status"] = args[i+1]
			i++
		case "--kanban-comment-id", "--kanban-card-id", "--vault-decision-note":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, args[i]+" requires a value", protocol.ExitUsage, nil))
			}
			linkedResult[payloadKey(args[i])] = args[i+1]
			i++
		case "--return-evidence":
			if i+1 >= len(args) {
				return writeProtocolError(stderr, protocol.NewError(protocol.ErrorValidation, "--return-evidence requires a value", protocol.ExitUsage, nil))
			}
			evidence = append(evidence, args[i+1])
			i++
		case "--stdin":
			payload["stdin_requested"] = true
		default:
			return writeProtocolError(stderr, protocol.UnsupportedFeature("council "+sub+" "+args[i]))
		}
	}
	if len(evidence) > 0 {
		linkedResult["evidence"] = evidence
	}
	if len(linkedResult) > 0 {
		payload["linked_authority_result"] = linkedResult
	}
	if len(payload) > 0 {
		params["payload"] = payload
	}
	return a.daemonRequestWithParams(stdout, stderr, commandName, params)
}

func councilCommandName(sub string) (string, bool) {
	if _, ok := councilEventCommands[sub]; !ok {
		return "", false
	}
	return "council." + strings.ReplaceAll(sub, "-", "_"), true
}

var councilEventCommands = map[string]struct{}{
	"request-attendance": {},
	"attend":             {},
	"lock-agenda":        {},
	"prepare":            {},
	"ready":              {},
	"prepared-partial":   {},
	"poll":               {},
	"hand-raise":         {},
	"grant":              {},
	"speak":              {},
	"intervene":          {},
	"propose":            {},
	"revise":             {},
	"request-vote":       {},
	"vote":               {},
	"finalize":           {},
	"unresolved":         {},
}

func splitCommaList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func durationSecondsArg(value string) (int, bool) {
	if i, ok := positiveIntArg(value); ok {
		return i, true
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0, false
	}
	return int(d / time.Second), true
}
