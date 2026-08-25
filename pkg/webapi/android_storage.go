package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerAndroidStorage(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/android/storage", s.getAndroidStorage)
}

// getAndroidStorage returns the current storage configuration on Android.
// On desktop it returns all-zero values; the frontend only renders the
// storage section when Platform.isAndroid.
func (s *Server) getAndroidStorage(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, dto.AndroidStorageResponse{
		UseSDCard:       s.storageLocation == "sdcard",
		SdCardAvailable: s.sdCardPath != "",
		SdCardPath:      s.sdCardPath,
		InternalPath:    s.internalPath,
	})
}
