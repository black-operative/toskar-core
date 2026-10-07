// Package automations models daemon-owned scheduled prompts.
package automations

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// NotifyMode chooses whether a finished run should notify the user.
type NotifyMode string

const (
	NotifyAlways      NotifyMode = "always"
	NotifyOnCondition NotifyMode = "condition"
	NotifyOnChange    NotifyMode = "change"
	NotifyNone        NotifyMode = "none"
	// NotifyOnFailure notifies only when a run fails.
	NotifyOnFailure NotifyMode = "failure"
)

// Condition is the structured rule for NotifyOnCondition.
// Price checks use threshold. Stock checks use available. A model can mark a result significant.
type Condition struct {
	Kind string `json:"kind"`
	// Op is below or above. It applies to a threshold.
	Op    string  `json:"op,omitempty"`
	Value float64 `json:"value,omitempty"`
	// Currency is the ISO 4217 code of a threshold's value, such as EUR. The
	// run is asked for the price in this currency, so the numbers compare as
	// they are. Empty means US dollars, as before currencies were recorded.
	Currency string `json:"currency,omitempty"`
}

const (
	ConditionThreshold   = "threshold"
	ConditionAvailable   = "available"
	ConditionSignificant = "significant"
	OpBelow              = "below"
	OpAbove              = "above"
)

// Notification is stored with the automation and evaluated after the run result is saved.
type Notification struct {
	Mode      NotifyMode `json:"mode"`
	Condition *Condition `json:"condition,omitempty"`
}

// Normalize sets the default mode when the caller left it empty.
func (n *Notification) Normalize() {
	if n.Mode == "" {
		n.Mode = NotifyAlways
	}
}

// Validate checks the mode and, for a condition, the rule that will be evaluated later.
func (n Notification) Validate() error {
	switch n.Mode {
	case NotifyAlways, NotifyOnCondition, NotifyOnChange, NotifyNone, NotifyOnFailure:
	default:
		return fmt.Errorf("unknown notification mode %q", n.Mode)
	}
	if n.Mode == NotifyOnCondition && n.Condition == nil {
		return fmt.Errorf("notification condition is required")
	}
	if n.Condition != nil {
		return n.Condition.Validate()
	}
	return nil
}

// Validate checks a notification rule.
func (c Condition) Validate() error {
	switch c.Kind {
	case ConditionThreshold:
		switch c.Op {
		case OpBelow, OpAbove:
		default:
			return fmt.Errorf("threshold requires op %q or %q", OpBelow, OpAbove)
		}
		if c.Currency != "" && !isCurrencyCode(c.Currency) {
			return fmt.Errorf("currency %q is not a three-letter ISO 4217 code such as EUR", c.Currency)
		}
	case ConditionAvailable, ConditionSignificant:
	default:
		return fmt.Errorf("unknown notification condition %q", c.Kind)
	}
	return nil
}

