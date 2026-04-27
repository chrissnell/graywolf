package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
	"gorm.io/gorm"
)

// validationErr is a sentinel-wrapper used by cross-table validation
// callers (e.g. ValidateChannelRef) to signal that a returned error
// should be surfaced as an HTTP 400 rather than the default 500 the
// generic handlers use for store errors. Detected via errors.As /
// errors.Is in handleCreate / handleUpdate and unwrapped for the body.
//
// Kept unexported — callers use validationError(err) to wrap and
// isValidationErr(err) to detect.
type validationErr struct{ err error }

func (v validationErr) Error() string { return v.err.Error() }
func (v validationErr) Unwrap() error { return v.err }

// validationError wraps err as a handler-level validation failure so
// handleCreate / handleUpdate turn it into a 400 response. Callers pass
// the original error; the wrapper stores it verbatim and surfaces the
// underlying message in the body. Returns the zero-value validationErr
// as an error interface so caller code reads as a one-liner.
func validationError(err error) error {
	if err == nil {
		return nil
	}
	return validationErr{err: err}
}

// isValidationErr unwraps err looking for a validationErr; returns the
// underlying error on hit, nil on miss. Used by handleCreate /
// handleUpdate to route 400 vs 500.
func isValidationErr(err error) error {
	var v validationErr
	if errors.As(err, &v) {
		return v.err
	}
	return nil
}

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

// handleGet is a generic GET-by-id handler. Only gorm.ErrRecordNotFound
// maps to 404; every other store error routes through internalError so
// real failures (connection loss, context cancel, driver faults) are
// logged and surfaced as 500 with a sanitized body instead of being
// silently masked as "not found".
func handleGet[TModel any, TResp any](
	s *Server,
	w http.ResponseWriter,
	r *http.Request,
	op string,
	id uint32,
	get func(ctx context.Context, id uint32) (TModel, error),
	toResp func(TModel) TResp,
) {
	m, err := get(r.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, op, err)
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
		if v := isValidationErr(err); v != nil {
			badRequest(w, v.Error())
			return
		}
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
		if v := isValidationErr(err); v != nil {
			badRequest(w, v.Error())
			return
		}
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
