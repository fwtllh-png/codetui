package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/provider"
	"github.com/fwtllh-png/QCode/internal/adapter/tool"
)

func TestCurrentTurnSpecReturnsFrozenActiveSpec(t *testing.T) {
	engine := &Engine{}
	engine.publishScope(&Scope{
		engine: engine,
		spec: TurnSpec{
			Identity: TurnIdentity{TurnID: "turn-active"},
			Request: TurnRequest{
				Prompt: "inspect auth",
				Attachments: []provider.Attachment{{
					Name: "context.txt", Data: []byte("original"),
				}},
			},
		},
	})

	first := engine.CurrentTurnSpec()
	if first.Identity.TurnID != "turn-active" ||
		first.Request.Prompt != "inspect auth" ||
		len(first.Request.Attachments) != 1 {
		t.Fatalf("snapshot = %+v", first)
	}
	first.Request.Attachments[0].Name = "mutated.txt"

	second := engine.CurrentTurnSpec()
	if second.Request.Attachments[0].Name != "context.txt" {
		t.Fatalf("snapshot aliases engine state: %+v", second)
	}
}

func TestWorkspaceExcerptReadsBoundedPrefix(t *testing.T) {
	engine := newEngine(
		t, &scriptedProvider{streams: []provider.Stream{textStream("ok")}},
		tool.NewRegistry(nil, nil),
	)
	root := t.TempDir()
	engine.options.Workspace = root
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("line of code\n", 512)
	if err := os.WriteFile(
		filepath.Join(root, "pkg", "node.go"), []byte(content), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	excerpt, ok := engine.WorkspaceExcerpt("pkg/node.go", 256)
	if !ok {
		t.Fatal("readable text file produced no excerpt")
	}
	if len(excerpt) > 256 || !strings.HasPrefix(excerpt, "line of code") {
		t.Fatalf("excerpt = %d bytes starting %q", len(excerpt), excerpt[:40])
	}
}

func TestWorkspaceExcerptRejectsEscapesAndUnreadableInput(t *testing.T) {
	engine := newEngine(
		t, &scriptedProvider{streams: []provider.Stream{textStream("ok")}},
		tool.NewRegistry(nil, nil),
	)
	root := t.TempDir()
	engine.options.Workspace = root
	if err := os.WriteFile(
		filepath.Join(root, "empty.go"), nil, 0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "blob.bin"), []byte{0x00, 0xff, 0xfe, 0xfd},
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"../outside.go", "/etc/passwd", "..", ".", "missing.go", "empty.go",
		"blob.bin",
	} {
		if excerpt, ok := engine.WorkspaceExcerpt(path, 256); ok {
			t.Fatalf("excerpt(%q) = %q, want none", path, excerpt)
		}
	}
	if _, ok := engine.WorkspaceExcerpt("node.go", 0); ok {
		t.Fatal("zero bound produced an excerpt")
	}
}
