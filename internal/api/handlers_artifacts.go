package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/imagegen"
	"github.com/yeixio/toskar-core/internal/mimir"
)

// BindArtifacts attaches the file routes: chat attachments and files the
// assistant produced.
func (s *Server) BindArtifacts(store *artifacts.Store) { s.artifacts = store }

func (s *Server) artifactRoutes(api *mux.Router) {
	api.HandleFunc("/artifacts", s.artifactHandler(s.handleUploadArtifact)).Methods(http.MethodPost)
	api.HandleFunc("/artifacts/{id}", s.artifactHandler(s.handleGetArtifact)).Methods(http.MethodGet)
	api.HandleFunc("/artifacts/{id}", s.artifactHandler(s.handleDeleteArtifact)).Methods(http.MethodDelete)
	api.HandleFunc("/artifacts/{id}/content", s.artifactHandler(s.handleArtifactContent)).Methods(http.MethodGet)
	api.HandleFunc("/conversations/{id}/artifacts", s.artifactHandler(s.handleConversationArtifacts)).Methods(http.MethodGet)
}

func (s *Server) artifactHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.artifacts == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Files are not available.", nil)
			return
		}
		h(w, r)
	}
}

func writeArtifactErr(w http.ResponseWriter, err error) {
	if errors.Is(err, artifacts.ErrNotFound) {
		writeErrFrom(w, http.StatusNotFound, "NOT_FOUND", err)
		return
	}
	writeErrFrom(w, http.StatusBadRequest, "BAD_REQUEST", err)
}

// handleUploadArtifact saves a file to attach to a chat. The body carries
// text, or content_base64 for binary files such as .xlsx and .pdf.
func (s *Server) handleUploadArtifact(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, artifacts.MaxBytes*4/3+4096)
	var body struct {
		Name           string `json:"name"`
		Text           string `json:"text"`
		ContentBase64  string `json:"content_base64"`
		ConversationID string `json:"conversation_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "The file could not be read. Files can be up to 25 MB.", nil)
		return
	}
	// Audio is kept as it is, to be played and transcribed (speech.transcribe),
	// and so are images, to be shown and edited (image.edit).
	kept := artifacts.IsAudio(body.Name) || imagegen.IsEditable(body.Name)
	if !kept && !mimir.Attachable(body.Name) {
		writeErr(w, http.StatusBadRequest, "UNSUPPORTED_FILE",
			fmt.Sprintf("Toskar can't read %s yet. Attach a document, spreadsheet, PDF, code file, audio, or a PNG or JPEG image.", artifacts.CleanName(body.Name)),
			map[string]any{"name": artifacts.CleanName(body.Name)})
		return
	}
	data := []byte(body.Text)
	if body.ContentBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(body.ContentBase64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "content_base64 is not valid base64", nil)
			return
		}
		data = decoded
	}
	// Check the file can be read now, so a broken file is reported when it
	// is attached rather than when the question is asked.
	if !kept {
		if _, err := mimir.FilePassages(body.Name, data); err != nil {
			writeErr(w, http.StatusBadRequest, "UNREADABLE_FILE", fmt.Sprintf("Toskar can't read %s: %s", artifacts.CleanName(body.Name), err.Error()),
				map[string]any{"name": artifacts.CleanName(body.Name), "cause": err.Error()})
			return
		}
	}
	a, err := s.artifacts.Save(r.Context(), artifacts.Input{
		ConversationID: body.ConversationID,
		Name:           body.Name,
		Producer:       artifacts.ProducerUser,
		Data:           data,
	})
	if err != nil {
		writeArtifactErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleGetArtifact(w http.ResponseWriter, r *http.Request) {
	a, err := s.artifacts.Get(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeArtifactErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleDeleteArtifact(w http.ResponseWriter, r *http.Request) {
	if err := s.artifacts.Delete(r.Context(), mux.Vars(r)["id"]); err != nil {
		writeArtifactErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleConversationArtifacts(w http.ResponseWriter, r *http.Request) {
	list, err := s.artifacts.List(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeArtifactErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleArtifactContent serves a file's bytes. Files download unless
// ?inline=1 asks to view a PDF or image in place. Nothing served here may run
// as a page on this origin, so HTML and SVG always download and the response
// is sandboxed.
func (s *Server) handleArtifactContent(w http.ResponseWriter, r *http.Request) {
	a, data, err := s.artifacts.Read(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeArtifactErr(w, err)
		return
	}
	disposition := "attachment"
	if r.URL.Query().Get("inline") == "1" && (a.Kind == "pdf" || a.Kind == "image" || a.Kind == "audio" || a.Kind == "video") {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", a.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q; filename*=UTF-8''%s",
		disposition, asciiName(a.Name), url.PathEscape(a.Name)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; style-src 'unsafe-inline'")
	w.Header().Set("Cache-Control", "private, no-store")
	// ServeContent answers range requests, which browsers need to play audio.
	http.ServeContent(w, r, "", a.CreatedAt, bytes.NewReader(data))
}

// asciiName is the fallback filename for clients that ignore filename*.
func asciiName(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
}
