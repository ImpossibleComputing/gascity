package mail

import (
	"reflect"
	"testing"
	"time"
)

func TestSortMessagesNewestFirstTieAndZeroTimestamp(t *testing.T) {
	base := time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)
	messages := []Message{
		{ID: "zero-b"},
		{ID: "mid", CreatedAt: base.Add(1 * time.Minute)},
		{ID: "new-a", CreatedAt: base.Add(2 * time.Minute)},
		{ID: "zero-a"},
		{ID: "new-z", CreatedAt: base.Add(2 * time.Minute)},
	}

	SortMessagesNewestFirst(messages)

	var got []string
	for _, msg := range messages {
		got = append(got, msg.ID)
	}
	want := []string{"new-z", "new-a", "mid", "zero-b", "zero-a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted IDs = %#v, want %#v", got, want)
	}
}
