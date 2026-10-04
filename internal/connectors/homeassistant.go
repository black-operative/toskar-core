package connectors

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/yeixio/yggdrasil-core/internal/tools"
)

// HomeAssistant reads device states and calls services, such as turning a
// light on.
type HomeAssistant struct{}

func (HomeAssistant) ID() string   { return "homeassistant" }
func (HomeAssistant) Name() string { return "Home Assistant" }
func (HomeAssistant) Description() string {
	return "Check lights, sensors, and other devices, and control them."
}
func (HomeAssistant) Scopes() string {
	return "Home Assistant tokens act as the user who made them. Make a separate user for Toskar, " +
		"give it only the areas and devices it needs, and create a long-lived access token on that user's profile page."
}

func (HomeAssistant) Fields() []Field {
	return []Field{
		{Key: "url", Label: "Home Assistant address", Placeholder: "http://homeassistant.local:8123"},
		{Key: "token", Label: "Long-lived access token", Secret: true},
	}
}

func haBase(cred Credential) (string, error) {
	u, err := url.Parse(strings.TrimRight(cred["url"], "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("the address must start with http:// or https://")
	}
	return u.String(), nil
}

func haHeader(cred Credential) map[string]string {
	return map[string]string{"Authorization": "Bearer " + cred["token"]}
}

func (HomeAssistant) Check(ctx context.Context, c *http.Client, cred Credential) (string, error) {
	base, err := haBase(cred)
	if err != nil {
		return "", err
	}
	var cfg struct {
		LocationName string `json:"location_name"`
	}
	if err := call(ctx, c, http.MethodGet, base+"/api/config", haHeader(cred), nil, &cfg); err != nil {
		return "", err
	}
	if cfg.LocationName == "" {
		return "Home Assistant", nil
	}
	return cfg.LocationName, nil
}

type haState struct {
	EntityID   string         `json:"entity_id"`
	State      string         `json:"state"`
	Attributes map[string]any `json:"attributes"`
}

func (s haState) view() map[string]any {
	out := map[string]any{"entity_id": s.EntityID, "state": s.State}
	if name, _ := s.Attributes["friendly_name"].(string); name != "" {
		out["name"] = name
	}
	if unit, _ := s.Attributes["unit_of_measurement"].(string); unit != "" {
		out["unit"] = unit
	}
	return out
}

var haNameRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// maxStates caps how many devices one look-up returns.
const maxStates = 50

func (HomeAssistant) Tools() []Tool {
	return []Tool{
		{
			Def: tools.Definition{ID: "homeassistant.states", Name: "Check Home Assistant", Capability: "homeassistant", Risk: tools.RiskRead, DefaultPolicy: tools.PolicyAllow, Prefetch: true,
				Description: "List devices and their current states, such as lights, switches, and sensors. filter narrows by name or entity id, such as \"kitchen\" or \"light.\".",
				Schema:      `{"filter":"string"}`},
			Run: func(ctx context.Context, c *http.Client, cred Credential, args map[string]any) (map[string]any, error) {
				base, err := haBase(cred)
				if err != nil {
					return nil, err
				}
				var states []haState
				if err := call(ctx, c, http.MethodGet, base+"/api/states", haHeader(cred), nil, &states); err != nil {
					return nil, err
				}
				filter := strings.ToLower(str(args, "filter"))
				sort.Slice(states, func(i, j int) bool { return states[i].EntityID < states[j].EntityID })
				out := make([]any, 0, maxStates)
				matched := 0
				for _, s := range states {
					name, _ := s.Attributes["friendly_name"].(string)
					if filter != "" && !strings.Contains(strings.ToLower(s.EntityID+" "+name), filter) {
						continue
					}
					matched++
					if len(out) < maxStates {
						out = append(out, s.view())
					}
				}
				return map[string]any{"devices": out, "matched": matched}, nil
			},
			Learn: haNames,
		},
		{
			Def: tools.Definition{ID: "homeassistant.call", Name: "Control Home Assistant", Capability: "homeassistant", Risk: tools.RiskWrite, DefaultPolicy: tools.PolicyAsk,
				Description: "Call a Home Assistant service on a device, such as domain \"light\", service \"turn_on\", entity_id \"light.kitchen\".",
				Schema:      `{"domain":"string","service":"string","entity_id":"string"}`},
			Run: func(ctx context.Context, c *http.Client, cred Credential, args map[string]any) (map[string]any, error) {
				base, err := haBase(cred)
				if err != nil {
					return nil, err
				}
				domain, service, entity := haService(str(args, "domain"), str(args, "service"), str(args, "entity_id"))
				if !haNameRe.MatchString(domain) || !haNameRe.MatchString(service) {
					return nil, fmt.Errorf("domain and service must be names such as light and turn_on")
				}
				if entity == "" {
					return nil, fmt.Errorf("entity_id is required")
				}
				var changed []haState
				u := fmt.Sprintf("%s/api/services/%s/%s", base, domain, service)
				if err := call(ctx, c, http.MethodPost, u, haHeader(cred), map[string]string{"entity_id": entity}, &changed); err != nil {
					return nil, err
				}
				list := make([]any, 0, len(changed))
				for _, s := range changed {
					list = append(list, s.view())
				}
				return map[string]any{"called": domain + "." + service, "entity_id": entity, "changed": list}, nil
			},
		},
	}
}

// haNames are device names from a states result, such as "Porch
// temperature", so "what is the porch temperature?" is about Home Assistant.
func haNames(result map[string]any) []string {
	devices, _ := result["devices"].([]any)
	seen := map[string]bool{}
	var out []string
	for _, d := range devices {
		m, _ := d.(map[string]any)
		name, _ := m["name"].(string)
		if name == "" {
			id, _ := m["entity_id"].(string)
			if k := strings.Index(id, "."); k >= 0 {
				name = strings.ReplaceAll(id[k+1:], "_", " ")
			}
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if len(name) >= 4 && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// haService reads the call models tend to write: "light.turn_on" as the
// service carries its own domain, and a missing domain comes from the
// device, so light.porch gives light.
func haService(domain, service, entity string) (string, string, string) {
	if d, s, ok := strings.Cut(service, "."); ok {
		domain, service = d, s
	}
	if domain == "" {
		domain, _, _ = strings.Cut(entity, ".")
	}
	return strings.ToLower(domain), strings.ToLower(service), entity
}
