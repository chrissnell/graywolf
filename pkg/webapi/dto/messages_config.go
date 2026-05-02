package dto

// MessagesConfig is the on-wire shape of GET/PUT /api/messages/config.
type MessagesConfig struct {
	TxChannel uint32 `json:"tx_channel"` // 0 = auto-resolve
}
