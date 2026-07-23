package workflow

import "testing"

func TestRender_InputsAndNodes(t *testing.T) {
	inputs := map[string]string{"topic": "go"}
	outputs := map[string]string{"research": "findings"}

	got, err := render("Topic {{ inputs.topic }} => {{ nodes.research.output }}", inputs, outputs)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Topic go => findings" {
		t.Fatalf("unexpected render: %q", got)
	}
}

func TestRender_Errors(t *testing.T) {
	cases := []string{
		"{{ inputs.missing }}",     // unknown input
		"{{ nodes.ghost.output }}", // node not completed
		"{{ nodes.a.value }}",      // unsupported field
		"{{ bogus.path }}",         // unsupported root
	}
	for _, tmpl := range cases {
		if _, err := render(tmpl, map[string]string{}, map[string]string{}); err == nil {
			t.Fatalf("expected error for template %q", tmpl)
		}
	}
}

func TestRender_NoVars(t *testing.T) {
	got, err := render("plain text with $dollar and {braces}", nil, nil)
	if err != nil || got != "plain text with $dollar and {braces}" {
		t.Fatalf("plain text should pass through: %q err=%v", got, err)
	}
}
