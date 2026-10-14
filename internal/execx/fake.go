package execx

import (
	"context"
	"strings"
)

// Fake records commands and answers them from a table keyed by the full
// command line. Unknown commands succeed with empty output.
type Fake struct {
	Calls     []string
	Responses map[string]FakeResponse
	// Match, when set, answers commands by prefix before Responses.
	Match func(cmdline string) (FakeResponse, bool)
}

// FakeResponse is a canned result.
type FakeResponse struct {
	Out []byte
	Err error
}

// Run implements Runner.
func (f *Fake) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	f.Calls = append(f.Calls, line)
	if f.Match != nil {
		if r, ok := f.Match(line); ok {
			return r.Out, r.Err
		}
	}
	if r, ok := f.Responses[line]; ok {
		return r.Out, r.Err
	}
	return nil, nil
}
