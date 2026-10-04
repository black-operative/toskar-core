package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/locale"
	modelhealth "github.com/yeixio/toskar-core/internal/models/health"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/ratings"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// errNoModel is returned when Auto has nothing to choose from.
var errNoModel = errors.New("no model is installed yet. Install one on the Models page")

// memoryTotal is this computer's memory in bytes, detected once. It is 0 when
// it cannot be read, which lets every model through.
func (a *App) memoryTotal(ctx context.Context) uint64 {
	if v := a.memTotal.Load(); v > 0 {
		return v
	}
	if a.hw == nil {
		return 0
	}
	inv, err := a.hw.Detect(ctx)
	if err != nil {
		return 0
	}
	a.memTotal.Store(inv.Memory.TotalBytes)
	return inv.Memory.TotalBytes
}

func (a *App) installedModels(ctx context.Context) []contracts.Model {
	if a.Models == nil {
		return nil
	}
	list, err := a.Models.List(ctx)
	if err != nil {
		return nil
	}
	return list
}

// failedFor is how long Auto avoids a model that could not answer.
const failedFor = 10 * time.Minute

// noteModelFailed records that a model could not answer, so Auto passes it
// over for a while instead of failing and falling back on every turn.
func (a *App) noteModelFailed(modelID string) {
	a.failedModels.Store(modelID, time.Now())
}

func (a *App) recentlyFailed(modelID string) bool {
	v, ok := a.failedModels.Load(modelID)
	if !ok {
		return false
	}
	if time.Since(v.(time.Time)) > failedFor {
		a.failedModels.Delete(modelID)
		return false
	}
	return true
}

// chooseAuto picks the model for a chat set to Auto (spec §12–13). lang is
// the language the answer is written in (multilingual spec §15–16), or ""
// to leave it out.
func (a *App) chooseAuto(ctx context.Context, message string, data bool, lang string) (huginn.Choice, error) {
	all := a.installedModels(ctx)
	usable := make([]contracts.Model, 0, len(all))
	for _, m := range all {
		if !a.recentlyFailed(m.ID) {
			usable = append(usable, m)
		}
	}
	if len(usable) == 0 {
		usable = all
	}
	usable = a.withLanguageRatings(ctx, usable, lang)
	kind := huginn.Classify(message)
	if data && kind == huginn.Chat {
		kind = huginn.Research
	}
	choice, ok := huginn.ChooseWith(kind, huginn.EffortFrom(ctx), lang, usable, a.memoryTotal(ctx), a.autoSignals(ctx))
	if !ok {
		return huginn.Choice{}, errNoModel
	}
	return choice, nil
}

// autoSignals are what Auto weighs beyond the request (spec §13): the
// community's ratings from hardware like this computer's, from the summary
// already kept, and the models busy answering something now.
func (a *App) autoSignals(ctx context.Context) huginn.Signals {
	var sig huginn.Signals
	if rated := a.communitySignals(ctx); len(rated) > 0 {
		sig.Rating = make(map[string]float64, len(rated))
		for id, s := range rated {
			sig.Rating[id] = s.Signal
		}
	}
	if a.Health != nil {
		sig.Busy = a.Health.Generating()
	}
	return sig
}

// otherQuantization is an installed version of the failed model at another
// quantization that fits this computer: smaller when it ran out of memory,
// and otherwise the largest that fits. It also returns both quantizations,
// for the step that explains it.
func otherQuantization(failedID string, installed []contracts.Model, memTotal uint64, oom bool) (contracts.Model, string, string, bool) {
	var failed contracts.Model
	for _, m := range installed {
		if m.ID == failedID {
			failed = m
		}
	}
	base, err := ratings.Identify(failed, "")
	if err != nil {
		return contracts.Model{}, "", "", false
	}
	var best contracts.Model
	bestQuant := ""
	for _, m := range installed {
		if m.ID == failedID || !m.Installed || huginn.Supporting(m) {
			continue
		}
		id, err := ratings.Identify(m, "")
		if err != nil || id.ID != base.ID || id.Format != base.Format || id.Quantization == base.Quantization {
			continue
		}
		if oom && failed.MemoryNeeded > 0 && (m.MemoryNeeded == 0 || m.MemoryNeeded >= failed.MemoryNeeded) {
			continue
		}
		if memTotal > 0 && m.MemoryNeeded > 0 && float64(m.MemoryNeeded) > float64(memTotal)*quantFallbackShare {
			continue
		}
		if bestQuant == "" || m.MemoryNeeded > best.MemoryNeeded {
			best, bestQuant = m, id.Quantization
		}
	}
	return best, base.Quantization, bestQuant, bestQuant != ""
}

