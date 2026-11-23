// Package messages holds what the cli-tool and the controller say to each
// other. The contract is architecture.md. This package is its form in code,
// shared by both programs so the two sides cannot drift apart.
package messages

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
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

// CallerHeader carries the caller token on every API call but the first.
const CallerHeader = "Gangway-Caller"

// CallerPrefix starts every caller token.
const CallerPrefix = "gwc_"

// KeyHash is the form of a session key that the controller may hold: the
// SHA-256 of the whole key, prefix included, in hex.
func KeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// PlainWebAddress tells whether s is an address that may be shown in the
// paste line: it starts with http:// or https://, names a host, and holds
// nothing but letters, digits and the usual marks of an address.
func PlainWebAddress(s string) bool {
	rest, ok := strings.CutPrefix(s, "https://")
	if !ok {
		if rest, ok = strings.CutPrefix(s, "http://"); !ok {
			return false
		}
	}
	if rest == "" || strings.ContainsAny(rest[:1], "/:?#@") || len(s) > 2000 {
		return false
	}
	for _, r := range rest {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("-._~:/?#[]@!$&'()*+,;=%", r):
		default:
			return false
		}
	}
	return true
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
	TypeTimeUp       = "time_up"  // the session's time is up
	TypeExtended     = "extended" // the user extended the session

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
	ReasonTimeUp           = "time_up"      // the session's time is up
	ReasonUnknownKind      = "unknown_kind" // the cli-tool does not know this kind of request
	ReasonAIGone           = "ai_gone"      // never shown to an AI
)

// The ways a command ends.
const (
	HowExit               = "exit"
	HowKilledByUser       = "killed_by_user"
	HowKilledByAI         = "killed_by_ai"
	HowKilledNoAnswer     = "killed_no_answer"
	HowKilledTimeUp       = "killed_time_up" // the session's time was up at the command's time limit
	HowStartFailed        = "start_failed"
	HowKilledAIGone       = "killed_ai_gone"       // never shown to an AI
	HowKilledSessionEnded = "killed_session_ended" // never shown to an AI
)

// The reasons a session ends.
const (
	EndUser              = "user"               // the user ended the cli-tool
	EndConnectionLost    = "connection_lost"    // noted by the side that saw it
	EndControllerStopped = "controller_stopped" // the controller is shutting down
	EndTimeUp            = "time_up"            // the time ran out: the user did not extend, or the maximum was reached
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

	// session_ok: the address the AI calls, for the paste line
	APIAddress string `json:"api_address,omitempty"`

	// session_start
	KeyHash string  `json:"key_hash,omitempty"`
	Host    string  `json:"host,omitempty"`
	Limits  *Limits `json:"limits,omitempty"`

	// session_start: the length the session starts with, in seconds
	LengthSeconds int `json:"length_seconds,omitempty"`

	// extended: the time the session has left, in seconds
	SecondsLeft int `json:"seconds_left,omitempty"`
}

// MaxMessageBytes is the largest message either side sends or takes. A
// piece of output must be cut into messages that stay under it.
const MaxMessageBytes = 1 << 20
