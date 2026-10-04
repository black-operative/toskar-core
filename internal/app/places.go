package app

import (
	"context"
	"net/http"
	"time"

	"github.com/yeixio/toskar-core/internal/cache"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/places"
	"github.com/yeixio/toskar-core/internal/version"
)

// registerPlaces adds places and routes with OpenStreetMap services
// (Gungnir §25). Each request is recorded in What left this computer, and
// repeats within the hour are answered from memory.
func (a *App) registerPlaces(cfg config.Config) {
	c := &places.Client{
		Geocoder: cfg.PlacesGeocoderURL, Overpass: cfg.PlacesOverpassURL, Router: cfg.PlacesRouterURL,
		HTTP:      &http.Client{Timeout: 25 * time.Second},
		UserAgent: "Toskar/" + version.Version + " (+https://github.com/yeixio/toskar-core)",
		Cache:     cache.New[[]byte](places.CachePolicy),
		Record: func(ctx context.Context, host, detail string) {
			if a.Egress != nil {
				a.Egress.Add(ctx, egress.Places, "OpenStreetMap ("+host+")", detail)
			}
		},
	}
	if a.Caches != nil {
		_ = a.Caches.Add(c.Cache)
	}
	a.Tools.Register(&places.SearchTool{Client: c})
	a.Tools.Register(&places.DetailsTool{Client: c})
	a.Tools.Register(&places.RouteTool{Client: c})
	a.Tools.Register(&places.DistanceTool{Client: c})
}