// withoutLargerVersions leaves out the other quantizations of the failed
// model that need at least as much memory.
func withoutLargerVersions(failedID string, installed []contracts.Model) []contracts.Model {
	var failed contracts.Model
	for _, m := range installed {
		if m.ID == failedID {
			failed = m
		}
	}
	base, err := ratings.Identify(failed, "")
	if err != nil {
		return installed
	}
	out := make([]contracts.Model, 0, len(installed))
	for _, m := range installed {
		if id, err := ratings.Identify(m, ""); err == nil && m.ID != failedID && id.ID == base.ID && m.MemoryNeeded >= failed.MemoryNeeded {
			continue
		}
		out = append(out, m)
	}
	return out
}

// quantFallbackShare is how much of this computer's memory another
// quantization may need, as for any model Auto picks.
const quantFallbackShare = 0.60

// recoverable reports whether a failed turn may be retried on another model:
// nothing was shown or changed yet, and the user did not stop it.
func recoverable(ctx context.Context, errText, shown string, env *chatExecEnv) bool {
	if ctx.Err() != nil || shown != "" || env.trace.hasSideEffects() {
		return false
	}
	lower := strings.ToLower(errText)
	return !strings.Contains(lower, "context canceled") && !strings.Contains(lower, "cancelled")
}

// fallback picks the model to retry with and the words that explain it
// (spec §14, §26). The note is empty when the answer should be as good.
// preferred is the profile's fallback order, tried before Yggdrasil's pick.
// lang is the App language the note is written in.
func (a *App) fallback(ctx context.Context, lang, failedID, errText string, preferred []string) (model contracts.Model, step, notice string, ok bool) {
	return fallbackFrom(lang, failedID, errText, a.installedModels(ctx), a.memoryTotal(ctx), preferred...)
}

func fallbackFrom(lang, failedID, errText string, installed []contracts.Model, memTotal uint64, preferred ...string) (model contracts.Model, step, notice string, ok bool) {
	health, isHealth := modelhealth.Parse(errText)
	oom := isHealth && health.LikelyMemoryPressure
	// Another quantization of the same model comes before another model
	// (spec §14).
	if next, failedQuant, quant, ok := otherQuantization(failedID, installed, memTotal, oom); ok {
		why := "chat:steps.fallbackQuantFailed"
		switch {
		case oom:
			why = "chat:steps.fallbackQuantOutOfMemory"
		case isHealth:
			why = "chat:steps.fallbackQuantStopped"
		}
		params := map[string]any{"model": huginn.Name(next), "failedQuant": failedQuant, "quant": quant}
		step = locale.T(lang, why, params)
		for _, m := range installed {
			if m.ID == failedID && next.MemoryNeeded < m.MemoryNeeded {
				notice = locale.T(lang, "chat:notices.fallbackQuantization", params)
			}
		}
		return next, step, notice, true
	}
	if oom {
		// A larger version of the model that ran out of memory won't do
		// better, whatever else is installed.
		installed = withoutLargerVersions(failedID, installed)
	}
	next, ok := preferredFallback(failedID, installed, preferred)
	if !ok {
		next, ok = huginn.Fallback(failedID, installed, memTotal)
	}
	if !ok {
		return contracts.Model{}, "", "", false
	}
	failed := contracts.Model{ID: failedID}
	for _, m := range installed {
		if m.ID == failedID {
			failed = m
		}
	}
	why := "chat:steps.fallbackFailed"
	if f, isHealth := modelhealth.Parse(errText); isHealth {
		why = "chat:steps.fallbackStopped"
		if f.LikelyMemoryPressure {
			why = "chat:steps.fallbackOutOfMemory"
		}
	}
	step = locale.T(lang, why, map[string]any{"failed": huginn.Name(failed), "model": huginn.Name(next)})
	if huginn.Smaller(failed, next) {
		notice = locale.T(lang, "chat:notices.fallbackSmaller", map[string]any{"failed": huginn.Name(failed), "model": huginn.Name(next)})
	}
	return next, step, notice, true
}

