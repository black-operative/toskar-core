package app

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestIsConnectivityErr(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("model not found"), false},
		{fmt.Errorf("dial tcp 10.0.0.2:7332: connect: connection refused"), true},
		{fmt.Errorf("Get \"http://x\": i/o timeout"), true},
		{&net.OpError{Op: "dial", Err: errors.New("refused")}, true},
	}
	for _, tc := range cases {
		if got := isConnectivityErr(tc.err); got != tc.want {
			t.Fatalf("isConnectivityErr(%v)=%v want %v", tc.err, got, tc.want)
		}
	}
}

func TestRemoteUnreachableErr(t *testing.T) {
	n := contracts.Node{ID: "n1", Name: "Laptop"}
	err := remoteUnreachableErr(n, fmt.Errorf("dial tcp: connection refused"))
	if err == nil || !strings.Contains(err.Error(), "Laptop") || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("got %v", err)
	}
	passthrough := remoteUnreachableErr(n, fmt.Errorf("model missing"))
	if passthrough.Error() != "model missing" {
		t.Fatalf("non-connectivity should pass through: %v", passthrough)
	}
}
