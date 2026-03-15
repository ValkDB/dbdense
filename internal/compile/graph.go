package compile

import (
	"sort"

	"github.com/valkdb/dbdense/pkg/schema"
)

// Graph is an in-memory adjacency list built from the given FK edges.
type Graph struct {
	// adjacency maps entity name -> set of neighbor entity names.
	adjacency map[string]map[string]bool
}

// BuildGraph constructs a Graph from the given FK edges.
func BuildGraph(edges []schema.Edge) *Graph {
	g := &Graph{
		adjacency: make(map[string]map[string]bool, len(edges)),
	}
	for _, e := range edges {
		g.addNeighbor(e.FromEntity, e.ToEntity)
		g.addNeighbor(e.ToEntity, e.FromEntity)
	}
	return g
}

// addNeighbor inserts a directed adjacency edge from -> to.
func (g *Graph) addNeighbor(from, to string) {
	if g.adjacency[from] == nil {
		g.adjacency[from] = make(map[string]bool, 4)
	}
	g.adjacency[from][to] = true
}

// Neighbors returns the sorted list of entity names adjacent to the given
// entity in the FK graph.
func (g *Graph) Neighbors(entity string) []string {
	adj := g.adjacency[entity]
	if len(adj) == 0 {
		return nil
	}
	out := make([]string, 0, len(adj))
	for n := range adj {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
