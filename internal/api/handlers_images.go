package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/imagegen"
)

// BindImages attaches image generation setup (Gungnir §17).
func (s *Server) BindImages(setup *imagegen.Setup) {
	s.images = setup
}

// BindVideo attaches video generation setup (Gungnir §27).
func (s *Server) BindVideo(setup *imagegen.Setup) {
	s.video = setup
}

func (s *Server) imageRoutes(api *mux.Router) {
	s.setupRoutes(api, "/images", func() *imagegen.Setup { return s.images }, "Image generation is not available.")
	s.setupRoutes(api, "/video", func() *imagegen.Setup { return s.video }, "Video generation is not available.")
}

// setupRoutes serves one kind's setup: status, start, stop, and removing a
// model.
func (s *Server) setupRoutes(api *mux.Router, prefix string, setup func() *imagegen.Setup, missing string) {
	bound := func(w http.ResponseWriter) *imagegen.Setup {
		st := setup()
		if st == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", missing, nil)
		}
		return st
	}
	// What is installed, the models on offer, and any setup in progress.
	api.HandleFunc(prefix+"/setup", func(w http.ResponseWriter, r *http.Request) {
		if st := bound(w); st != nil {
			writeJSON(w, http.StatusOK, st.Status())
		}
	}).Methods(http.MethodGet)
	// Install stable-diffusion.cpp and a model in the background.
	api.HandleFunc(prefix+"/setup", func(w http.ResponseWriter, r *http.Request) {
		st := bound(w)
		if st == nil {
			return
		}
		var body struct {
			ModelID string `json:"model_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
			return
		}
		if err := st.Start(body.ModelID); err != nil {
			writeErrFrom(w, http.StatusConflict, "SETUP_REFUSED", err)
			return
		}
		writeJSON(w, http.StatusAccepted, st.Status())
	}).Methods(http.MethodPost)
	// Stop a setup; what was downloaded is kept.
	api.HandleFunc(prefix+"/setup", func(w http.ResponseWriter, r *http.Request) {
		if st := bound(w); st != nil {
			st.Cancel()
			writeJSON(w, http.StatusOK, st.Status())
		}
	}).Methods(http.MethodDelete)
	// Delete an installed model.
	api.HandleFunc(prefix+"/models/{id}", func(w http.ResponseWriter, r *http.Request) {
		st := bound(w)
		if st == nil {
			return
		}
		if err := st.Remove(mux.Vars(r)["id"]); err != nil {
			writeErrFrom(w, http.StatusConflict, "MODEL_BUSY", err)
			return
		}
		writeJSON(w, http.StatusOK, st.Status())
	}).Methods(http.MethodDelete)
}
