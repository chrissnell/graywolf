package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// decodeJSON reads a JSON request body into T and rejects any unknown
// fields. Handlers route every request decode through this helper so
// the API contract fails loudly when a client sends a misspelled or
// deprecated field instead of silently dropping it.
func decodeJSON[T any](r *http.Request) (T, error) {
	var out T
	dec := json.NewDecoder(r.Body) // decodeJSON: the one permitted call
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}

// handleList is a generic GET-collection handler. It invokes the store
// list operation, runs every model through toResp, and writes a 200
// response. Store errors are routed through internalError so the wire
// body stays sanitized.
func handleList[TModel any, TResp any](
	s *Server,
	w http.ResponseWriter,
	r *http.Request,
	op string,
	list func(ctx context.Context) ([]TModel, error),
	toResp func(TModel) TResp,
) {
	models, err := list(r.Context())
	if err != nil {
		s.internalError(w, r, op, err)
		return
	}
	resp := make([]TResp, len(models))
	for i, m := range models {
		resp[i] = toResp(m)
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGet is a generic GET-by-id handler. A lookup failure maps to
// 404 (the store returns a gorm.ErrRecordNotFound which is not a
// server fault).
func handleGet[TModel any, TResp any](
	s *Server,
	w http.ResponseWriter,
	r *http.Request,
	id uint32,
	get func(ctx context.Context, id uint32) (TModel, error),
	toResp func(TModel) TResp,
) {
	m, err := get(r.Context(), id)
	if err != nil {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, toResp(m))
}

// handleCreate is a generic POST handler. It decodes the request,
// validates it, invokes create, and writes a 201 with the mapped
// response.
func handleCreate[TReq dto.Validator, TModel any, TResp any](
	s *Server,
	w http.ResponseWriter,
	r *http.Request,
	op string,
	create func(ctx context.Context, req TReq) (TModel, error),
	toResp func(TModel) TResp,
) {
	req, err := decodeJSON[TReq](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	m, err := create(r.Context(), req)
	if err != nil {
		s.internalError(w, r, op, err)
		return
	}
	writeJSON(w, http.StatusCreated, toResp(m))
}

// handleUpdate is a generic PUT handler. It decodes the request,
// validates it, invokes update with the caller-supplied id, and
// writes a 200 with the mapped response.
func handleUpdate[TReq dto.Validator, TModel any, TResp any](
	s *Server,
	w http.ResponseWriter,
	r *http.Request,
	op string,
	id uint32,
	update func(ctx context.Context, id uint32, req TReq) (TModel, error),
	toResp func(TModel) TResp,
) {
	req, err := decodeJSON[TReq](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	m, err := update(r.Context(), id, req)
	if err != nil {
		s.internalError(w, r, op, err)
		return
	}
	writeJSON(w, http.StatusOK, toResp(m))
}

// handleDelete is a generic DELETE handler. Writes 204 on success.
func handleDelete(
	s *Server,
	w http.ResponseWriter,
	r *http.Request,
	op string,
	id uint32,
	del func(ctx context.Context, id uint32) error,
) {
	if err := del(r.Context(), id); err != nil {
		s.internalError(w, r, op, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
