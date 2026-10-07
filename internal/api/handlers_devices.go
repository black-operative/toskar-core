package api

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/auth"
)

// Connect a device (#216). The computer's own UI makes a 6-digit code; the
// phone sends it from the local network, without a key, and gets one.

func (s *Server) deviceRoutes(api *mux.Router) {
	api.HandleFunc("/devices/pairing", s.handleStartDevicePairing).Methods(http.MethodPost)
	api.HandleFunc("/devices/pairing", s.handleDevicePairingStatus).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/devices/pairing", s.handleCancelDevicePairing).Methods(http.MethodDelete)
	api.HandleFunc(pairDeviceRoute, s.handlePairDevice).Methods(http.MethodPost)
}

// pairDeviceRoute is the one control API route that needs no key.
const pairDeviceRoute = "/devices/pair"

// remoteIP is the address a request came from, without its port.
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// pairingView is a code being shown, with where a phone reaches this
// computer.
type pairingView struct {
	auth.DevicePairing
	// Address is this computer's address on the local network, such as
	// 192.168.1.20:7331.
	Address string `json:"address,omitempty"`
	// Reachable is false while the API answers only on this computer: a
	// phone can't connect until local network access is on.
	Reachable bool `json:"reachable"`
}

func (s *Server) pairingView(p auth.DevicePairing) pairingView {
	v := pairingView{DevicePairing: p}
	if s.deps.PhoneAddress != nil {
		v.Address, v.Reachable = s.deps.PhoneAddress()
	}
	return v
}

// handleStartDevicePairing shows a new code, replacing any other. With
// enable_lan it first turns on local network access, which a phone needs.
func (s *Server) handleStartDevicePairing(w http.ResponseWriter, r *http.Request) {
	if s.deps.Devices == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Connecting a device isn't available.", nil)
		return
	}
	var body struct {
		EnableLAN bool `json:"enable_lan"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body)
	if body.EnableLAN && s.deps.EnableLANForPhone != nil {
		if err := s.deps.EnableLANForPhone(r.Context()); err != nil {
			writeErrFrom(w, http.StatusInternalServerError, "LAN_ACCESS_FAILED", err)
			return
		}
	}
	p, err := s.deps.Devices.Start()
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "PAIRING_FAILED", err)
		return
	}
	writeJSON(w, http.StatusCreated, s.pairingView(p))
}

// handleDevicePairingStatus says whether a phone has connected with the code.
func (s *Server) handleDevicePairingStatus(w http.ResponseWriter, r *http.Request) {
	if s.deps.Devices == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Connecting a device isn't available.", nil)
		return
	}
	writeJSON(w, http.StatusOK, s.pairingView(s.deps.Devices.Status()))
}

// handleCancelDevicePairing stops showing the code.
func (s *Server) handleCancelDevicePairing(w http.ResponseWriter, r *http.Request) {
	if s.deps.Devices != nil {
		s.deps.Devices.Cancel()
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePairDevice exchanges a code for the phone's own key. It needs no
// key, so it answers only on the local network.
func (s *Server) handlePairDevice(w http.ResponseWriter, r *http.Request) {
	if s.deps.Devices == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Connecting a device isn't available.", nil)
		return
	}
	if !auth.FromLocalNetwork(r) {
		writeErrFrom(w, http.StatusForbidden, "PAIRING_NOT_LOCAL", auth.ErrNotLocalNetwork)
		return
	}
	var body struct {
		Code       string `json:"code"`
		DeviceName string `json:"device_name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	rec, secret, err := s.deps.Devices.Pair(r.Context(), body.Code, body.DeviceName, remoteIP(r))
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, auth.ErrDeviceThrottled):
			status = http.StatusTooManyRequests
		case errors.Is(err, auth.ErrWrongDeviceCode):
			status = http.StatusUnauthorized
		case errors.Is(err, auth.ErrNoDeviceCode), errors.Is(err, auth.ErrDeviceCodeExpired):
			status = http.StatusGone
		}
		writeErrFrom(w, status, "PAIRING_FAILED", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"api_key": secret, "key": rec})
}
