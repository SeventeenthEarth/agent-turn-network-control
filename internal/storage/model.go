package storage

import (
	"encoding/json"
	"time"

	"kkachi-agent-network-control/internal/registry"
)

type SessionType string

const (
	SessionTypeDelegation SessionType = "delegation"
	SessionTypeCouncil    SessionType = "council"
)

type Phase string

const (
	PhaseCreated Phase = "created"
)

type Status string

const (
	StatusOpen     Status = "open"
	StatusBlocked  Status = "blocked"
	StatusTerminal Status = "terminal"
)

type Surface struct {
	Kind          string `yaml:"kind" json:"kind"`
	Platform      string `yaml:"platform,omitempty" json:"platform,omitempty"`
	GuildID       string `yaml:"guild_id,omitempty" json:"guild_id,omitempty"`
	ChannelID     string `yaml:"channel_id,omitempty" json:"channel_id,omitempty"`
	ThreadID      string `yaml:"thread_id,omitempty" json:"thread_id,omitempty"`
	OriginMessage string `yaml:"origin_message_id,omitempty" json:"origin_message_id,omitempty"`
	StartedBy     string `yaml:"started_by,omitempty" json:"started_by,omitempty"`
	DeliveryOwner string `yaml:"delivery_owner,omitempty" json:"delivery_owner,omitempty"`
}

type LinkedAuthority struct {
	KanbanCardID      string `yaml:"kanban_card_id,omitempty" json:"kanban_card_id,omitempty"`
	VaultDecisionNote string `yaml:"vault_decision_note,omitempty" json:"vault_decision_note,omitempty"`
}

type Limits struct {
	MaxTurns                        int            `yaml:"max_turns,omitempty" json:"max_turns,omitempty"`
	MaxConsensusRounds              int            `yaml:"max_consensus_rounds,omitempty" json:"max_consensus_rounds,omitempty"`
	ConsensusRoundWarningThreshold  int            `yaml:"consensus_round_warning_threshold,omitempty" json:"consensus_round_warning_threshold,omitempty"`
	NoProgressRoundLimit            int            `yaml:"no_progress_round_limit,omitempty" json:"no_progress_round_limit,omitempty"`
	PreparationTimeoutSec           int            `yaml:"preparation_timeout_sec,omitempty" json:"preparation_timeout_sec,omitempty"`
	HandRaiseResearchTimeoutSec     int            `yaml:"hand_raise_research_timeout_sec,omitempty" json:"hand_raise_research_timeout_sec,omitempty"`
	DispatchTimeoutSec              int            `yaml:"dispatch_timeout_sec,omitempty" json:"dispatch_timeout_sec,omitempty"`
	ClarificationResponseTimeoutSec int            `yaml:"clarification_response_timeout_sec,omitempty" json:"clarification_response_timeout_sec,omitempty"`
	EscalationResponseTimeoutSec    int            `yaml:"escalation_response_timeout_sec,omitempty" json:"escalation_response_timeout_sec,omitempty"`
	RunnerMaxRetries                int            `yaml:"runner_max_retries,omitempty" json:"runner_max_retries,omitempty"`
	MaxRunnerCalls                  int            `yaml:"max_runner_calls,omitempty" json:"max_runner_calls,omitempty"`
	MaxTokensTotal                  int            `yaml:"max_tokens_total,omitempty" json:"max_tokens_total,omitempty"`
	MaxUSD                          float64        `yaml:"max_usd,omitempty" json:"max_usd,omitempty"`
	MaxUserEscalations              int            `yaml:"max_user_escalations,omitempty" json:"max_user_escalations,omitempty"`
	MaxElapsedSec                   int            `yaml:"max_elapsed_sec,omitempty" json:"max_elapsed_sec,omitempty"`
	MaxArtifactBytes                int            `yaml:"max_artifact_bytes,omitempty" json:"max_artifact_bytes,omitempty"`
	StreamHeartbeatIntervalSec      int            `yaml:"stream_heartbeat_interval_sec,omitempty" json:"stream_heartbeat_interval_sec,omitempty"`
	StreamStaleThresholdSec         int            `yaml:"stream_stale_threshold_sec,omitempty" json:"stream_stale_threshold_sec,omitempty"`
	StreamRepollThresholdSec        int            `yaml:"stream_repoll_threshold_sec,omitempty" json:"stream_repoll_threshold_sec,omitempty"`
	Council                         CouncilLimits  `yaml:"council,omitempty" json:"council,omitempty"`
	Extra                           map[string]any `yaml:",inline,omitempty" json:"-"`
}

type CouncilLimits struct {
	DiscussionQuality *DiscussionQualityLimits `yaml:"discussion_quality,omitempty" json:"discussion_quality,omitempty"`
}