// preferredFallback is the first model in a profile's fallback order that is
// installed, can answer chats, and is not the one that failed (§20).
func preferredFallback(failedID string, installed []contracts.Model, preferred []string) (contracts.Model, bool) {
	for _, id := range preferred {
		if id == failedID {
			continue
		}
		for _, m := range installed {
			if m.ID == id && (m.Installed || m.Status == "installed") && !huginn.Supporting(m) {
				return m, true
			}
		}
	}
	return contracts.Model{}, false
}

// profileRoleModel is the profile's own model for this kind of request when
// the chat is on Auto (§20): its coding model for coding, and its fast model
// for a quick question. reason is in the App language lang. ok is false
// when the profile has none installed.
func (a *App) profileRoleModel(ctx context.Context, lang string, profile profiles.Profile, message string, data bool) (id, reason string, ok bool) {
	kind := huginn.Classify(message)
	if data && kind == huginn.Chat {
		kind = huginn.Research
	}
	role := ""
	switch kind {
	case huginn.Coding:
		role = profiles.RoleCoding
	case huginn.Chat:
		role = profiles.RoleFast
	default:
		return "", "", false
	}
	id = profiles.RoleModel(profile, role)
	if id == "" || a.recentlyFailed(id) {
		return "", "", false
	}
	for _, m := range a.installedModels(ctx) {
		if m.ID == id && (m.Installed || m.Status == "installed") && !huginn.Supporting(m) {
			key := "chat:steps.profileCoding"
			if role == profiles.RoleFast {
				key = "chat:steps.profileFast"
			}
			return id, locale.T(lang, key, map[string]any{"model": huginn.Name(m), "profile": profile.Name}), true
		}
	}
	return "", "", false
}

// turnHasData reports whether a turn will answer from the user's own data:
// files attached now or earlier in the chat, or connected knowledge.
func (a *App) turnHasData(ctx context.Context, conversationID string, profile profiles.Profile) bool {
	if len(artifacts.AttachmentsFrom(ctx)) > 0 || len(profile.KnowledgeSources) > 0 {
		return true
	}
	if a.Artifacts != nil && conversationID != "" {
		if list, err := a.Artifacts.List(ctx, conversationID); err == nil && len(list) > 0 {
			return true
		}
	}
	return false
}

// smallModelNotice warns that a small model answered from the user's files
// or knowledge, where it is most likely to mix up details, and suggests a
// larger model, in the App language lang. dataKind is "file", "knowledge",
// or "" when no data was used.
func (a *App) smallModelNotice(ctx context.Context, lang, dataKind, modelID string, auto bool) string {
	if dataKind == "" || modelID == "" {
		return ""
	}
	return smallModelNote(lang, dataKind, modelID, a.installedModels(ctx), a.memoryTotal(ctx), auto)
}

func smallModelNote(lang, dataKind, modelID string, installed []contracts.Model, memTotal uint64, auto bool) string {
	if dataKind == "" || modelID == "" {
		return ""
	}
	var answered contracts.Model
	for _, m := range installed {
		if m.ID == modelID {
			answered = m
		}
	}
	if !huginn.Small(answered) {
		return ""
	}
	// The sentences are whole keys, not joined here: languages put them
	// together differently.
	key := "smallModelFiles"
	if dataKind == "knowledge" {
		key = "smallModelKnowledge"
	}
	params := map[string]any{"model": huginn.Name(answered)}
	if larger, ok := huginn.Larger(installed, memTotal); ok && !auto {
		key += "Choose"
		params["larger"] = huginn.Name(larger)
	} else if !ok {
		key += "Install"
	}
	return locale.T(lang, "chat:notices."+key, params)
}

// languageWeakNotice says, in the App language, that the model may write
// lang less well (multilingual spec §16).
func (a *App) languageWeakNotice(ctx context.Context, m contracts.Model, lang string) string {
	app := a.appLanguage(ctx)
	return locale.T(app, "chat:notices.languageWeak", map[string]any{"model": huginn.Name(m), "language": locale.LanguageName(lang, app)})
}

// appLanguage is the App language (the ui_locale setting), which the
// notices shown with an answer are written in.
func (a *App) appLanguage(ctx context.Context) string {
	app := ""
	if a.Settings != nil {
		app, _ = a.Settings.GetString(ctx, "ui_locale", "")
	}
	return locale.Resolve(app)
}
