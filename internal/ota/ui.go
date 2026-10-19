package ota

// deckd update shows an update on the device's screen through deckd keys: deckd listens on
// UISocket (root only) for UIRequests, one JSON object per line, and
// answers a confirm with a UIReply once a key is pressed. The screen goes
// back to normal when the connection closes.
const UISocket = "/run/deckd-update.sock"

// UIRequest asks deckd to show the confirm screen or a progress step.
type UIRequest struct {
	Confirm  *ConfirmRequest `json:"confirm,omitempty"`
	Progress string          `json:"progress,omitempty"`
	Step     string          `json:"step,omitempty"`  // being written, e.g. Kernel
	Done     int             `json:"done,omitempty"`  // steps done
	Total    int             `json:"total,omitempty"` // of this many
}

// StepFailed is the progress step for a failed install.
const StepFailed = "Failed"

// ConfirmRequest describes the update to confirm.
type ConfirmRequest struct {
	Version string `json:"version"`
	Flavour string `json:"flavour"`
	Older   bool   `json:"older"` // not newer than the installed build
}

// UIReply answers a confirm: OK if the user chose Install.
type UIReply struct {
	OK bool `json:"ok"`
}

// ReadyFile is where deckd writes its PID once it's drawn its first page:
// how deckd update tells deckd keys is healthy after an update.
const ReadyFile = "/run/deckd.ready"
