package ratings

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Tags are the reasons a rating may give.
var Tags = []string{"great_responses", "fast", "slow", "stable", "crashed", "too_much_memory",
	"great_for_coding", "great_for_chat", "good_tool_use", "poor_tool_use"}

// When to ask for a rating: after a model has answered this many times on
// at least this many days.
const (
	askAfterReplies = 10
	askAfterDays    = 2
)

// summaryMaxAge is how long a downloaded summary is used before another.
const summaryMaxAge = 24 * time.Hour

// Settings are the keys this package keeps.
const (
	// SettingShow shows community ratings, downloading the public summary
	// once a day while models are browsed. Off by default.
	SettingShow = "community_ratings"
	// SettingAsk asks for a rating after a model has been used a while.
	SettingAsk = "ratings_prompts"
	// settingClient is the random rating ID, never derived from hardware.
	settingClient = "ratings_client_id"
)

// SettingsStore keeps settings.
type SettingsStore interface {
	GetBool(ctx context.Context, key string, def bool) (bool, error)
	GetString(ctx context.Context, key, def string) (string, error)
	Set(ctx context.Context, key, value string) error
}

// Service is a person's ratings and the community's.
type Service struct {
	DB       *sql.DB
	Settings SettingsStore
	Client   *Client
	// Hardware is this computer's inventory.
	Hardware func(ctx context.Context) (contracts.HardwareInventory, error)
	// Models lists the catalog and installed models.
	Models func(ctx context.Context) ([]contracts.Model, error)
	// Record notes data leaving this computer.
	Record func(ctx context.Context, destination, detail string)
	// OutOfMemory says whether a failed start's error means the model ran
	// out of memory.
	OutOfMemory func(text string) bool
	// LocalNode is this computer's node ID, to leave out replies that ran
	// on a paired computer.
	LocalNode  func() string
	AppVersion string
	Now        func() time.Time

	fetch sync.Mutex
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) record(ctx context.Context, destination, detail string) {
	if s.Record != nil {
		s.Record(ctx, destination, detail)
	}
}

// Input is a rating to save.
type Input struct {
	Stars int      `json:"stars"`
	Tags  []string `json:"tags"`
	// Share sends the rating to the community; false keeps it on this
	// computer and withdraws it if it was shared.
	Share bool `json:"share"`
	// Observations includes how the model runs here in a shared rating.
	Observations bool `json:"observations"`
	// Language is the language the model was used in, such as es, when the
	// person says (multilingual spec §23); "" when they don't.
	Language string `json:"language,omitempty"`
}

