package repoindex

import (
	"context"
	"sort"
	"strings"
)

// Impact analysis walks the reference graph backwards from a change: a file is
// affected when it depends on a changed file, transitively, within a bounded
// number of hops. The walk answers "what else must be looked at" — callers,
// callers of callers, and the tests that exercise the change — which naming
// conventions cannot, because a test that imports a source file is a reference
// edge no file name reveals.

// ImpactHit is one file the walk reached.
type ImpactHit struct {
	Path string
	// Hops is the distance from the nearest changed path: 1 is a direct
	// dependent, 2 depends on that one, and so on.
	Hops int
	// Via is the kind of the last edge the walk followed — an import or a
	// reference — reported so a consumer can weigh a structural edge above a
	// name coincidence.
	Via string
}

// ImpactOptions bound the reverse walk.
type ImpactOptions struct {
	// MaxDepth is how many dependency hops a change may reach through.
	MaxDepth int
	// MaxResults bounds how many files one answer may name, so a change in a
	// repository's root package does not return the repository.
	MaxResults int
}

// Defaults for impact options left unset. Three hops covers the direct
// dependent, its dependents and one more layer — beyond that, a lexical graph's
// name-coincidence error accumulates faster than its recall improves.
const (
	DefaultImpactMaxDepth   = 3
	DefaultImpactMaxResults = 200
)

func (o ImpactOptions) withDefaults() ImpactOptions {
	if o.MaxDepth <= 0 {
		o.MaxDepth = DefaultImpactMaxDepth
	}
	if o.MaxResults <= 0 {
		o.MaxResults = DefaultImpactMaxResults
	}
	return o
}

// Impact returns the files that depend on the given paths, transitively, with
// the hop count and last edge kind of the shortest walk to each. The second
// result reports whether the answer was cut short by the result bound: a
// truncated closure is still a closure's beginning, and the caller decides
// whether it is enough.
func (i *Index) Impact(ctx context.Context, paths []string) (map[string]ImpactHit, bool, error) {
	edges, err := i.store.Edges(ctx)
	if err != nil {
		return nil, false, err
	}
	reverse := make(map[string][]graphEdge, len(edges))
	for _, edge := range edges {
		reverse[edge.Dst] = append(reverse[edge.Dst], edge)
	}
	options := i.options.Impact.withDefaults()

	found := make(map[string]ImpactHit, len(edges))
	truncated := false
	// A plain queue in breadth order is what makes Hops the shortest walk:
	// every file is first reached through as few edges as possible.
	type pending struct {
		path string
		hops int
		via  string
	}
	queue := make([]pending, 0, len(paths))
	roots := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		roots[path] = struct{}{}
	}
	for _, path := range paths {
		queue = append(queue, pending{path: path})
	}
	for head := 0; head < len(queue); head++ {
		current := queue[head]
		for _, edge := range reverse[current.path] {
			if _, isRoot := roots[edge.Src]; isRoot {
				continue
			}
			if _, reached := found[edge.Src]; reached {
				continue
			}
			if len(found) >= options.MaxResults {
				truncated = true
				continue
			}
			hit := ImpactHit{Path: edge.Src, Hops: current.hops + 1, Via: edge.Kind}
			found[edge.Src] = hit
			if hit.Hops < options.MaxDepth {
				queue = append(queue, pending{path: edge.Src, hops: hit.Hops, via: edge.Kind})
			}
		}
	}
	// The map is the answer; determinism of iteration is the caller's to pick
	// up by sorting, which every consumer that prints does.
	return found, truncated, nil
}

// RelatedTest is one test file that covers a source path, with how it was
// found. The graph route reaches tests naming conventions miss — a test file
// that imports the source is a reference edge regardless of what it is called —
// and the convention route remains for repositories and languages whose files
// the graph does not yet connect.
type RelatedTest struct {
	Path string `json:"path"`
	// Hops is the graph distance from the source; zero when the convention
	// route found the test.
	Hops int `json:"hops,omitempty"`
	// Via is the last edge kind of the walk that reached the test.
	Via string `json:"via,omitempty"`
	// Resolution says which route found the test: "graph" or "convention".
	Resolution string `json:"resolution"`
}

// Resolutions a related test can carry.
const (
	TestFromGraph      = "graph"
	TestFromConvention = "convention"
)

