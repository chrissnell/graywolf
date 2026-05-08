package main

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// bearerAuth gates next behind an Authorization: Bearer <token> header.
// Constant-time compare against the configured token.
func bearerAuth(token string, next http.Handler) http.Handler {
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		if !strings.HasPrefix(got, "Bearer ") {
			http.Error(w, "missing bearer", http.StatusUnauthorized)
			return
		}
		if subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(got, "Bearer ")), want) != 1 {
			http.Error(w, "bad bearer", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
