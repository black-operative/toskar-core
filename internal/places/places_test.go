package places

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/cache"
)

// fakeOSM serves Nominatim, Overpass, and OSRM answers for Juneau.
type fakeOSM struct {
	mu       sync.Mutex
	requests []string
	geocodes []time.Time
	overpass []string
	noRoute  bool
	busy     bool
}

func (f *fakeOSM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
	if r.Header.Get("User-Agent") == "" {
		http.Error(w, "no user agent", http.StatusForbidden)
		return
	}
	switch {
	case r.URL.Path == "/search" || r.URL.Path == "/lookup":
		f.mu.Lock()
		f.geocodes = append(f.geocodes, time.Now())
		f.mu.Unlock()
		q := r.URL.Query().Get("q")
		if strings.Contains(q, "Nowhere") {
			fmt.Fprint(w, `[]`)
			return
		}
		name, lat, lon := "Juneau", "58.3019613", "-134.4196751"
		if strings.Contains(q, "Airport") {
			name, lat, lon = "Juneau International Airport", "58.3547", "-134.5768"
		}
		if r.URL.Path == "/lookup" {
			name = "Coppa"
		}
		fmt.Fprintf(w, `[{"osm_type":"node","osm_id":42,"lat":%q,"lon":%q,"category":"amenity","type":"cafe","name":%q,"display_name":"%s, Juneau, Alaska","extratags":{"opening_hours":"Mo-Fr 06:30-17:00","website":"https://coppa.example"}}]`, lat, lon, name, name)
	case r.URL.Path == "/interpreter":
		if f.busy {
			http.Error(w, "busy", http.StatusGatewayTimeout)
			return
		}
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		f.mu.Lock()
		f.overpass = append(f.overpass, form.Get("data"))
		f.mu.Unlock()
		if strings.Contains(form.Get("data"), "around:2000,") && strings.Contains(form.Get("data"), `"pharmacy"`) {
			fmt.Fprint(w, `{"elements":[]}`)
			return
		}
		fmt.Fprint(w, `{"elements":[
			{"type":"node","id":2,"lat":58.31,"lon":-134.43,"tags":{"name":"Far Cafe","amenity":"cafe"}},
			{"type":"node","id":1,"lat":58.3018,"lon":-134.4214,"tags":{"name":"Coppa","amenity":"cafe","addr:housenumber":"917","addr:street":"Glacier Avenue","addr:city":"Juneau","opening_hours":"Mo-Th 06:30-17:00"}},
			{"type":"way","id":3,"center":{"lat":58.302,"lon":-134.42},"tags":{"amenity":"cafe"}}]}`)
	case strings.Contains(r.URL.Path, "/route/v1/"):
		if f.noRoute {
			fmt.Fprint(w, `{"code":"NoRoute","routes":[]}`)
			return
		}
		fmt.Fprint(w, `{"code":"Ok","routes":[{"distance":15764.3,"duration":1359.7,"legs":[{"steps":[
			{"name":"Glacier Avenue","distance":400,"maneuver":{"type":"depart","bearing_after":300}},
			{"name":"Egan Drive","distance":15000,"maneuver":{"type":"turn","modifier":"left"}},
			{"name":"","distance":300,"maneuver":{"type":"roundabout","exit":2}},
			{"name":"","distance":0,"maneuver":{"type":"arrive"}}]}]}]}`)
	default:
		http.NotFound(w, r)
	}
}

func newClient(t *testing.T, f *fakeOSM) (*Client, *[]string) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	var sent []string
	var mu sync.Mutex
	c := &Client{Geocoder: srv.URL, Overpass: srv.URL + "/interpreter", Router: srv.URL, geocodeGap: 50 * time.Millisecond,
		Cache: cache.New[[]byte](CachePolicy),
		Record: func(_ context.Context, host, detail string) {
			mu.Lock()
			sent = append(sent, detail)
			mu.Unlock()
		}}
	return c, &sent
}