func isCurrencyCode(code string) bool {
	if len(code) != 3 {
		return false
	}
	for _, r := range code {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

// Automation is a scheduled prompt owned by the daemon.
type Automation struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Enabled   bool     `json:"enabled"`
	Schedule  Schedule `json:"schedule"`
	Prompt    string   `json:"prompt"`
	ProfileID string   `json:"profile_id,omitempty"`
	// ModelID is the installed model this scheduled prompt runs.
	ModelID      string       `json:"model_id,omitempty"`
	Tools        []string     `json:"tools"`
	Notification Notification `json:"notification"`
	// ResponseLanguage is the language results are written in (multilingual
	// spec §22): "" or "account" follows the assistant language setting,
	// "app" the App language, "auto" the language the request is written
	// in, or a BCP 47 tag such as "de". A language the request asks for
	// always wins.
	ResponseLanguage string `json:"response_language,omitempty"`
	// ConversationID is the chat the automation was made from, and DraftID
	// the draft that chat showed (#204). Both are empty for one made
	// elsewhere.
	ConversationID string `json:"conversation_id,omitempty"`
	DraftID        string `json:"draft_id,omitempty"`
	// SaveFolder, when set, is a folder each result is also saved to as a
	// Markdown file (#204), such as ~/Documents/Toskar/News.
	SaveFolder string    `json:"save_folder,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	// NextRunAt is the occurrence the daemon should execute next.
	// A missed restart keeps only the latest missed occurrence here.
	NextRunAt *time.Time `json:"next_run_at,omitempty"`
	// LastRunAt is when the previous occurrence finished. Zero means never run.
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
	// ConsecutiveFailures counts terminal failures in a row. A success clears it.
	ConsecutiveFailures int `json:"consecutive_failures"`
	// LastError is the most recent failure, including one that is waiting to retry.
	LastError string `json:"last_error,omitempty"`
	// LastStatus and LastResult summarize the newest occurrence. List fills them.
	LastStatus RunStatus `json:"last_status,omitempty"`
	LastResult string    `json:"last_result,omitempty"`
}

// SetNextRun stores the occurrence NextRun selects for now.
func (a *Automation) SetNextRun(now time.Time) error {
	last := time.Time{}
	if a.LastRunAt != nil {
		last = *a.LastRunAt
	}
	next, ok, err := a.Schedule.NextRun(a.CreatedAt, last, now)
	if err != nil {
		return err
	}
	if !ok {
		a.NextRunAt = nil
		return nil
	}
	next = next.UTC().Truncate(time.Second)
	a.NextRunAt = &next
	return nil
}

// CreateInput is the caller-supplied body of a new automation.
type CreateInput struct {
	Name         string       `json:"name"`
	Enabled      *bool        `json:"enabled,omitempty"`
	Schedule     Schedule     `json:"schedule"`
	Prompt       string       `json:"prompt"`
	ProfileID    string       `json:"profile_id,omitempty"`
	ModelID      string       `json:"model_id,omitempty"`
	Tools        []string     `json:"tools,omitempty"`
	Notification Notification `json:"notification"`
	// ResponseLanguage: see Automation.
	ResponseLanguage string `json:"response_language,omitempty"`
	// ConversationID and DraftID: see Automation. Creating a draft again
	// returns the automation it already made.
	ConversationID string `json:"conversation_id,omitempty"`
	DraftID        string `json:"draft_id,omitempty"`
	// SaveFolder: see Automation.
	SaveFolder string `json:"save_folder,omitempty"`
}

// Patch updates the fields that are non-nil.
type Patch struct {
	Name         *string       `json:"name,omitempty"`
	Enabled      *bool         `json:"enabled,omitempty"`
	Schedule     *Schedule     `json:"schedule,omitempty"`
	Prompt       *string       `json:"prompt,omitempty"`
	ProfileID    *string       `json:"profile_id,omitempty"`
	ModelID      *string       `json:"model_id,omitempty"`
	Tools        *[]string     `json:"tools,omitempty"`
	Notification *Notification `json:"notification,omitempty"`
	// ResponseLanguage: see Automation; "" goes back to the account's.
	ResponseLanguage *string `json:"response_language,omitempty"`
	// SaveFolder: see Automation; "" stops saving.
	SaveFolder *string `json:"save_folder,omitempty"`
}

// Response languages an automation can have besides a language tag.
const (
	ResponseAccount = "account"
	ResponseApp     = "app"
	ResponseAuto    = "auto"
)

var languageTag = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// ValidResponseLanguage reports a response language an automation can have.
func ValidResponseLanguage(v string) bool {
	switch v {
	case "", ResponseAccount, ResponseApp, ResponseAuto:
		return true
	}
	return len(v) <= 35 && languageTag.MatchString(v)
}

// ValidateDraft checks the fields required to store an automation.
func ValidateDraft(name, prompt, modelID string, tools []string, notification Notification, schedule Schedule) error {
	if err := validateIdentity(name, prompt, modelID, tools, notification); err != nil {
		return err
	}
	return schedule.Validate()
}

func validateIdentity(name, prompt, modelID string, tools []string, notification Notification) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("prompt is required")
	}
	if strings.TrimSpace(modelID) == "" {
		return fmt.Errorf("model is required")
	}
	for _, id := range tools {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("tool id is required")
		}
	}
	return notification.Validate()
}
