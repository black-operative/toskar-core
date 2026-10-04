package remotetools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Enter admits work from another computer, waiting for this computer's own
// chats first; done ends it. A nil Enter admits everything.
type Enter func(ctx context.Context, label string) (done func(), err error)

type providersResponse struct {
	Protocol  int        `json:"protocol"`
	Providers []Provider `json:"providers"`
}

type runRequest struct {
	Tool string `json:"tool"`
	Job  Job    `json:"job"`
}

type errorResponse struct {
	Error string `json:"error"`
	// NotReady means the tool cannot run here now; the caller may try
	// another computer.
	NotReady bool `json:"not_ready,omitempty"`
}

// Handler serves this computer's portable tools to paired computers, at
// /tools/providers and /tools/run. The internal server authenticates the
// peer before it gets here.
func Handler(local []Portable, enter Enter) http.Handler {
	byID := map[string]Portable{}
	for _, p := range local {
		byID[p.ID()] = p
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tools/providers", func(w http.ResponseWriter, r *http.Request) {
		out := providersResponse{Protocol: Protocol, Providers: []Provider{}}
		for _, p := range local {
			out.Providers = append(out.Providers, p.Provider())
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("POST /tools/run", func(w http.ResponseWriter, r *http.Request) {
		var req runRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, MaxBody)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "the job could not be read: " + err.Error()})
			return
		}
		p, ok := byID[req.Tool]
		if !ok {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: req.Tool + " cannot run on this computer", NotReady: true})
			return
		}
		if ready, why := p.Available(); !ready {
			writeJSON(w, http.StatusConflict, errorResponse{Error: why, NotReady: true})
			return
		}
		if enter != nil {
			done, err := enter(r.Context(), "Running "+req.Tool+" for another computer")
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: err.Error(), NotReady: true})
				return
			}
			defer done()
		}
		out, err := p.Run(r.Context(), req.Job)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Doer sends an authenticated request to a path on a paired computer's
// internal API, such as /internal/v1/tools/run. *nodes.Client implements it.
type Doer interface {
	Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error)
}

// internalPrefix is where the internal server mounts Handler.
const internalPrefix = "/internal/v1"

// ErrOldPeer means the computer runs a Yggdrasil without remote tools.
var ErrOldPeer = contracts.NewError("PEER_TOO_OLD", nil, errors.New("this computer needs a newer Toskar to run tools for others"))

// RemoteError is the tool's own error from the other computer, such as a
// prompt that is too long. NotReady errors may be retried elsewhere.
type RemoteError struct {
	Message  string
	NotReady bool
}

func (e *RemoteError) Error() string { return e.Message }

// FetchProviders asks a paired computer which tools it can run.
func FetchProviders(ctx context.Context, peer Doer) ([]Provider, error) {
	resp, err := peer.Do(ctx, http.MethodGet, internalPrefix+"/tools/providers", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrOldPeer
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var out providersResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, err
	}
	if out.Protocol != Protocol {
		return nil, ErrOldPeer
	}
	return out.Providers, nil
}

// RunOn runs a job on a paired computer. Cancelling ctx stops it there: the
// connection closes and the other computer stops the work.
func RunOn(ctx context.Context, peer Doer, tool string, job Job) (Output, error) {
	raw, err := json.Marshal(runRequest{Tool: tool, Job: job})
	if err != nil {
		return Output{}, err
	}
	if len(raw) > MaxBody {
		return Output{}, fmt.Errorf("the files are too large to send to another computer")
	}
	resp, err := peer.Do(ctx, http.MethodPost, internalPrefix+"/tools/run", bytes.NewReader(raw))
	if err != nil {
		return Output{}, err
	}
	defer resp.Body.Close()
	body := io.LimitReader(resp.Body, MaxBody)
	if resp.StatusCode != http.StatusOK {
		var e errorResponse
		if json.NewDecoder(body).Decode(&e) != nil || e.Error == "" {
			if resp.StatusCode == http.StatusNotFound {
				return Output{}, &RemoteError{Message: ErrOldPeer.Error(), NotReady: true}
			}
			return Output{}, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return Output{}, &RemoteError{Message: e.Error, NotReady: e.NotReady}
	}
	var out Output
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		return Output{}, fmt.Errorf("the result could not be read: %w", err)
	}
	return out, nil
}
