package contracts

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// ContractVersion is the version of the client contract (spec §68): the
// events, run traces, citations, files, and steps the desktop app, mobile
// apps, and other clients read. It is major.minor.
//
//   - Minor rises when fields or event types are added. Clients ignore what
//     they do not know, so an older client keeps working.
//   - Major rises only when something is removed or changes meaning. A
//     client built for another major version is told to update.
//
// tests/contract checks that no field in the contract is removed or renamed.
const ContractVersion = "1.6"

// Headers that carry the contract version. Each has a name from before the
// Toskar rename that is still sent and accepted, so clients and computers
// on older versions keep working (#237).
const (
	// ContractHeader is on every API response, and so is
	// LegacyContractHeader.
	ContractHeader       = "Toskar-Contract"
	LegacyContractHeader = "Yggdrasil-Contract"
	// ClientContractHeader is what a client may send: the contract it was
	// built for. LegacyClientContractHeader is accepted too.
	ClientContractHeader       = "Toskar-Client-Contract"
	LegacyClientContractHeader = "Yggdrasil-Client-Contract"
)

// ClientContract is the contract a request says its client was built for,
// from either header (the new name wins), and the header it came in.
func ClientContract(h http.Header) (version, header string) {
	if v := h.Get(ClientContractHeader); v != "" {
		return v, ClientContractHeader
	}
	return h.Get(LegacyClientContractHeader), LegacyClientContractHeader
}

// ContractInfo describes the contract in /api/v1/version.
type ContractInfo struct {
	Version string `json:"version"`
	// Major is the major version clients must match.
	Major int `json:"major"`
}

// CurrentContract describes the contract this build speaks.
func CurrentContract() ContractInfo {
	major, _ := ContractMajor(ContractVersion)
	return ContractInfo{Version: ContractVersion, Major: major}
}

// ContractMajor reads the major version of "1.4" or "1".
func ContractMajor(v string) (int, bool) {
	head, _, _ := strings.Cut(strings.TrimSpace(v), ".")
	n, err := strconv.Atoi(head)
	return n, err == nil && n > 0
}

// CheckClientContract reports whether a client built for version v can talk
// to this build. Any minor version of the same major works; an empty
// version means the client did not say, which is allowed.
func CheckClientContract(v string) error {
	return CheckClientContractHeader(v, ClientContractHeader)
}

// CheckClientContractHeader is CheckClientContract for a version read from
// header, which a malformed version's message names.
func CheckClientContractHeader(v, header string) error {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	client, ok := ContractMajor(v)
	if !ok {
		return fmt.Errorf("%s must look like 1.0", header)
	}
	server, _ := ContractMajor(ContractVersion)
	switch {
	case client > server:
		return fmt.Errorf("this app needs Toskar contract %d.x, and this Toskar speaks %s. Update Toskar", client, ContractVersion)
	case client < server:
		return fmt.Errorf("this app was built for Toskar contract %d.x, and this Toskar speaks %s. Update the app", client, ContractVersion)
	}
	return nil
}