// RelatedTests maps each source path to the test files that cover it. Two
// routes answer together: the reverse walk over the reference graph finds
// every indexed test file that depends on the source — the naming convention
// finds the test a project names after it — and a test both routes reach keeps
// the graph's evidence, which is the more specific claim.
//
// Paths that are themselves tests map to themselves. A language the convention
// table does not know can still produce graph rows, which is how Rust gains
// affected-test answers for the first time.
func (i *Index) RelatedTests(
	ctx context.Context, paths []string,
) (map[string][]RelatedTest, Snapshot, error) {
	files, snapshot, err := i.Files(ctx)
	if err != nil || !snapshot.Ready() {
		return nil, snapshot, err
	}
	indexed := make(map[string]struct{}, len(files))
	directories := make(map[string][]string)
	for path := range files {
		indexed[path] = struct{}{}
		directory, name := splitPath(path)
		directories[directory] = append(directories[directory], name)
	}
	impacted, _, err := i.Impact(ctx, paths)
	if err != nil {
		// A graph that cannot answer is not a reason to stop answering: the
		// convention route stands on its own, and the rows say which route
		// spoke.
		impacted = nil
	}

	related := make(map[string][]RelatedTest, len(paths))
	for _, path := range paths {
		matches := relatedTestRows(path, indexed, directories, impacted)
		if matches == nil {
			continue
		}
		related[path] = matches
	}
	return related, snapshot, nil
}

// relatedTestRows merges the two routes for one source path: graph hits first
// (with their evidence), then convention hits the graph did not reach. A
// convention miss the graph also missed leaves the path absent from the
// result, which is how a caller tells "no tests" from "cannot tell".
func relatedTestRows(
	path string,
	indexed map[string]struct{},
	directories map[string][]string,
	impacted map[string]ImpactHit,
) []RelatedTest {
	var rows []RelatedTest
	seen := make(map[string]struct{})
	if impacted != nil {
		// Deterministic order over the walk's results: hops then path, so the
		// closest tests answer first. The graph route recognizes a test by
		// where it lives as well as what it is called — the whole point of
		// the route is reaching tests whose names no convention pairs —
		// which admits a helper beside the tests as the price of the recall.
		var hits []ImpactHit
		for _, hit := range impacted {
			if _, isIndexed := indexed[hit.Path]; !isIndexed {
				continue
			}
			if looksLikeTest(hit.Path) {
				hits = append(hits, hit)
			}
		}
		sort.Slice(hits, func(x, y int) bool {
			if hits[x].Hops != hits[y].Hops {
				return hits[x].Hops < hits[y].Hops
			}
			return hits[x].Path < hits[y].Path
		})
		for _, hit := range hits {
			rows = append(rows, RelatedTest{
				Path: hit.Path, Hops: hit.Hops, Via: hit.Via,
				Resolution: TestFromGraph,
			})
			seen[hit.Path] = struct{}{}
		}
	}
	if IsTestPath(path) {
		if _, found := indexed[path]; found {
			if _, duplicated := seen[path]; !duplicated {
				rows = append(rows, RelatedTest{Path: path, Resolution: TestFromGraph})
				seen[path] = struct{}{}
			}
		}
		return rows
	}
	for _, candidate := range relatedTests(path, indexed, directories) {
		if _, duplicated := seen[candidate]; duplicated {
			continue
		}
		rows = append(rows, RelatedTest{Path: candidate, Resolution: TestFromConvention})
		seen[candidate] = struct{}{}
	}
	// An empty answer and an absent one say different things: a path the
	// convention table knows maps to an empty row ("no tests"), a path no
	// route can speak about stays nil ("cannot tell").
	if rows == nil && Convention(path) {
		return []RelatedTest{}
	}
	return rows
}

// looksLikeTest reports whether the graph route should offer a file as a test.
// The naming conventions of IsTestPath answer for the convention route; the
// graph route also claims a file that lives where a project keeps its tests,
// because reaching a test no naming convention would pair with is exactly what
// the route exists for.
func looksLikeTest(path string) bool {
	if IsTestPath(path) {
		return true
	}
	for _, segment := range strings.Split(path, "/") {
		switch segment {
		case "tests", "test", "__tests__", "spec":
			return true
		}
	}
	return false
}