// View is a person's rating of one model and what sharing it would send.
type View struct {
	ModelID string `json:"model_id"`
	// Rateable is false when the model cannot be compared with others'
	// ratings; Reason says why. It can still be rated on this computer.
	Rateable bool     `json:"rateable"`
	Reason   string   `json:"reason,omitempty"`
	Stars    int      `json:"stars,omitempty"`
	Tags     []string `json:"tags"`
	// Language is the language the rating is for, or "".
	Language  string     `json:"language,omitempty"`
	Shared    bool       `json:"shared"`
	SharedAt  *time.Time `json:"shared_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	// ShareObservations is whether the shared rating includes how the model
	// runs here.
	ShareObservations bool `json:"share_observations"`
	// Observations are how the model ran here in the last 30 days, exactly
	// as sharing them would send; nil when nothing was measured.
	Observations *Observations `json:"observations,omitempty"`
	// Ask is true when it is a good time to ask for a rating.
	Ask bool `json:"ask"`
	// Shares is exactly what sharing sends, besides the stars, tags, a
	// random rating ID, and the app version.
	Shares *Shares `json:"shares,omitempty"`
}

// Shares is what a shared rating says about the model and computer.
type Shares struct {
	Destination string   `json:"destination"`
	Model       Identity `json:"model"`
	Hardware    Hardware `json:"hardware"`
}

// ErrStars is a rating outside 1 to 5 stars or with unknown tags.
var ErrStars = errors.New("a rating is 1 to 5 stars, with tags from the list")

// ErrNoModel is a model ID neither in the catalog nor installed.
var ErrNoModel = errors.New("no model has that ID")

// ErrNotShareable is a request to share a rating of a model that cannot be
// compared.
var ErrNotShareable = errors.New("this model cannot be compared with others' ratings, so its rating stays on this computer")

// ShareError is a rating saved on this computer that could not be shared or
// withdrawn.
type ShareError struct{ Err error }

func (e *ShareError) Error() string {
	return "the rating is saved on this computer, but the ratings service could not be reached: " + e.Err.Error()
}
func (e *ShareError) Unwrap() error { return e.Err }

func (s *Service) model(ctx context.Context, id string) (contracts.Model, bool) {
	list, err := s.Models(ctx)
	if err != nil {
		return contracts.Model{}, false
	}
	for _, m := range list {
		if m.ID == id {
			return m, true
		}
	}
	return contracts.Model{}, false
}

// shares is what sharing a rating of m would send, or an error saying why
// it cannot be shared.
func (s *Service) shares(ctx context.Context, m contracts.Model) (*Shares, error) {
	inv, err := s.Hardware(ctx)
	if err != nil {
		return nil, err
	}
	h, ok := Normalize(inv)
	if !ok {
		return nil, errors.New("ratings are compared on macOS, Linux, and Windows computers with arm64 or x86-64 processors")
	}
	id, err := Identify(m, Backend(inv))
	if err != nil {
		return nil, err
	}
	return &Shares{Destination: s.Client.ServiceHost(), Model: id, Hardware: h}, nil
}

type row struct {
	stars     int
	tags      []string
	language  string
	remoteKey string
	shareObs  bool
	sharedAt  *time.Time
	updatedAt time.Time
}

func (s *Service) load(ctx context.Context, id string) (*row, error) {
	var r row
	var tags string
	var key, shared sql.NullString
	var updated string
	err := s.DB.QueryRowContext(ctx, `SELECT stars, tags, language, remote_key, share_observations, shared_at, updated_at FROM model_ratings WHERE model_id = ?`, id).
		Scan(&r.stars, &tags, &r.language, &key, &r.shareObs, &shared, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(tags), &r.tags)
	r.remoteKey = key.String
	if t, err := time.Parse(time.RFC3339, shared.String); err == nil {
		r.sharedAt = &t
	}
	r.updatedAt, _ = time.Parse(time.RFC3339, updated)
	return &r, nil
}

// Get is a person's rating of a model.
func (s *Service) Get(ctx context.Context, modelID string) (View, error) {
	m, ok := s.model(ctx, modelID)
	if !ok {
		return View{}, ErrNoModel
	}
	v := View{ModelID: modelID, Tags: []string{}}
	if sh, err := s.shares(ctx, m); err != nil {
		v.Reason = err.Error()
	} else {
		v.Rateable, v.Shares = true, sh
		if v.Observations, err = s.Observe(ctx, modelID); err != nil {
			return View{}, err
		}
	}
	r, err := s.load(ctx, modelID)
	if err != nil {
		return View{}, err
	}
	if r != nil {
		v.Stars, v.Tags, v.Language, v.Shared, v.SharedAt = r.stars, r.tags, r.language, r.remoteKey != "", r.sharedAt
		v.ShareObservations = r.shareObs && v.Shared
		v.UpdatedAt = &r.updatedAt
		if v.Tags == nil {
			v.Tags = []string{}
		}
	} else if m.Installed {
		v.Ask = s.shouldAsk(ctx, modelID)
	}
	return v, nil
}

// shouldAsk is true once a model has answered enough on enough days, while
// asking is on and the person has not said not to.
func (s *Service) shouldAsk(ctx context.Context, modelID string) bool {
	if on, err := s.Settings.GetBool(ctx, SettingAsk, true); err != nil || !on {
		return false
	}
	var dismissed int
	if s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_rating_prompts WHERE model_id = ?`, modelID).Scan(&dismissed) != nil || dismissed > 0 {
		return false
	}
	var replies, days int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(DISTINCT substr(created_at, 1, 10)) FROM generation_metrics WHERE model_id = ?`, modelID).
		Scan(&replies, &days); err != nil {
		return false
	}
	return replies >= askAfterReplies && days >= askAfterDays
}

// Dismiss stops asking for a rating of a model.
func (s *Service) Dismiss(ctx context.Context, modelID string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO model_rating_prompts (model_id, dismissed_at) VALUES (?, ?) ON CONFLICT(model_id) DO NOTHING`,
		modelID, s.now().UTC().Format(time.RFC3339))
	return err
}

func validInput(in Input) bool {
	if in.Stars < 1 || in.Stars > 5 || len(in.Tags) > len(Tags) {
		return false
	}
	seen := map[string]bool{}
	for _, t := range in.Tags {
		if !slices.Contains(Tags, t) || seen[t] {
			return false
		}
		seen[t] = true
	}
	return true
}