type DiscussionQualityLimits struct {
	Mode                           string `yaml:"mode,omitempty" json:"mode,omitempty"`
	OpeningUnlinkedTurns           int    `yaml:"opening_unlinked_turns,omitempty" json:"opening_unlinked_turns,omitempty"`
	RequireClaims                  bool   `yaml:"require_claims,omitempty" json:"require_claims,omitempty"`
	RequireStanceLinksAfterOpening bool   `yaml:"require_stance_links_after_opening,omitempty" json:"require_stance_links_after_opening,omitempty"`
	AllowNewAxisWithReason         bool   `yaml:"allow_new_axis_with_reason,omitempty" json:"allow_new_axis_with_reason,omitempty"`
	MaxConsecutiveNewAxis          int    `yaml:"max_consecutive_new_axis,omitempty" json:"max_consecutive_new_axis,omitempty"`
}

type SessionState struct {
	Phase          Phase  `yaml:"phase" json:"phase"`
	CurrentTurn    int    `yaml:"current_turn" json:"current_turn"`
	LastSpeaker    string `yaml:"last_speaker,omitempty" json:"last_speaker,omitempty"`
	ConsensusRound int    `yaml:"consensus_round" json:"consensus_round"`
}

type CostSummary struct {
	TokensInTotal               int     `yaml:"tokens_in_total" json:"tokens_in_total"`
	TokensOutTotal              int     `yaml:"tokens_out_total" json:"tokens_out_total"`
	USDEstimateTotal            float64 `yaml:"usd_estimate_total" json:"usd_estimate_total"`
	RunnerCallsTotal            int     `yaml:"runner_calls_total" json:"runner_calls_total"`
	MissingCostRunnerCallsTotal int     `yaml:"missing_cost_runner_calls_total" json:"missing_cost_runner_calls_total"`
}

type EscalationSummary struct {
	UserEscalationsTotal          int `yaml:"user_escalations_total" json:"user_escalations_total"`
	PendingBatchesTotal           int `yaml:"pending_batches_total" json:"pending_batches_total"`
	PendingBatchedCandidatesTotal int `yaml:"pending_batched_candidates_total" json:"pending_batched_candidates_total"`
	WaitingUser                   any `yaml:"waiting_user" json:"waiting_user"`
}

type SessionMetadata struct {
	ID               string                    `yaml:"id" json:"id"`
	SessionType      SessionType               `yaml:"session_type" json:"session_type"`
	Status           Status                    `yaml:"status" json:"status"`
	Title            string                    `yaml:"title" json:"title"`
	Moderator        string                    `yaml:"moderator" json:"moderator"`
	Participants     []string                  `yaml:"participants" json:"participants"`
	Surface          *Surface                  `yaml:"surface,omitempty" json:"surface,omitempty"`
	LinkedAuthority  *LinkedAuthority          `yaml:"linked_authority,omitempty" json:"linked_authority,omitempty"`
	TurnMode         string                    `yaml:"turn_mode,omitempty" json:"turn_mode,omitempty"`
	CreatedAt        time.Time                 `yaml:"created_at" json:"created_at"`
	Limits           Limits                    `yaml:"limits" json:"limits"`
	State            SessionState              `yaml:"state" json:"state"`
	Cost             CostSummary               `yaml:"cost" json:"cost"`
	Escalations      EscalationSummary         `yaml:"escalations" json:"escalations"`
	RegistrySnapshot registry.SnapshotMetadata `yaml:"registry_snapshot" json:"registry_snapshot"`
}

type SessionSpec struct {
	ID              string
	SessionType     SessionType
	Title           string
	Moderator       string
	Participants    []string
	Surface         *Surface
	LinkedAuthority *LinkedAuthority
	TurnMode        string
	Limits          Limits
	EventID         string
	CommandID       string
	CorrelationID   string
}

type RunnerInfo struct {
	InvocationID    string   `json:"invocation_id"`
	AdapterKind     string   `json:"adapter_kind"`
	Member          string   `json:"member"`
	Attempt         int      `json:"attempt"`
	IsRetry         bool     `json:"is_retry"`
	SourceCommandID string   `json:"source_command_id,omitempty"`
	Status          string   `json:"status"`
	DurationSec     *float64 `json:"duration_sec,omitempty"`
}

type EventEnvelope struct {
	SchemaVersion    int             `json:"schema_version"`
	EventID          string          `json:"event_id"`
	CommandID        string          `json:"command_id,omitempty"`
	CausationEventID string          `json:"causation_event_id,omitempty"`
	CorrelationID    string          `json:"correlation_id,omitempty"`
	SessionID        string          `json:"session_id"`
	SessionType      SessionType     `json:"session_type"`
	Turn             *int            `json:"turn,omitempty"`
	Phase            Phase           `json:"phase"`
	Type             string          `json:"type"`
	From             string          `json:"from"`
	To               []string        `json:"to"`
	CreatedAt        time.Time       `json:"created_at"`
	Runner           *RunnerInfo     `json:"runner,omitempty"`
	Cost             json.RawMessage `json:"cost,omitempty"`
	Payload          map[string]any  `json:"payload"`
}

type AppendResult struct {
	Cursor  string `json:"cursor"`
	Offset  int64  `json:"offset"`
	EventID string `json:"event_id"`
}

type LogIndex struct {
	Events   []EventEnvelope
	EventIDs map[string]struct{}
}
