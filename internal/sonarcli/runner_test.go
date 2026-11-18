package sonarcli

import "testing"

func TestErrorHTTPStatus(t *testing.T) {
	for stderr, want := range map[string]int{
		"SonarQube API error: 404 - {\"errors\":[]}":        404,
		"SonarQube API error: 500 - timed out after 404 ms": 500,
		"Could not determine project key.":                  0,
	} {
		if got := (&Error{Stderr: stderr}).HTTPStatus(); got != want {
			t.Errorf("%q: %d, want %d", stderr, got, want)
		}
	}
}
