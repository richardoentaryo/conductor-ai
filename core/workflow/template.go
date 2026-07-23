package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

// varRef matches a template variable: {{ inputs.name }} or {{ nodes.id.output }}.
// Whitespace inside the braces is optional; the captured group is the path.
var varRef = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.]+)\s*\}\}`)

// render substitutes template variables in s. Supported references:
//
//	{{ inputs.<name> }}       -> a workflow input value
//	{{ nodes.<id>.output }}   -> the text output of a completed upstream node
//
// An unknown variable, an unsupported reference shape, or a reference to a node
// that has not produced output yet is an error — failing loudly beats silently
// injecting an empty string into a prompt.
func render(s string, inputs map[string]string, outputs map[string]string) (string, error) {
	var firstErr error
	out := varRef.ReplaceAllStringFunc(s, func(match string) string {
		path := varRef.FindStringSubmatch(match)[1]
		val, err := resolve(path, inputs, outputs)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return val
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

// resolve looks up a single dotted reference path.
func resolve(path string, inputs, outputs map[string]string) (string, error) {
	parts := strings.Split(path, ".")
	switch {
	case len(parts) == 2 && parts[0] == "inputs":
		v, ok := inputs[parts[1]]
		if !ok {
			return "", fmt.Errorf("unknown input %q", parts[1])
		}
		return v, nil
	case len(parts) == 3 && parts[0] == "nodes" && parts[2] == "output":
		v, ok := outputs[parts[1]]
		if !ok {
			return "", fmt.Errorf("reference to node %q output before it completed", parts[1])
		}
		return v, nil
	default:
		return "", fmt.Errorf("unsupported template reference %q (use inputs.x or nodes.id.output)", path)
	}
}
