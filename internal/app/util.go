package app

import "encoding/json"

func extractMessageID(raw json.RawMessage) int64 {
	var r struct {
		MessageID int64 `json:"message_id"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return 0
	}
	return r.MessageID
}
