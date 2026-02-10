package pipeline

import (
	"testing"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
)

func TestResolveFidelity_EdgeOverridesNode(t *testing.T) {
	edge := &dotparser.Edge{
		From:  "A",
		To:    "B",
		Attrs: []dotparser.Attr{strAttr("fidelity", "full")},
	}
	node := newNode("B", strAttr("fidelity", "truncate"))
	graph := newTestGraph(nil, nil, []dotparser.Attr{strAttr("default_fidelity", "compact")})

	result := ResolveFidelity(edge, node, graph)

	assert.Equal(t, FidelityFull, result)
}

func TestResolveFidelity_NodeOverridesGraphDefault(t *testing.T) {
	node := newNode("B", strAttr("fidelity", "summary:high"))
	graph := newTestGraph(nil, nil, []dotparser.Attr{strAttr("default_fidelity", "truncate")})

	result := ResolveFidelity(nil, node, graph)

	assert.Equal(t, FidelitySummaryHigh, result)
}

func TestResolveFidelity_GraphDefaultUsedWhenNoNodeEdgeFidelity(t *testing.T) {
	node := newNode("B") // no fidelity attribute
	graph := newTestGraph(nil, nil, []dotparser.Attr{strAttr("default_fidelity", "summary:medium")})

	result := ResolveFidelity(nil, node, graph)

	assert.Equal(t, FidelitySummaryMedium, result)
}

func TestResolveFidelity_DefaultIsCompactWhenNothingSet(t *testing.T) {
	node := newNode("B") // no fidelity attribute
	graph := newTestGraph(nil, nil, nil)

	result := ResolveFidelity(nil, node, graph)

	assert.Equal(t, FidelityCompact, result)
}

func TestResolveFidelity_NilParameters(t *testing.T) {
	// Should not panic and return default
	result := ResolveFidelity(nil, nil, nil)
	assert.Equal(t, FidelityCompact, result)
}

func TestResolveFidelity_InvalidModeIgnored(t *testing.T) {
	// Invalid mode on edge should be ignored, fall through to node
	edge := &dotparser.Edge{
		From:  "A",
		To:    "B",
		Attrs: []dotparser.Attr{strAttr("fidelity", "invalid_mode")},
	}
	node := newNode("B", strAttr("fidelity", "full"))
	graph := newTestGraph(nil, nil, nil)

	result := ResolveFidelity(edge, node, graph)

	assert.Equal(t, FidelityFull, result)
}

func TestIsValidFidelity_AcceptsAllValidModes(t *testing.T) {
	validModes := []string{
		"full",
		"truncate",
		"compact",
		"summary:low",
		"summary:medium",
		"summary:high",
	}

	for _, mode := range validModes {
		t.Run(mode, func(t *testing.T) {
			assert.True(t, IsValidFidelity(mode))
		})
	}
}

func TestIsValidFidelity_RejectsInvalidModes(t *testing.T) {
	invalidModes := []string{
		"",
		"invalid",
		"FULL",       // case sensitive
		"summary",    // incomplete
		"summary:",   // no level
		"high",       // not a valid standalone mode
		"compressed", // made up
	}

	for _, mode := range invalidModes {
		t.Run(mode, func(t *testing.T) {
			assert.False(t, IsValidFidelity(mode))
		})
	}
}

func TestResolveThread_NodeThreadIDHighestPrecedence(t *testing.T) {
	edge := &dotparser.Edge{
		From:  "A",
		To:    "B",
		Attrs: []dotparser.Attr{strAttr("thread_id", "edge-thread")},
	}
	node := newNode("B", strAttr("thread_id", "node-thread"))
	graph := newTestGraph(nil, nil, []dotparser.Attr{strAttr("default_thread", "graph-thread")})

	result := ResolveThread(edge, node, graph, "previous-node")

	assert.Equal(t, "node-thread", result)
}

func TestResolveThread_EdgeThreadIDSecondPrecedence(t *testing.T) {
	edge := &dotparser.Edge{
		From:  "A",
		To:    "B",
		Attrs: []dotparser.Attr{strAttr("thread_id", "edge-thread")},
	}
	node := newNode("B") // no thread_id
	graph := newTestGraph(nil, nil, []dotparser.Attr{strAttr("default_thread", "graph-thread")})

	result := ResolveThread(edge, node, graph, "previous-node")

	assert.Equal(t, "edge-thread", result)
}

func TestResolveThread_GraphDefaultThreadThirdPrecedence(t *testing.T) {
	node := newNode("B") // no thread_id
	graph := newTestGraph(nil, nil, []dotparser.Attr{strAttr("default_thread", "graph-thread")})

	result := ResolveThread(nil, node, graph, "previous-node")

	assert.Equal(t, "graph-thread", result)
}

func TestResolveThread_FallbackToPreviousNodeID(t *testing.T) {
	node := newNode("B") // no thread_id
	graph := newTestGraph(nil, nil, nil)

	result := ResolveThread(nil, node, graph, "previous-node")

	assert.Equal(t, "previous-node", result)
}

func TestResolveThread_NilParameters(t *testing.T) {
	// Should return previous node ID when all nil
	result := ResolveThread(nil, nil, nil, "fallback")
	assert.Equal(t, "fallback", result)
}

func TestResolveThread_EmptyThreadIDIgnored(t *testing.T) {
	node := newNode("B", strAttr("thread_id", "")) // empty thread_id
	graph := newTestGraph(nil, nil, []dotparser.Attr{strAttr("default_thread", "graph-thread")})

	result := ResolveThread(nil, node, graph, "previous-node")

	// Empty thread_id should be ignored, fall through to graph default
	assert.Equal(t, "graph-thread", result)
}

func TestFidelityModeConstants(t *testing.T) {
	// Verify the constants have expected values
	assert.Equal(t, FidelityMode("full"), FidelityFull)
	assert.Equal(t, FidelityMode("truncate"), FidelityTruncate)
	assert.Equal(t, FidelityMode("compact"), FidelityCompact)
	assert.Equal(t, FidelityMode("summary:low"), FidelitySummaryLow)
	assert.Equal(t, FidelityMode("summary:medium"), FidelitySummaryMedium)
	assert.Equal(t, FidelityMode("summary:high"), FidelitySummaryHigh)
}

func TestDefaultFidelityIsCompact(t *testing.T) {
	assert.Equal(t, FidelityCompact, DefaultFidelity)
}
