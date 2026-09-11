package state

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/fwtllh-png/QCode/internal/persist/state/eventlog"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

func TestStoreReconcile(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    string
		wantError bool
	}{
		{name: "consistent"},
		{name: "missing index", mutate: `DELETE FROM event_index WHERE sequence = 1`},
		{name: "missing reservation", mutate: `DELETE FROM event_reservations WHERE sequence = 1`},
		{name: "missing projection", mutate: `DELETE FROM event_index WHERE sequence = 1; DELETE FROM event_reservations WHERE sequence = 1`},
		{name: "reserved", mutate: `UPDATE event_reservations SET status = 'reserved' WHERE sequence = 1`},
		{name: "abandoned with durable event", mutate: `UPDATE event_reservations SET status = 'abandoned' WHERE sequence = 1`},
		{name: "conflicting reservation", mutate: `UPDATE event_reservations SET event_id = 'wrong' WHERE sequence = 1`, wantError: true},
		{name: "conflicting abandoned reservation", mutate: `UPDATE event_reservations SET status = 'abandoned', event_id = 'wrong' WHERE sequence = 1`, wantError: true},
		{name: "wrong index id", mutate: `UPDATE event_index SET event_id = 'wrong' WHERE sequence = 1`},
		{name: "wrong index offset", mutate: `UPDATE event_index SET log_offset = 123 WHERE sequence = 1`},
		{name: "wrong index length", mutate: `UPDATE event_index SET log_length = 123 WHERE sequence = 1`},
		{name: "wrong index hash", mutate: `UPDATE event_index SET sha256 = printf('%064d', 0) WHERE sequence = 1`},
		{name: "wrong index thread", mutate: `UPDATE event_index SET thread_id = 'wrong' WHERE sequence = 1`},
		{name: "wrong index turn", mutate: `UPDATE event_index SET turn_id = 'wrong' WHERE sequence = 1`},
		{name: "wrong index item", mutate: `UPDATE event_index SET item_id = 'wrong' WHERE sequence = 1`},
		{name: "wrong index kind", mutate: `UPDATE event_index SET kind = 'wrong' WHERE sequence = 1`},
		{name: "wrong index timestamp", mutate: `UPDATE event_index SET created_at = 'wrong' WHERE sequence = 1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := Open(t.Context(), Options{DataDir: root})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.CloseAll(context.Background()) })
			first := testEvent(t, 1)
			if err := store.Append(t.Context(), first); err != nil {
				t.Fatal(err)
			}
			if err := store.Append(t.Context(), testEventWithData(t, 2, &protocol.OutputDeltaData{Text: "noise"})); err != nil {
				t.Fatal(err)
			}
			if err := store.Append(t.Context(), testEvent(t, 3)); err != nil {
				t.Fatal(err)
			}
			const untouched = "2020-01-01T00:00:00Z"
			if _, err := store.SQLite().DB().ExecContext(t.Context(),
				`UPDATE event_reservations SET updated_at = ? WHERE sequence = 3`, untouched,
			); err != nil {
				t.Fatal(err)
			}
			if tc.mutate != "" {
				if _, err := store.SQLite().DB().ExecContext(t.Context(), tc.mutate); err != nil {
					t.Fatal(err)
				}
			} else {
				for _, table := range []string{"event_index", "event_reservations"} {
					for _, action := range []string{"INSERT", "UPDATE", "DELETE"} {
						if _, err := store.SQLite().DB().ExecContext(t.Context(),
							`CREATE TRIGGER no_`+action+`_`+table+` BEFORE `+action+` ON `+table+
								` BEGIN SELECT RAISE(ABORT, 'consistent projection must not be rewritten'); END`,
						); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if err := store.CloseAll(t.Context()); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(t.Context(), Options{DataDir: root})
			if tc.wantError {
				if reopened != nil {
					_ = reopened.CloseAll(t.Context())
				}
				if !errors.Is(err, ErrProjection) {
					t.Fatalf("Open error = %v, want ErrProjection", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			store = reopened
			records, err := store.events.ReplayRecords(t.Context(), 0)
			if err != nil || len(records) != 2 {
				t.Fatalf("records = %d, err = %v", len(records), err)
			}
			assertEventProjection(t, store, records[0])
			assertEventProjection(t, store, records[1])
			var updated string
			if err := store.SQLite().DB().QueryRowContext(t.Context(),
				`SELECT updated_at FROM event_reservations WHERE sequence = 3`,
			).Scan(&updated); err != nil {
				t.Fatal(err)
			}
			if updated != untouched {
				t.Fatalf("unaffected projection rewritten at %s", updated)
			}
			last, err := store.LastSequence(t.Context())
			if err != nil || last != 3 {
				t.Fatalf("high watermark = %d, err = %v", last, err)
			}
			// The durable log remains the authority, including when index repair is needed.
			event, found, err := store.EventByID(t.Context(), first.ID)
			if err != nil || !found || event.ID != first.ID {
				t.Fatalf("EventByID = %v, %v, %v", event.ID, found, err)
			}
			if err := store.CloseAll(t.Context()); err != nil {
				t.Fatal(err)
			}
			log, err := eventlog.Open(filepath.Join(root, "events-v1.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close(t.Context())
			events, err := log.Replay(t.Context(), 0)
			if err != nil || len(events) != 2 || events[0].ID != first.ID {
				t.Fatalf("log after repair = %v, err = %v", events, err)
			}
		})
	}
}

func BenchmarkStoreReconcile(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("events=%d", count), func(b *testing.B) {
			store, err := Open(b.Context(), Options{DataDir: b.TempDir()})
			if err != nil {
				b.Fatal(err)
			}
			defer store.CloseAll(context.Background())
			for sequence := 1; sequence <= count; sequence++ {
				if err := store.Append(b.Context(), testEvent(b, protocol.Cursor(sequence))); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := store.reconcile(b.Context()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func assertEventProjection(t *testing.T, store *Store, record eventlog.Record) {
	t.Helper()
	event := record.Event
	var count int
	if err := store.SQLite().DB().QueryRowContext(t.Context(), `
		SELECT COUNT(*) FROM event_index i JOIN event_reservations r USING(sequence)
		WHERE i.sequence = ? AND i.event_id = ? AND i.thread_id IS ? AND i.turn_id IS ?
		  AND i.item_id IS ? AND i.kind = ? AND i.log_offset = ? AND i.log_length = ?
		  AND i.sha256 = ? AND i.created_at = ? AND r.event_id = i.event_id
		  AND r.status = 'committed'`,
		event.Sequence, event.ID, nullString(event.ThreadID), nullString(event.TurnID),
		nullString(event.ItemID), event.Kind, record.Evidence.Offset, record.Evidence.Length,
		record.Evidence.SHA256, timestamp(event.CreatedAt),
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("projection does not match durable event")
	}
}
