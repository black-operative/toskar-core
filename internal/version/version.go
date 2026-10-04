package version

import (
	"fmt"
	"strings"
)

const (
	// ProductName is the name used in the AGPL source offer.
	ProductName = "Toskar Core"
	// LicenseID is the SPDX identifier for this program.
	LicenseID = "AGPL-3.0-or-later"
	// Repository is the upstream source repository.
	Repository = "https://github.com/yeixio/yggdrasil-core"
)

// Build-time variables set via -ldflags.
var (
	Version   = "0.1.0-dev"
	Commit    = "unknown"
	BuildDate = "unknown"
	// SourceURL overrides the corresponding-source link. Forks that run a
	// modified daemon on a network set this to the URL of their source.
	SourceURL = ""
)

// Offer is the AGPL corresponding-source notice for this build.
type Offer struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	License string `json:"license"`
	Source  string `json:"source"`
}

// CurrentOffer returns the source offer for the running build.
func CurrentOffer() Offer {
	return Offer{
		Name:    ProductName,
		Version: Version,
		Commit:  Commit,
		License: LicenseID,
		Source:  CorrespondingSource(),
	}
}

// CorrespondingSource is the URL for the source of this exact build.
// A release version points at that tag. A development build with a known
// commit points at that commit. An explicit SourceURL replaces both.
func CorrespondingSource() string {
	if u := strings.TrimSpace(SourceURL); u != "" {
		return u
	}
	if isReleaseVersion(Version) {
		return Repository + "/tree/" + releaseTag(Version)
	}
	if commitKnown(Commit) {
		return Repository + "/tree/" + Commit
	}
	return Repository
}

// Text is the human-readable source offer printed by the CLI.
func (o Offer) Text() string {
	return fmt.Sprintf("Toskar Core %s\nLicensed under %s\nCorresponding source:\n%s\nCommit: %s\n", o.Version, o.License, o.Source, o.Commit)
}

// Info returns version metadata for API and UI surfaces.
func Info() map[string]string {
	offer := CurrentOffer()
	return map[string]string{
		"version":    Version,
		"commit":     Commit,
		"build_date": BuildDate,
		"product":    "Yggdrasil",
		"license":    offer.License,
		"source":     offer.Source,
	}
}

func isReleaseVersion(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || v == "unknown" {
		return false
	}
	return !strings.Contains(strings.ToLower(v), "dev")
}

func releaseTag(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

func commitKnown(c string) bool {
	c = strings.TrimSpace(c)
	if len(c) < 7 || strings.EqualFold(c, "unknown") {
		return false
	}
	for _, r := range c {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
