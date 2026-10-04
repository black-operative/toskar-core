package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/config"
)

const joinUsage = `usage: yggctl join --server <host:port> --token <ygj_…> --fingerprint <sha256:…> [--name <name>] [--wait 60s] [--output json]
Joins this computer to the Yggdrasil network of the computer that made the
join command. Run the command that computer printed, as it is.
  --name   rename this computer as it joins
  --wait   wait this long for Yggdrasil to start here, for provisioning scripts`

const joinTokenUsage = `usage: yggctl join-token <create|list|revoke> [--output json]
  create [--ttl 15m]   make a one-time join token and print the command to run on the new computer
  list                 list tokens made in the last week
  revoke <id>          stop an unused token`

const networkUsage = `usage: yggctl network [status] [--output json]
Shows this computer's network, its address and fingerprint, and the computers it trusts.`

const leaveUsage = `usage: yggctl leave [--output json]
Leaves the network: tells each paired computer, then forgets them all.
Models, settings, and this computer's identity stay.`

// exitError carries an exit status.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func newDaemon() daemonClient {
	base := strings.TrimRight(config.Env("URL"), "/")
	if base == "" {
		base = "http://" + config.DefaultConfig().APIAddr()
	}
	return daemonClient{base: base, key: config.Env("API_KEY"), client: &http.Client{Timeout: 60 * time.Second}}
}

// notRunningError is a daemon that didn't answer at all.
type notRunningError struct{ base string }

func (e *notRunningError) Error() string {
	return fmt.Sprintf("Yggdrasil isn't running on this computer (%s). Start it, for example with systemctl start yggdrasil, and try again", e.base)
}

// waitRunning waits up to d for the daemon to answer, as a provisioning
// script that just installed it needs. Any answer means it is running.
func (c daemonClient) waitRunning(d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		err := c.request(http.MethodGet, "/health", nil, nil)
		var down *notRunningError
		if !errors.As(err, &down) {
			return nil
		}
		if !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// apiError is a refusal from the daemon, with its code.
type apiError struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *apiError) Error() string { return e.Message }

// request is daemonClient.call keeping the error code, and saying plainly
// when the daemon isn't running.
func (c daemonClient) request(method, path string, body, dest any) error {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+"/api/v1"+path, payload)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return &notRunningError{base: c.base}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Code    string         `json:"code"`
				Message string         `json:"message"`
				Details map[string]any `json:"details"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error.Message != "" {
			return &apiError{Code: e.Error.Code, Message: e.Error.Message, Details: e.Error.Details}
		}
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	if dest == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return json.Unmarshal(data, dest)
}

func outputFlag(fs *flag.FlagSet) *string {
	return fs.String("output", "text", "text or json")
}

func checkOutput(o string) error {
	if o != "text" && o != "json" {
		return &exitError{code: 2, msg: "--output is text or json"}
	}
	return nil
}

type joinResult struct {
	Status    string      `json:"status"`
	NetworkID string      `json:"network_id"`
	Server    networkNode `json:"server"`
	Node      networkNode `json:"node"`
}

func joinCommand(args []string, c daemonClient, out io.Writer) error {
	fs := flag.NewFlagSet("join", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	server := fs.String("server", "", "")
	token := fs.String("token", "", "")
	fingerprint := fs.String("fingerprint", "", "")
	name := fs.String("name", "", "")
	wait := fs.Duration("wait", 0, "")
	output := outputFlag(fs)
	if err := fs.Parse(args); err != nil || *server == "" || *token == "" || *fingerprint == "" || fs.NArg() > 0 || *wait < 0 {
		return &exitError{code: 2, msg: joinUsage}
	}
	if err := checkOutput(*output); err != nil {
		return err
	}
	if err := c.waitRunning(*wait); err != nil {
		return &exitError{code: 1, msg: err.Error()}
	}
	body := map[string]string{"server": *server, "token": *token, "fingerprint": *fingerprint}
	if *name != "" {
		body["name"] = *name
	}
	var res joinResult
	err := c.request(http.MethodPost, "/network/join", body, &res)
	if err != nil {
		return joinFailure(err)
	}
	if *output == "json" {
		return writeJSON(out, res)
	}
	if res.Status == "already_joined" {
		fmt.Fprintf(out, "This computer is already joined to %s's network.\n  network: %s\n  node:    %s (%s)\n", res.Server.Name, res.NetworkID, res.Node.Name, res.Node.ID)
		return nil
	}
	fmt.Fprintf(out, "✓ Connected to %s (%s)\n✓ Joined the Yggdrasil network\n\nThis computer:\n  name:    %s\n  id:      %s\n  network: %s\n",
		res.Server.Name, res.Server.Address, res.Node.Name, res.Node.ID, res.NetworkID)
	return nil
}

// joinFailure adds what to do next to a failed join.
func joinFailure(err error) error {
	var e *apiError
	if !errors.As(err, &e) {
		return err
	}
	msg := "Join failed: " + e.Message
	switch e.Code {
	case "JOIN_WRONG_SERVER":
		msg = "Join stopped: " + e.Message
	case "JOIN_UNREACHABLE":
		msg += "\n\nCheck:\n- the server address\n- that its firewall allows TCP port 7332\n- that Yggdrasil is running there"
	case "JOIN_TOKEN_EXPIRED", "JOIN_TOKEN_USED", "JOIN_TOKEN_REVOKED", "JOIN_TOKEN_INVALID":
		msg += "\n\nMake a new join command on a computer in the network: yggctl join-token create"
	}
	return &exitError{code: 1, msg: msg}
}

type joinToken struct {
	ID          string     `json:"id"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	UsedBy      string     `json:"used_by,omitempty"`
	UsedAt      *time.Time `json:"used_at,omitempty"`
	Status      string     `json:"status"`
	Token       string     `json:"token,omitempty"`
	Server      string     `json:"server,omitempty"`
	Fingerprint string     `json:"fingerprint,omitempty"`
	Command     string     `json:"command,omitempty"`
	Install     string     `json:"install_command,omitempty"`
	Windows     string     `json:"windows_command,omitempty"`
}

