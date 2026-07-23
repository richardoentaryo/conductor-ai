package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/conductor-ai/conductor/core/ports"
)

// LoadDir reads every *.yaml / *.yml file in dir as a single workflow definition
// and returns them sorted by filename for deterministic ordering. A missing dir
// yields no workflows (not an error), so an unset workflows dir simply disables
// the feature. Definitions are NOT validated here — NewService validates them.
func LoadDir(dir string) ([]ports.Workflow, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("workflows: read dir %q: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext := strings.ToLower(filepath.Ext(e.Name())); ext == ".yaml" || ext == ".yml" {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	var out []ports.Workflow
	for _, name := range files {
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("workflows: read %q: %w", path, err)
		}
		var wf ports.Workflow
		dec := yaml.NewDecoder(strings.NewReader(string(raw)))
		dec.KnownFields(true) // reject unknown keys — catches typos in workflow files
		if err := dec.Decode(&wf); err != nil {
			return nil, fmt.Errorf("workflows: parse %q: %w", path, err)
		}
		out = append(out, wf)
	}
	return out, nil
}
