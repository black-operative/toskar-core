package auth

import (
	"net"
	"net/http"
	"net/netip"
)

// FromThisComputer reports a request that came over loopback, from an app
// on the same computer. Such a request needs no API key even when the API
// is open to the network, so turning on network access never breaks the
// desktop app, toskarctl, or a browser on this computer.
//
// A request carrying forwarding headers came through a proxy, such as one
// on this computer that serves the network over TLS; it is treated as
// coming from the network, since the proxy's own loopback address says
// nothing about the caller.
func FromThisComputer(r *http.Request) bool {
	for _, h := range []string{"Forwarded", "X-Forwarded-For", "X-Real-IP"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Unmap().IsLoopback()
}
