package webapi

import "net/http"

// requireRemoteActions writes 503 and returns false when the outbound
// Actions service is not wired (typically because the messages.Service
// failed to construct, or NewService returned an error). Handlers call
// this before touching s.remoteActions.
func (s *Server) requireRemoteActions(w http.ResponseWriter) bool {
	if s.remoteActions == nil {
		serviceUnavailable(w, "remote actions service not configured")
		return false
	}
	return true
}