func asJSON(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

// "Coffee near Juneau" finds cafes around Juneau, nearest first, with their
// hours and both units of distance (Gungnir §25).
func TestSearchNearby(t *testing.T) {
	f := &fakeOSM{}
	c, sent := newClient(t, f)
	tool := &SearchTool{Client: c}
	res, err := tool.Execute(context.Background(), map[string]any{"query": "coffee shops", "near": "Juneau"})
	if err != nil {
		t.Fatal(err)
	}
	out := asJSON(t, res)
	places := out["places"].([]any)
	if len(places) != 2 || out["kind"] != "cafe" || out["attribution"] != Attribution {
		t.Fatalf("result %v", out)
	}
	coppa := places[0].(map[string]any)
	if coppa["name"] != "Coppa" || coppa["address"] != "917 Glacier Avenue, Juneau" || coppa["opening_hours"] == "" || coppa["url"] != "https://www.openstreetmap.org/node/1" {
		t.Fatalf("nearest %v", coppa)
	}
	d := coppa["distance"].(map[string]any)
	if d["meters"].(float64) > 200 || d["miles"] == nil {
		t.Fatalf("distance %v", d)
	}
	if len(*sent) != 2 || (*sent)[0] != "Juneau" || !strings.HasPrefix((*sent)[1], "cafe within 2000 m of 58.3020,-134.4197") {
		t.Fatalf("recorded %v", *sent)
	}
	// A repeat is answered from memory and sends nothing.
	before := len(f.requests)
	if _, err := tool.Execute(context.Background(), map[string]any{"query": "coffee shops", "near": "Juneau"}); err != nil {
		t.Fatal(err)
	}
	if len(f.requests) != before || len(*sent) != 2 {
		t.Fatalf("repeat sent %d requests, recorded %v", len(f.requests)-before, *sent)
	}
}

// Nothing close: it looks farther once.
func TestSearchWidens(t *testing.T) {
	f := &fakeOSM{}
	c, _ := newClient(t, f)
	res, err := (&SearchTool{Client: c}).Execute(context.Background(), map[string]any{"query": "pharmacy", "near": "58.30,-134.42"})
	if err != nil {
		t.Fatal(err)
	}
	if res["radius_m"] != 10000 || len(f.overpass) != 2 || len(f.geocodes) != 0 {
		t.Fatalf("radius %v, overpass %d, geocodes %d", res["radius_m"], len(f.overpass), len(f.geocodes))
	}
}

// A busy Overpass falls back to the geocoder, still nearest first.
func TestSearchFallsBackWhenOverpassIsBusy(t *testing.T) {
	f := &fakeOSM{busy: true}
	c, _ := newClient(t, f)
	res, err := (&SearchTool{Client: c}).Execute(context.Background(), map[string]any{"query": "coffee", "near": "Juneau"})
	if err != nil {
		t.Fatal(err)
	}
	out := asJSON(t, res)
	places := out["places"].([]any)
	if len(places) != 1 || out["near"] == nil || places[0].(map[string]any)["distance"] == nil {
		t.Fatalf("fallback %v", out)
	}
	if len(f.geocodes) != 2 {
		t.Fatalf("geocoder asked %d times", len(f.geocodes))
	}
}

func TestSearchByNameAndDetails(t *testing.T) {
	f := &fakeOSM{}
	c, _ := newClient(t, f)
	res, err := (&SearchTool{Client: c}).Execute(context.Background(), map[string]any{"query": "Juneau International Airport"})
	if err != nil {
		t.Fatal(err)
	}
	p := asJSON(t, res)["places"].([]any)[0].(map[string]any)
	if p["name"] != "Juneau International Airport" || p["id"] != "node/42" {
		t.Fatalf("place %v", p)
	}
	res, err = (&DetailsTool{Client: c}).Execute(context.Background(), map[string]any{"id": "node/42"})
	if err != nil {
		t.Fatal(err)
	}
	if d := asJSON(t, res)["place"].(map[string]any); d["name"] != "Coppa" || d["website"] != "https://coppa.example" {
		t.Fatalf("details %v", d)
	}
	if _, err := (&DetailsTool{Client: c}).Execute(context.Background(), map[string]any{"id": "Coppa"}); err == nil {
		t.Fatal("a name was accepted as an id")
	}
	// Geocoder requests are spaced out, as Nominatim asks.
	if gap := f.geocodes[1].Sub(f.geocodes[0]); gap < 45*time.Millisecond {
		t.Errorf("geocoder requests %v apart", gap)
	}
}

func TestRoute(t *testing.T) {
	f := &fakeOSM{}
	c, _ := newClient(t, f)
	res, err := (&RouteTool{Client: c}).Execute(context.Background(), map[string]any{"from": "Juneau", "to": "Juneau International Airport", "mode": "drive"})
	if err != nil {
		t.Fatal(err)
	}
	out := asJSON(t, res)
	if out["mode"] != "driving" || out["minutes"] != 22.7 || out["distance"].(map[string]any)["km"] != 15.8 {
		t.Fatalf("route %v", out)
	}
	var got []string
	for _, s := range out["steps"].([]any) {
		got = append(got, s.(map[string]any)["instruction"].(string))
	}
	want := []string{"Head northwest on Glacier Avenue", "Turn left onto Egan Drive", "At the roundabout, take exit 2", "Arrive at the destination"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("steps %q", got)
	}
	if !strings.Contains(strings.Join(f.requests, " "), "/route/v1/driving/") {
		t.Errorf("requests %v", f.requests)
	}
	if _, err := (&RouteTool{Client: c}).Execute(context.Background(), map[string]any{"from": "Juneau", "to": "Juneau", "mode": "teleport"}); err == nil {
		t.Error("an unknown mode was accepted")
	}
	if _, err := (&RouteTool{Client: c}).Execute(context.Background(), map[string]any{"from": "Nowhere at all", "to": "Juneau"}); err == nil || !strings.Contains(err.Error(), "from: no place") {
		t.Errorf("unknown place: %v", err)
	}
}

// Without a route, the straight-line distance still answers.
func TestDistance(t *testing.T) {
	f := &fakeOSM{noRoute: true}
	c, _ := newClient(t, f)
	res, err := (&DistanceTool{Client: c}).Execute(context.Background(), map[string]any{"from": "58.3019,-134.4197", "to": "58.3547,-134.5768", "mode": "walking"})
	if err != nil {
		t.Fatal(err)
	}
	out := asJSON(t, res)
	line := out["straight_line"].(map[string]any)
	if line["km"].(float64) < 10 || line["km"].(float64) > 11 || out["route_error"] == nil || len(f.geocodes) != 0 {
		t.Fatalf("distance %v", out)
	}
}

func TestKinds(t *testing.T) {
	for q, want := range map[string]string{"Where can I get coffee?": "cafe", "gas station": "fuel", "a pharmacy open late": "pharmacy", "bookstores": "books", "Juneau airport": ""} {
		tag, _ := kindOf(q)
		if tag.Value != want {
			t.Errorf("%q: %q, want %q", q, tag.Value, want)
		}
	}
}
