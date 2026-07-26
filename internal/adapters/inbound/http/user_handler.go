package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	app "github.com/example/go-ddd-hex/internal/application/user"
	domain "github.com/example/go-ddd-hex/internal/domain/user"
)

// UserHandler is a DRIVING (inbound) adapter. It translates HTTP <-> the
// application's inbound port (app.UseCases). It depends on the port interface,
// never on the concrete Service.
type UserHandler struct {
	uc  app.UseCases
	log *slog.Logger
}

func NewUserHandler(uc app.UseCases, log *slog.Logger) *UserHandler {
	return &UserHandler{uc: uc, log: log}
}

// Routes mounts the user endpoints on a chi router.
func (h *UserHandler) Routes(r chi.Router) {
	r.Post("/users", h.register)
	r.Get("/users", h.list)
	r.Get("/users/{id}", h.getByID)
	r.Patch("/users/{id}", h.rename)
	r.Post("/users/{id}/activate", h.activate)
	r.Post("/users/{id}/deactivate", h.deactivate)
	r.Delete("/users/{id}", h.delete)
}

func (h *UserHandler) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decode(w, r, &req) {
		return
	}
	view, err := h.uc.Register(r.Context(), app.RegisterCommand{Name: req.Name, Email: req.Email})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (h *UserHandler) list(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	views, err := h.uc.List(r.Context(), limit, offset)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, views)
}

func (h *UserHandler) getByID(w http.ResponseWriter, r *http.Request) {
	view, err := h.uc.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *UserHandler) rename(w http.ResponseWriter, r *http.Request) {
	var req renameRequest
	if !decode(w, r, &req) {
		return
	}
	view, err := h.uc.Rename(r.Context(), app.RenameCommand{ID: chi.URLParam(r, "id"), Name: req.Name})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *UserHandler) activate(w http.ResponseWriter, r *http.Request) {
	view, err := h.uc.Activate(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *UserHandler) deactivate(w http.ResponseWriter, r *http.Request) {
	view, err := h.uc.Deactivate(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *UserHandler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.uc.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeError maps domain/application errors onto HTTP status codes. This is the
// single boundary where domain semantics become transport semantics.
func (h *UserHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrEmailTaken):
		writeJSON(w, http.StatusConflict, errorResponse{Error: err.Error()})
	case app.IsValidationError(err):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
	default:
		h.log.ErrorContext(r.Context(), "unhandled error", slog.Any("error", err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
	}
}

// decode reads and validates a JSON body, writing a 400 on failure.
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid JSON body: " + err.Error()})
		return false
	}
	return true
}

// decodeOptional decodes a JSON body if present, ignoring EOF (empty body). It
// is used by endpoints where the request body is optional (e.g. cancel reason).
func decodeOptional(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload != nil {
		_ = json.NewEncoder(w).Encode(payload)
	}
}
