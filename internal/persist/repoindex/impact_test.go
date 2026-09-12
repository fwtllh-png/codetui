package repoindex

import (
	"fmt"
	"testing"
)

// The impact tests walk a repository whose files depend on each other in
// known directions: what the reverse closure should reach, how many hops it
// took, and which route found each test.

// impactFixture builds a repository wired as follows:
//
//	core/core.go          the changed file, exports Run
//	mid/mid.go            imports core, exports Wrap (calls Run)
//	app/app.go            imports mid, calls Wrap
//	tests/odd_name.py     imports nothing by name, but references Run —
//	                      a test file no naming convention would pair with core
//	tests/integration.rs  references Wrap — Rust, which has no convention here
func writeImpactFixture(t *testing.T, root string) {
	t.Helper()
	writeFile(t, root, "core/core.go",
		"package core\n\n// Run does the work.\nfunc Run() error { return nil }\n")
	writeFile(t, root, "mid/mid.go", "package mid\n\nimport \"example.com/app/core\"\n\nfunc Wrap() error { return core.Run() }\n")
	writeFile(t, root, "app/app.go", "package main\n\nimport \"example.com/app/mid\"\n\nfunc main() { _ = mid.Wrap() }\n")
	writeFile(t, root, "tests/odd_name.py", "from core import Run\n\ndef check():\n    assert Run() is None\n")
	writeFile(t, root, "tests/integration.rs", "use crate::mid::Wrap;\n\n#[test]\nfn verifies() {\n    let _ = Wrap();\n}\n")
	// Give the Rust test a crate path to reach: a src/ tree beside it.
	writeFile(t, root, "src/mid.rs", "pub fn Wrap() {}\n")
}

func TestImpactWalksReverseDependenciesWithHops(t *testing.T) {
	root := t.TempDir()
	writeImpactFixture(t, root)
	index, _ := newIndex(t, root, Options{})

	snapshot, err := index.Ensure(t.Context())
	if err != nil || !snapshot.Ready() {
		t.Fatalf("ensure: %v %+v", err, snapshot)
	}
	impacted, truncated, err := index.Impact(t.Context(), []string{"core/core.go"})
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatalf("small fixture reported truncation")
	}
	// mid.go imports core: one hop. app.go imports mid: two hops. The test
	// files reference the exported names: one hop each. The src/mid.rs shadow
	// of Wrap is a declaration, not a dependent of core.
	for path, wantHops := range map[string]int{
		"mid/mid.go":           1,
		"app/app.go":           2,
		"tests/odd_name.py":    1,
		"tests/integration.rs": 2,
	} {
		hit, reached := impacted[path]
		if !reached {
			t.Fatalf("expected %s in the closure, got %#v", path, impacted)
		}
		if hit.Hops != wantHops {
			t.Fatalf("%s hops = %d, want %d", path, hit.Hops, wantHops)
		}
	}
	// A same-named declaration in another language (Wrap lives in both
	// mid/mid.go and src/mid.rs) joins the closure through a reference edge —
	// the documented cost of a lexical graph, carried openly on the edge's
	// kind so a consumer can weigh it.
	if shadow, reached := impacted["src/mid.rs"]; reached {
		if shadow.Via != EdgeReference {
			t.Fatalf("declaration shadow reached by %q, want reference", shadow.Via)
		}
	}
	if _, self := impacted["core/core.go"]; self {
		t.Fatalf("the changed file reached itself")
	}
}

func TestRelatedTestsMergesGraphAndConventionRoutes(t *testing.T) {
	root := t.TempDir()
	writeImpactFixture(t, root)
	// A convention-shaped Go test, which the graph also reaches through its
	// reference of Run.
	writeFile(t, root, "core/core_test.go", "package core\n\nimport \"testing\"\n\nfunc TestRun(t *testing.T) { _ = Run() }\n")
	index, _ := newIndex(t, root, Options{})

	if _, err := index.Ensure(t.Context()); err != nil {
		t.Fatal(err)
	}
	related, snapshot, err := index.RelatedTests(t.Context(), []string{"core/core.go"})
	if err != nil || !snapshot.Ready() {
		t.Fatalf("related: %v %+v", err, snapshot)
	}
	rows := related["core/core.go"]
	byPath := map[string]RelatedTest{}
	for _, row := range rows {
		byPath[row.Path] = row
	}
	// The graph reaches the irregularly named test and the Rust integration;
	// the convention would pair neither with core.go.
	graph, found := byPath["tests/odd_name.py"]
	if !found || graph.Resolution != TestFromGraph || graph.Hops != 1 {
		t.Fatalf("odd_name.py = %#v, want a one-hop graph hit", graph)
	}
	rust, found := byPath["tests/integration.rs"]
	if !found || rust.Resolution != TestFromGraph {
		t.Fatalf("integration.rs = %#v, want a graph hit — Rust has no convention", rust)
	}
	// The convention-shaped test is reached by both routes; the graph's
	// evidence is the more specific claim and must win the duplicate.
	conventional, found := byPath["core/core_test.go"]
	if !found {
		t.Fatalf("core_test.go missing from %#v", rows)
	}
	if conventional.Resolution != TestFromGraph || conventional.Hops != 1 {
		t.Fatalf("core_test.go = %#v, want the graph route to win the duplicate", conventional)
	}
	// Graph hits answer first, closest first.
	for index := 1; index < len(rows); index++ {
		previous, current := rows[index-1], rows[index]
		if previous.Resolution == TestFromGraph && current.Resolution == TestFromGraph {
			if previous.Hops > current.Hops {
				t.Fatalf("graph rows not ordered by hops: %#v", rows)
			}
		}
	}
}

func TestImpactRespectsTheResultBound(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "core/core.go", "package core\n\nfunc Run() error { return nil }\n")
	for index := 0; index < 12; index++ {
		writeFile(t, root, fmt.Sprintf("dep/dep%02d.go", index),
			fmt.Sprintf("package dep\n\nimport \"example.com/app/core\"\n\nfunc Use%02d() { _ = core.Run() }\n", index))
	}
	index, _ := newIndex(t, root, Options{
		Impact: ImpactOptions{MaxDepth: 3, MaxResults: 4},
	})
	if _, err := index.Ensure(t.Context()); err != nil {
		t.Fatal(err)
	}
	impacted, truncated, err := index.Impact(t.Context(), []string{"core/core.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatalf("expected truncation with %d hits under the bound", len(impacted))
	}
	if len(impacted) > 4 {
		t.Fatalf("closure size %d exceeds the bound", len(impacted))
	}
}

func TestRelatedTestsKeepsConventionWhenGraphIsSilent(t *testing.T) {
	root := t.TempDir()
	// No import specifiers and no shared names: the graph has nothing to say,
	// and the convention answers alone.
	writeFile(t, root, "api.go", "package api\n\nfunc Serve() {}\n")
	writeFile(t, root, "api_test.go", "package api\n\nimport \"testing\"\n\nfunc TestServe(t *testing.T) {}\n")
	index, _ := newIndex(t, root, Options{})
	if _, err := index.Ensure(t.Context()); err != nil {
		t.Fatal(err)
	}
	related, _, err := index.RelatedTests(t.Context(), []string{"api.go"})
	if err != nil {
		t.Fatal(err)
	}
	rows := related["api.go"]
	if len(rows) != 1 || rows[0].Path != "api_test.go" ||
		rows[0].Resolution != TestFromConvention {
		t.Fatalf("rows = %#v, want api_test.go by convention", rows)
	}
}
