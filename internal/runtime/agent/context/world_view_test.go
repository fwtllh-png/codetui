package agentcontext

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/provider"
)

func TestWorldViewRebuildsHistoricalBaselineAndKeepsCurrentPrefix(t *testing.T) {
	for _, versions := range []int{1, 22, 100} {
		t.Run(fmt.Sprint(versions), func(t *testing.T) {
			var history []provider.Message
			baseline := WorldBaseline{}
			for i := 0; i < versions; i++ {
				value := provider.TextMessage(provider.RoleSystem, fmt.Sprintf("state %d", i))
				value.Turn = 1
				projection, err := ProjectWorld([]WorldSection{{
					ID: "session_state", Digest: fmt.Sprint(i), Message: &value,
				}}, baseline, history)
				if err != nil {
					t.Fatal(err)
				}
				history = append(history, projection.Messages...)
				baseline = projection.Baseline
			}
			user := provider.TextMessage(provider.RoleUser, "continue")
			user.Turn = 2
			history = append(history, user)
			original := CloneMessages(history)
			view := ProjectContextViewFrom(history, versions)
			if len(view) != 2 || view[0].Text() != fmt.Sprintf("state %d", versions-1) {
				t.Fatalf("historical versions leaked: %+v", view)
			}
			reply := provider.TextMessage(provider.RoleAssistant, "next")
			reply.Turn = 2
			next := ProjectContextViewFrom(append(CloneMessages(history), reply), versions)
			if !reflect.DeepEqual(view, next[:len(view)]) || !reflect.DeepEqual(history, original) {
				t.Fatal("projection rewrote prefix or durable history")
			}
			removed, err := ProjectWorld(nil, baseline, history)
			if err != nil {
				t.Fatal(err)
			}
			later := provider.TextMessage(provider.RoleUser, "later")
			later.Turn = 3
			removedView := ProjectContextViewFrom(append(append(CloneMessages(history), removed.Messages...), later), len(history))
			if len(removedView) != 1 || removedView[0].Text() != "later" {
				t.Fatalf("removed section resurfaced: %+v", removedView)
			}
		})
	}
}