// clientID is the random rating ID, made the first time a rating is shared.
func (s *Service) clientID(ctx context.Context) (string, error) {
	id, err := s.Settings.GetString(ctx, settingClient, "")
	if err != nil || id != "" {
		return id, err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id = hex.EncodeToString(b)
	return id, s.Settings.Set(ctx, settingClient, id)
}

// Put saves a person's rating, and shares or withdraws it as they chose.
// A *ShareError means it is saved here but the service could not be reached.
func (s *Service) Put(ctx context.Context, modelID string, in Input) (View, error) {
	if !validInput(in) {
		return View{}, ErrStars
	}
	lang, ok := RatingLanguage(in.Language)
	if !ok {
		return View{}, ErrLanguage
	}
	in.Language = lang
	m, ok := s.model(ctx, modelID)
	if !ok {
		return View{}, ErrNoModel
	}
	var sh *Shares
	if in.Share {
		var err error
		if sh, err = s.shares(ctx, m); err != nil {
			return View{}, ErrNotShareable
		}
	}
	if in.Tags == nil {
		in.Tags = []string{}
	}
	tags, _ := json.Marshal(in.Tags)
	now := s.now().UTC().Format(time.RFC3339)
	if _, err := s.DB.ExecContext(ctx, `
		INSERT INTO model_ratings (model_id, stars, tags, language, share_observations, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(model_id) DO UPDATE SET stars = excluded.stars, tags = excluded.tags, language = excluded.language,
			share_observations = excluded.share_observations, updated_at = excluded.updated_at`,
		modelID, in.Stars, string(tags), in.Language, in.Share && in.Observations, now); err != nil {
		return View{}, err
	}
	prev, err := s.load(ctx, modelID)
	if err != nil {
		return View{}, err
	}
	var shareErr error
	switch {
	case in.Share:
		shareErr = s.share(ctx, modelID, sh, in)
	case prev.remoteKey != "":
		shareErr = s.withdraw(ctx, modelID, prev.remoteKey)
	}
	v, err := s.Get(ctx, modelID)
	if err != nil {
		return View{}, err
	}
	if shareErr != nil {
		return v, &ShareError{Err: shareErr}
	}
	return v, nil
}

func (s *Service) share(ctx context.Context, modelID string, sh *Shares, in Input) error {
	client, err := s.clientID(ctx)
	if err != nil {
		return err
	}
	r := Rating{SchemaVersion: 1, ClientID: client, Stars: in.Stars, Tags: in.Tags, AppVersion: appVersion(s.AppVersion), Language: in.Language,
		Model:    model{ID: sh.Model.ID, Format: sh.Model.Format, Quantization: sh.Model.Quantization},
		Runtime:  runtime{Type: sh.Model.Runtime, Backend: sh.Model.Backend},
		Hardware: sh.Hardware}
	if len(r.Tags) == 0 {
		r.Tags = nil
	}
	detail := "Shared a " + itoa(in.Stars) + "-star rating of " + sh.Model.ID + " " + sh.Model.Quantization + " on " + sh.Hardware.Key()
	if in.Language != "" {
		detail += ", used in " + in.Language
	}
	if in.Observations {
		if r.Observations, err = s.Observe(ctx, modelID); err != nil {
			return err
		}
		if r.Observations != nil {
			detail += ", with how it runs: " + r.Observations.String()
		}
	}
	s.record(ctx, sh.Destination, detail)
	key, err := s.Client.Submit(ctx, r)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE model_ratings SET remote_key = ?, shared_at = ? WHERE model_id = ?`, key, s.now().UTC().Format(time.RFC3339), modelID)
	return err
}

func (s *Service) withdraw(ctx context.Context, modelID, key string) error {
	client, err := s.clientID(ctx)
	if err != nil {
		return err
	}
	s.record(ctx, s.Client.ServiceHost(), "Removed a shared rating")
	if err := s.Client.Remove(ctx, key, client); err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE model_ratings SET remote_key = NULL, shared_at = NULL WHERE model_id = ?`, modelID)
	return err
}

