package dotparser

// Position tracks a source location for error messages.
type Position struct {
	Line   int // 1-based line number
	Column int // 1-based column number
	Offset int // 0-based byte offset into source
}

// ValueKind discriminates the Value tagged union.
type ValueKind string

const (
	ValueString ValueKind = "string"
	ValueInt    ValueKind = "int"
	ValueFloat  ValueKind = "float"
	ValueBool   ValueKind = "bool"
)

// Value is a parsed attribute value. Kind determines which typed field is populated.
type Value struct {
	Kind  ValueKind
	Str   string  // populated when Kind == ValueString
	Int   int64   // populated when Kind == ValueInt
	Float float64 // populated when Kind == ValueFloat
	Bool  bool    // populated when Kind == ValueBool
	Raw   string  // original text representation, always set
}

// String returns the original text representation of the value.
func (v Value) String() string { return v.Raw }

// Attr is a key=value pair from an attribute block.
type Attr struct {
	Key   string // plain identifier or qualified (e.g. "stack.child_dotfile")
	Value Value
	Pos   Position
}

// Node represents a parsed node declaration.
type Node struct {
	ID    string
	Attrs []Attr
	Pos   Position
}

// Attr looks up a node attribute by key. Returns the value and true if found.
func (n *Node) Attr(key string) (Value, bool) {
	for i := len(n.Attrs) - 1; i >= 0; i-- {
		if n.Attrs[i].Key == key {
			return n.Attrs[i].Value, true
		}
	}
	return Value{}, false
}

// Edge represents a single directed edge from one node to another.
type Edge struct {
	From  string
	To    string
	Attrs []Attr
	Pos   Position
}

// Attr looks up an edge attribute by key. Returns the value and true if found.
func (e *Edge) Attr(key string) (Value, bool) {
	for i := len(e.Attrs) - 1; i >= 0; i-- {
		if e.Attrs[i].Key == key {
			return e.Attrs[i].Value, true
		}
	}
	return Value{}, false
}

// Graph is the complete parsed representation of a DOT digraph.
type Graph struct {
	Name       string   // the identifier after "digraph"
	GraphAttrs []Attr   // from graph [...] and top-level key=value declarations
	Nodes      []*Node  // all nodes in declaration order
	Edges      []*Edge  // all edges (chained edges expanded)
}

// NodeByID returns the node with the given ID, or nil if not found.
func (g *Graph) NodeByID(id string) *Node {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

// EdgesFrom returns all edges originating from the given node ID.
func (g *Graph) EdgesFrom(id string) []*Edge {
	var result []*Edge
	for _, e := range g.Edges {
		if e.From == id {
			result = append(result, e)
		}
	}
	return result
}

// EdgesTo returns all edges targeting the given node ID.
func (g *Graph) EdgesTo(id string) []*Edge {
	var result []*Edge
	for _, e := range g.Edges {
		if e.To == id {
			result = append(result, e)
		}
	}
	return result
}

// GraphAttr looks up a graph-level attribute by key. Returns the value and true if found.
func (g *Graph) GraphAttr(key string) (Value, bool) {
	for i := len(g.GraphAttrs) - 1; i >= 0; i-- {
		if g.GraphAttrs[i].Key == key {
			return g.GraphAttrs[i].Value, true
		}
	}
	return Value{}, false
}
