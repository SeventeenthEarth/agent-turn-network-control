package protocol

type CommandRequest struct {
	SchemaVersion int            `json:"schema_version"`
	RequestID     string         `json:"request_id"`
	Command       string         `json:"command"`
	Params        map[string]any `json:"params,omitempty"`
}

type CommandResponse struct {
	SchemaVersion int              `json:"schema_version"`
	RequestID     string           `json:"request_id"`
	OK            bool             `json:"ok"`
	Result        map[string]any   `json:"result,omitempty"`
	Error         *StructuredError `json:"error,omitempty"`
}

func NewRequest(requestID, command string, params map[string]any) CommandRequest {
	return CommandRequest{SchemaVersion: SchemaVersion, RequestID: requestID, Command: command, Params: params}
}

func SuccessResponse(request CommandRequest, result map[string]any) CommandResponse {
	return CommandResponse{SchemaVersion: SchemaVersion, RequestID: request.RequestID, OK: true, Result: result}
}

func ErrorResponse(request CommandRequest, err error) CommandResponse {
	return CommandResponse{SchemaVersion: SchemaVersion, RequestID: request.RequestID, OK: false, Error: ToStructuredError(err)}
}
