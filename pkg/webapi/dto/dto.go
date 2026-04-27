// Package dto defines the request and response shapes accepted and
// returned by pkg/webapi. DTOs decouple the HTTP contract from
// configstore storage models so the DB schema can evolve without
// changing the API surface, and vice versa.
//
// Each mutable resource exposes:
//
//   - A request struct used for both POST (create) and PUT (update).
//     It implements Validator.
//   - ToModel / ToUpdate that map into a configstore.* value.
//   - A response struct + FromModel conversion that masks any internal
//     fields (timestamps, denormalized state).
//
// Read-only resources continue to return storage models directly;
// add DTOs only if the shape needs to diverge.
package dto

// Validator is implemented by request DTOs that can validate
// themselves. The generic handler helpers require it so bad input
// turns into a 400 before any store work happens.
type Validator interface {
	Validate() error
}