func joinTokenCommand(args []string, c daemonClient, out io.Writer, now func() time.Time) error {
	if len(args) == 0 {
		return &exitError{code: 2, msg: joinTokenUsage}
	}
	fs := flag.NewFlagSet("join-token", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	output := outputFlag(fs)
	ttl := fs.Duration("ttl", 15*time.Minute, "")
	if err := fs.Parse(args[1:]); err != nil {
		return &exitError{code: 2, msg: joinTokenUsage}
	}
	if err := checkOutput(*output); err != nil {
		return err
	}
	switch args[0] {
	case "create":
		if fs.NArg() > 0 || *ttl < time.Minute || *ttl > 24*time.Hour {
			return &exitError{code: 2, msg: "--ttl is between 1m and 24h\n" + joinTokenUsage}
		}
		var t joinToken
		if err := c.request(http.MethodPost, "/join-tokens", map[string]int{"ttl_minutes": int(ttl.Minutes())}, &t); err != nil {
			return &exitError{code: 1, msg: err.Error()}
		}
		if *output == "json" {
			return writeJSON(out, t)
		}
		fmt.Fprintf(out, "Join a computer to this Yggdrasil network. On that computer, run:\n\n  %s\n", t.Command)
		if t.Install != "" {
			fmt.Fprintf(out, "\nIf Yggdrasil isn't installed there yet, this installs it and joins (Linux, macOS):\n\n  %s\n", t.Install)
		}
		if t.Windows != "" {
			fmt.Fprintf(out, "\nOn Windows, in PowerShell:\n\n  %s\n", t.Windows)
		}
		fmt.Fprintf(out, "\nThis token expires in %s and can be used once (ID %s).\n", roundDuration(t.ExpiresAt.Sub(now())), t.ID)
		return nil
	case "list":
		var list []joinToken
		if err := c.request(http.MethodGet, "/join-tokens", nil, &list); err != nil {
			return &exitError{code: 1, msg: err.Error()}
		}
		if *output == "json" {
			return writeJSON(out, list)
		}
		if len(list) == 0 {
			fmt.Fprintln(out, "No join tokens in the last week. Make one with: yggctl join-token create")
			return nil
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tCREATED\tEXPIRES\tSTATUS")
		for _, t := range list {
			expires := "—"
			if t.Status == "active" {
				expires = "in " + roundDuration(t.ExpiresAt.Sub(now()))
			}
			status := t.Status
			if t.UsedBy != "" {
				status += " by " + t.UsedBy
			}
			fmt.Fprintf(tw, "%s\t%s ago\t%s\t%s\n", t.ID, roundDuration(now().Sub(t.CreatedAt)), expires, status)
		}
		return tw.Flush()
	case "revoke":
		if fs.NArg() != 1 {
			return &exitError{code: 2, msg: joinTokenUsage}
		}
		var t joinToken
		if err := c.request(http.MethodDelete, "/join-tokens/"+fs.Arg(0), nil, &t); err != nil {
			return &exitError{code: 1, msg: err.Error()}
		}
		if *output == "json" {
			return writeJSON(out, t)
		}
		fmt.Fprintf(out, "Revoked join token %s.\n", t.ID)
		return nil
	}
	return &exitError{code: 2, msg: joinTokenUsage}
}

// roundDuration is a duration as a person says it: 14m, 3h, 2d.
func roundDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", max(0, int(d.Seconds())))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()+0.5))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()+0.5))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24+0.5))
}

type networkNode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Address     string `json:"address,omitempty"`
	Status      string `json:"status,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

func networkCommand(args []string, c daemonClient, out io.Writer) error {
	if len(args) > 0 && args[0] == "status" {
		args = args[1:]
	}
	fs := flag.NewFlagSet("network", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	output := outputFlag(fs)
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return &exitError{code: 2, msg: networkUsage}
	}
	if err := checkOutput(*output); err != nil {
		return err
	}
	var st struct {
		NetworkID string        `json:"network_id"`
		Reachable bool          `json:"reachable"`
		Node      networkNode   `json:"node"`
		Peers     []networkNode `json:"peers"`
	}
	if err := c.request(http.MethodGet, "/network", nil, &st); err != nil {
		return &exitError{code: 1, msg: err.Error()}
	}
	if *output == "json" {
		return writeJSON(out, st)
	}
	network := st.NetworkID
	if network == "" {
		network = "none yet"
	}
	fmt.Fprintf(out, "This computer:\n  name:        %s\n  id:          %s\n  address:     %s\n  fingerprint: %s\n  network:     %s\n",
		st.Node.Name, st.Node.ID, st.Node.Address, st.Node.Fingerprint, network)
	if !st.Reachable {
		fmt.Fprintln(out, "\nOther computers can't reach this one: turn on discovery_enabled and restart Yggdrasil.")
	}
	if len(st.Peers) == 0 {
		fmt.Fprintln(out, "\nNo paired computers. Add one with: yggctl join-token create")
		return nil
	}
	fmt.Fprintln(out, "\nPaired computers:")
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, p := range st.Peers {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", p.Name, p.Address, p.Status)
	}
	return tw.Flush()
}

func leaveCommand(args []string, c daemonClient, out io.Writer) error {
	fs := flag.NewFlagSet("leave", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	output := outputFlag(fs)
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return &exitError{code: 2, msg: leaveUsage}
	}
	if err := checkOutput(*output); err != nil {
		return err
	}
	var res struct {
		Left        []string `json:"left"`
		Unreachable []string `json:"unreachable"`
	}
	if err := c.request(http.MethodPost, "/network/leave", nil, &res); err != nil {
		return &exitError{code: 1, msg: err.Error()}
	}
	if *output == "json" {
		return writeJSON(out, res)
	}
	if len(res.Left)+len(res.Unreachable) == 0 {
		fmt.Fprintln(out, "This computer wasn't paired with any computers. It is ready to join a network.")
		return nil
	}
	fmt.Fprintln(out, "Left the network. Models and settings stay on this computer.")
	if len(res.Left) > 0 {
		fmt.Fprintf(out, "Told: %s\n", strings.Join(res.Left, ", "))
	}
	if len(res.Unreachable) > 0 {
		fmt.Fprintf(out, "Couldn't reach %s; remove this computer there from the Computers page.\n", strings.Join(res.Unreachable, ", "))
	}
	return nil
}
