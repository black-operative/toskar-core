package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/ratings"
)

// Ratings is community model ratings (#37).
type Ratings interface {
	Get(ctx context.Context, modelID string) (ratings.View, error)
	Put(ctx context.Context, modelID string, in ratings.Input) (ratings.View, error)
	Delete(ctx context.Context, modelID string) error
	Dismiss(ctx context.Context, modelID string) error
	Community(ctx context.Context) (ratings.Community, error)
}

// BindRatings attaches the ratings routes.
func (s *Server) BindRatings(r Ratings) { s.ratings = r }

func (s *Server) ratingsRoutes(api *mux.Router) {
	api.HandleFunc("/ratings/community", s.ratingsHandler(s.handleCommunityRatings)).Methods(http.MethodGet)
	api.HandleFunc("/models/{id}/rating", s.ratingsHandler(s.handleGetRating)).Methods(http.MethodGet)
	api.HandleFunc("/models/{id}/rating", s.ratingsHandler(s.handlePutRating)).Methods(http.MethodPut)
	api.HandleFunc("/models/{id}/rating", s.ratingsHandler(s.handleDeleteRating)).Methods(http.MethodDelete)
	api.HandleFunc("/models/{id}/rating/dismiss", s.ratingsHandler(s.handleDismissRating)).Methods(http.MethodPost)
}

func (s *Server) ratingsHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.ratings == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Model ratings are not available.", nil)
			return
		}
		h(w, r)
	}
}

// writeRatingErr answers a ratings error with its status.
func writeRatingErr(w http.ResponseWriter, err error) {
	var se *ratings.ShareError
	switch {
	case errors.As(err, &se):
		writeErr(w, http.StatusBadGateway, "RATINGS_UNREACHABLE", err.Error(), nil)
	case errors.Is(err, ratings.ErrNoModel):
		writeErr(w, http.StatusNotFound, "MODEL_NOT_FOUND", err.Error(), nil)
	case errors.Is(err, ratings.ErrStars), errors.Is(err, ratings.ErrLanguage):
		writeErr(w, http.StatusBadRequest, "INVALID_RATING", err.Error(), nil)
	case errors.Is(err, ratings.ErrNotShareable):
		writeErr(w, http.StatusUnprocessableEntity, "RATING_NOT_SHAREABLE", err.Error(), nil)
	default:
		writeErrFrom(w, http.StatusInternalServerError, "RATING_FAILED", err)
	}
}

func (s *Server) handleCommunityRatings(w http.ResponseWriter, r *http.Request) {
	c, err := s.ratings.Community(r.Context())
	if err != nil {
		writeRatingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleGetRating(w http.ResponseWriter, r *http.Request) {
	v, err := s.ratings.Get(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeRatingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handlePutRating(w http.ResponseWriter, r *http.Request) {
	var in ratings.Input
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_RATING", "The body must be a rating: stars, tags, and share.", nil)
		return
	}
	v, err := s.ratings.Put(r.Context(), mux.Vars(r)["id"], in)
	if err != nil {
		writeRatingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleDeleteRating(w http.ResponseWriter, r *http.Request) {
	if err := s.ratings.Delete(r.Context(), mux.Vars(r)["id"]); err != nil {
		writeRatingErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDismissRating(w http.ResponseWriter, r *http.Request) {
	if err := s.ratings.Dismiss(r.Context(), mux.Vars(r)["id"]); err != nil {
		writeRatingErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
