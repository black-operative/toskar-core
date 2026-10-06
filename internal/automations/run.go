package automations

import "time"

// RunStatus is the lifecycle of one scheduled occurrence.
type RunStatus string

const (
	RunClaimed   RunStatus = "claimed"
	RunRunning   RunStatus = "running"
	RunRetrying  RunStatus = "retrying"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
)

// Run is the durable record for one occurrence of an automation.
type Run struct {
	ID               string     `json:"id"`
	AutomationID     string     `json:"automation_id"`
	OccurrenceAt     time.Time  `json:"occurrence_at"`
	Status           RunStatus  `json:"status"`
	ClaimedAt        *time.Time `json:"claimed_at,omitempty"`
	LeaseUntil       *time.Time `json:"lease_until,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	Result           string     `json:"result,omitempty"`
	Error            string     `json:"error,omitempty"`
	NotificationSent bool       `json:"notification_sent"`
	ModelID          string     `json:"model_id,omitempty"`
	NodeID           string     `json:"node_id,omitempty"`
	Attempt          int        `json:"attempt"`
	RetryAt          *time.Time `json:"retry_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

// Detail is an automation and its newest runs, newest occurrence first.
type Detail struct {
	Automation
	History []Run `json:"history"`
	// HistoryMore says older runs exist; GET /automations/{id}/runs pages them.
	HistoryMore bool `json:"history_more"`
}

// RunsPage is one page of an automation's runs, newest first.
type RunsPage struct {
	Runs []Run `json:"runs"`
	// More says older runs exist; ask again with before set to the last run's id.
	More bool `json:"more"`
}

const (
	// HistoryPage runs come with an automation, and by default in a page.
	HistoryPage = 20
	// MaxHistoryPage is the most a page may ask for.
	MaxHistoryPage = 100
)

// Execution is the outcome of running a scheduled prompt.
type Execution struct {
	Text    string
	ModelID string
	NodeID  string
	// Skipped lists tools the run reached that nobody approved for it.
	Skipped []string
}
