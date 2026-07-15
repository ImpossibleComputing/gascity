package mail

import "sort"

// SortMessagesNewestFirst sorts messages in the canonical inbox order:
// created_at descending, with message ID descending as the deterministic
// tie-break. Zero/missing timestamps are the Go zero time, so they sort after
// any real timestamp and still tie-break by ID.
func SortMessagesNewestFirst(messages []Message) {
	sort.Slice(messages, func(i, j int) bool {
		if messages[i].CreatedAt.Equal(messages[j].CreatedAt) {
			return messages[i].ID > messages[j].ID
		}
		return messages[i].CreatedAt.After(messages[j].CreatedAt)
	})
}