// Delete removes a person's rating, withdrawing it first if it was shared.
func (s *Service) Delete(ctx context.Context, modelID string) error {
	r, err := s.load(ctx, modelID)
	if err != nil || r == nil {
		return err
	}
	if r.remoteKey != "" {
		if err := s.withdraw(ctx, modelID, r.remoteKey); err != nil {
			return &ShareError{Err: err}
		}
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM model_ratings WHERE model_id = ?`, modelID)
	return err
}

// Community is everyone's ratings of the models here.
type Community struct {
	// Enabled is whether community ratings are shown.
	Enabled bool `json:"enabled"`
	// FetchedAt is when the summary was downloaded; nil when there is none.
	FetchedAt   *time.Time `json:"fetched_at,omitempty"`
	GeneratedAt *time.Time `json:"generated_at,omitempty"`
	Source      string     `json:"source,omitempty"`
	// Error is why the latest download failed; an older summary may still
	// be shown.
	Error string `json:"error,omitempty"`
	// Models are by local model ID.
	Models map[string]ModelCommunity `json:"models"`
}

// ModelCommunity is one model's community ratings: from hardware like this
// computer's, from everyone who runs it the same way, and from everyone by
// the language they used it in.
type ModelCommunity struct {
	Similar   *Stats          `json:"similar,omitempty"`
	Overall   *Stats          `json:"overall,omitempty"`
	Languages []LanguageStats `json:"languages,omitempty"`
}

// Community returns everyone's ratings of the models here, downloading the
// public summary when the one kept is more than a day old.
func (s *Service) Community(ctx context.Context) (Community, error) {
	out := Community{Models: map[string]ModelCommunity{}}
	on, err := s.Settings.GetBool(ctx, SettingShow, false)
	if err != nil || !on {
		return out, err
	}
	out.Enabled = true
	snap, fetched, source, ferr := s.summary(ctx)
	if ferr != nil {
		out.Error = ferr.Error()
	}
	if fetched.IsZero() {
		return out, nil
	}
	out.FetchedAt, out.Source = &fetched, source
	if !snap.GeneratedAt.IsZero() {
		out.GeneratedAt = &snap.GeneratedAt
	}
	inv, err := s.Hardware(ctx)
	if err != nil {
		return out, err
	}
	h, hok := Normalize(inv)
	backend := Backend(inv)
	list, err := s.Models(ctx)
	if err != nil {
		return out, err
	}
	for _, m := range list {
		id, err := Identify(m, backend)
		if err != nil {
			continue
		}
		if mc, ok := lookup(snap, id, h, hok); ok {
			out.Models[m.ID] = mc
		}
	}
	return out, nil
}

// lookup finds a model's ratings in the summary: the narrowest published
// cohort of hardware like h, and everyone's.
func lookup(snap Snapshot, id Identity, h Hardware, hok bool) (ModelCommunity, bool) {
	for _, e := range snap.Models {
		if e.Model != id.ID || e.Format != id.Format || e.Quantization != id.Quantization || e.Runtime != id.Runtime || e.Backend != id.Backend {
			continue
		}
		mc := ModelCommunity{Languages: e.Languages}
		for i := range e.Cohorts {
			if e.Cohorts[i].Tier == "global" {
				st := e.Cohorts[i]
				mc.Overall = &st
			}
		}
		if hok {
		tiers:
			for _, c := range Cohorts(h, id.Backend) {
				for i := range e.Cohorts {
					if e.Cohorts[i].Tier == c.Tier && e.Cohorts[i].Cohort == c.Key {
						st := e.Cohorts[i]
						mc.Similar = &st
						break tiers
					}
				}
			}
		}
		return mc, mc.Overall != nil || mc.Similar != nil
	}
	return ModelCommunity{}, false
}

// summary returns the kept summary, downloading a new one when it is more
// than a day old. A failed download keeps the old one.
func (s *Service) summary(ctx context.Context) (Snapshot, time.Time, string, error) {
	s.fetch.Lock()
	defer s.fetch.Unlock()
	var snap Snapshot
	var body, source, at string
	err := s.DB.QueryRowContext(ctx, `SELECT body, source, fetched_at FROM ratings_summary WHERE id = 1`).Scan(&body, &source, &at)
	fetched, _ := time.Parse(time.RFC3339, at)
	if err == nil {
		_ = json.Unmarshal([]byte(body), &snap)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, time.Time{}, "", err
	}
	if !fetched.IsZero() && s.now().Sub(fetched) < summaryMaxAge {
		return snap, fetched, source, nil
	}
	s.record(ctx, s.Client.ServiceHost(), "Downloaded the public ratings summary")
	fresh, from, ferr := s.Client.Summary(ctx)
	if ferr != nil {
		return snap, fetched, source, ferr
	}
	if u, err := url.Parse(from); err == nil && u.Host != s.Client.ServiceHost() {
		s.record(ctx, u.Host, "Downloaded the public ratings summary")
	}
	b, _ := json.Marshal(fresh)
	now := s.now().UTC()
	if _, err := s.DB.ExecContext(ctx, `
		INSERT INTO ratings_summary (id, body, source, fetched_at) VALUES (1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET body = excluded.body, source = excluded.source, fetched_at = excluded.fetched_at`,
		string(b), from, now.Format(time.RFC3339)); err != nil {
		return fresh, now, from, err
	}
	return fresh, now.Truncate(time.Second), from, nil
}

func itoa(n int) string { return string(rune('0' + n)) }

// appVersion is the version, or "" for a development build the service
// would refuse.
func appVersion(v string) string {
	if !versionRe.MatchString(v) {
		return ""
	}
	return v
}
