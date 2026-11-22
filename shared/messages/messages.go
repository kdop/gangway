// Package messages holds what the cli-tool and the controller say to each
// other. The contract is architecture.md. This package is its form in code,
// shared by both programs so the two sides cannot drift apart.
package messages

import (
	"crypto/sha256"
	"encoding/hex"
)

// HostPath is the path on the controller where the cli-tool opens its
// websocket.
const HostPath = "/host/v1"

// DefaultListen is where the controller listens when nothing else is set.
const DefaultListen = "127.0.0.1:8642"

// HostDoorPort is where the controller's door for the hosts answers on
// the Tailscale network.
const HostDoorPort = 8643

// DefaultControllerName is the controller's name on the Tailscale network
// when nothing else is set.
const DefaultControllerName = "gangway-controller"

// KeyPrefix starts every session key.
const KeyPrefix = "gw_"

// KeyHash is the form of a session key that the controller may hold: the
// SHA-256 of the whole key, prefix included, in hex.
func KeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// The types of message on the connection.
const (
	// From the cli-tool.
	TypeSessionStart = "session_start"
	TypeRefused      = "refused"
	TypeStarted      = "started"
	TypeOutput       = "output"
	TypeStillRunning = "still_running"
	TypeEnded        = "ended"

	// From the controller.
	TypeSessionOK = "session_ok"
	TypeRequest   = "request"
	TypeWait      = "wait"
	TypeStop      = "stop"
	TypeCancel    = "cancel"

	// From either side.
	TypeSessionEnd = "session_end"
)

// KindCommand is the kind of request that runs a command.
const KindCommand = "command"

// The reasons a request is refused.
const (
	ReasonUser             = "user"
	ReasonControlCharacter = "control_character"
	ReasonLineFull         = "line_full"
	ReasonSessionEnded     = "session_ended"
	ReasonUnknownKind      = "unknown_kind" // the cli-tool does not know this kind of request
	ReasonAIGone           = "ai_gone"      // never shown to an AI
)

// The ways a command ends.
const (
	HowExit               = "exit"
	HowKilledByUser       = "killed_by_user"
	HowKilledByAI         = "killed_by_ai"
	HowKilledNoAnswer     = "killed_no_answer"
	HowStartFailed        = "start_failed"
	HowKilledAIGone       = "killed_ai_gone"       // never shown to an AI
	HowKilledSessionEnded = "killed_session_ended" // never shown to an AI
)

// The reasons a session ends.
const (
	EndUser              = "user"               // the user ended the cli-tool
	EndConnectionLost    = "connection_lost"    // noted by the side that saw it
	EndControllerStopped = "controller_stopped" // the controller is shutting down
)

// Limits are the limits of a session, told to the AI up front.
type Limits struct {
	CommandSeconds int `json:"command_seconds"`
	AnswerSeconds  int `json:"answer_seconds"`
	MaxWaiting     int `json:"max_waiting"`
}

// Message is one JSON object on the connection. Type says which fields
// count; the others are left out.
type Message struct {
	Type string `json:"type"`

	// request, refused, started, output, still_running, wait, stop, cancel, ended
	ID string `json:"id,omitempty"`

	// request
	Kind    string `json:"kind,omitempty"`
	Command string `json:"command,omitempty"`

	// refused, session_end
	Reason string `json:"reason,omitempty"`

	// output
	Text string `json:"text,omitempty"`

	// ended. ExitCode is set only when How is "exit".
	How           string `json:"how,omitempty"`
	ExitCode      *int   `json:"exit_code,omitempty"`
	Cut           bool   `json:"cut,omitempty"`
	BytesReplaced bool   `json:"bytes_replaced,omitempty"`

	// session_start
	KeyHash string  `json:"key_hash,omitempty"`
	Host    string  `json:"host,omitempty"`
	Limits  *Limits `json:"limits,omitempty"`
}

// MaxMessageBytes is the largest message either side sends or takes. A
// piece of output must be cut into messages that stay under it.
const MaxMessageBytes = 1 << 20
