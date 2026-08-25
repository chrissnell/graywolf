package dto

// AndroidStorageResponse is the body returned by GET /api/android/storage.
// On desktop all fields carry their zero values; the frontend only renders
// the storage section when Platform.isAndroid, so desktop clients never see
// this endpoint in practice.
type AndroidStorageResponse struct {
	UseSDCard       bool   `json:"use_sd_card"`
	SdCardAvailable bool   `json:"sd_card_available"`
	SdCardPath      string `json:"sd_card_path"`
	InternalPath    string `json:"internal_path"`
}
