package pipeline

import (
	"github.com/martinemde/attractor/dotparser"
)

// Choice represents a human-selectable option derived from an outgoing edge.
type Choice struct {
	Key   string // Accelerator key (e.g., "Y", "N")
	Label string // Display label
	To    string // Target node ID
}

// WaitForHumanHandler blocks pipeline execution until a human selects an option
// derived from the node's outgoing edges. This implements the human-in-the-loop
// pattern described in Section 4.6 of the spec.
type WaitForHumanHandler struct {
	Interviewer Interviewer
}

// NewWaitForHumanHandler creates a new WaitForHumanHandler with the given interviewer.
func NewWaitForHumanHandler(interviewer Interviewer) *WaitForHumanHandler {
	return &WaitForHumanHandler{Interviewer: interviewer}
}

// Execute presents choices from outgoing edges to the human and routes based on selection.
func (h *WaitForHumanHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	// 1. Derive choices from outgoing edges
	edges := graph.EdgesFrom(node.ID)
	choices := make([]Choice, 0, len(edges))

	for _, edge := range edges {
		label := edge.To // Default to target node ID
		if labelAttr, ok := edge.Attr("label"); ok && labelAttr.Str != "" {
			label = labelAttr.Str
		}
		key := ParseAcceleratorKey(label)
		choices = append(choices, Choice{
			Key:   key,
			Label: label,
			To:    edge.To,
		})
	}

	if len(choices) == 0 {
		return Fail("No outgoing edges for human gate"), nil
	}

	// 2. Build question from choices
	options := make([]Option, len(choices))
	for i, c := range choices {
		options[i] = Option{
			Key:   c.Key,
			Label: c.Label,
		}
	}

	// Get question text from node label or use default
	questionText := "Select an option:"
	if labelAttr, ok := node.Attr("label"); ok && labelAttr.Str != "" {
		questionText = labelAttr.Str
	}

	// Check for timeout configuration
	var timeoutSeconds float64
	if timeoutAttr, ok := node.Attr("human.timeout"); ok {
		timeoutSeconds = timeoutAttr.Float
	}

	question := &Question{
		Text:           questionText,
		Type:           QuestionMultipleChoice,
		Options:        options,
		TimeoutSeconds: timeoutSeconds,
		Stage:          node.ID,
	}

	// 3. Present to interviewer and wait for answer
	if h.Interviewer == nil {
		return Fail("No interviewer configured for human gate"), nil
	}

	answer, err := h.Interviewer.Ask(question)
	if err != nil {
		return Fail("interviewer error: " + err.Error()), nil
	}

	// 4. Handle timeout
	if answer.Timeout {
		// Check for default choice
		if defaultChoice, ok := node.Attr("human.default_choice"); ok && defaultChoice.Str != "" {
			// Find the choice matching the default
			selected := findChoiceByTarget(choices, defaultChoice.Str)
			if selected != nil {
				return h.buildSuccessOutcome(selected), nil
			}
			// If default doesn't match any target, try matching by key
			selected = findChoiceByKey(choices, defaultChoice.Str)
			if selected != nil {
				return h.buildSuccessOutcome(selected), nil
			}
		}
		// No default or default not found
		return Retry("human gate timeout, no default"), nil
	}

	// 5. Handle skipped
	if answer.Skipped {
		return Fail("human skipped interaction"), nil
	}

	// 6. Find matching choice
	var selected *Choice

	// First try to match by selected option
	if answer.SelectedOption != nil {
		selected = findChoiceByKey(choices, answer.SelectedOption.Key)
	}

	// Then try to match by value
	if selected == nil && answer.Value != "" {
		selected = findChoiceByKey(choices, answer.Value)
		if selected == nil {
			// Try matching by normalized label
			selected = findChoiceByLabel(choices, answer.Value)
		}
		if selected == nil {
			// Try matching by target node ID
			selected = findChoiceByTarget(choices, answer.Value)
		}
	}

	// Fallback to first choice
	if selected == nil {
		selected = &choices[0]
	}

	return h.buildSuccessOutcome(selected), nil
}

// buildSuccessOutcome creates a success outcome for the selected choice.
func (h *WaitForHumanHandler) buildSuccessOutcome(selected *Choice) *Outcome {
	return Success().
		WithSuggestedNextIDs(selected.To).
		WithContextUpdate("human.gate.selected", selected.Key).
		WithContextUpdate("human.gate.label", selected.Label)
}

// findChoiceByKey finds a choice by its accelerator key (case-insensitive).
func findChoiceByKey(choices []Choice, key string) *Choice {
	normalizedKey := normalizeLabel(key)
	for i := range choices {
		if normalizeLabel(choices[i].Key) == normalizedKey {
			return &choices[i]
		}
	}
	return nil
}

// findChoiceByLabel finds a choice by its label (normalized comparison).
func findChoiceByLabel(choices []Choice, label string) *Choice {
	normalizedLabel := normalizeLabel(label)
	for i := range choices {
		if normalizeLabel(choices[i].Label) == normalizedLabel {
			return &choices[i]
		}
	}
	return nil
}

// findChoiceByTarget finds a choice by its target node ID.
func findChoiceByTarget(choices []Choice, target string) *Choice {
	for i := range choices {
		if choices[i].To == target {
			return &choices[i]
		}
	}
	return nil
}
