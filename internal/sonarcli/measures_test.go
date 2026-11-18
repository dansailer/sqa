package sonarcli

import (
	"encoding/json"
	"testing"
)

func TestMeasureValueSources(t *testing.T) {
	var rc rawComponent
	if err := json.Unmarshal([]byte(`{"key":"demo","qualifier":"TRK","measures":[
		{"metric":"coverage","value":"71.3"},
		{"metric":"new_coverage","period":{"index":1,"value":"62.5"}},
		{"metric":"new_lines_to_cover","periods":[{"index":1,"value":"40"}]},
		{"metric":"new_uncovered_lines","period":{"index":1}},
		{"metric":"uncovered_lines","value":"n/a"}
	]}`), &rc); err != nil {
		t.Fatal(err)
	}
	c := rc.parse()
	if c.Measures["coverage"] != 71.3 || c.Measures["new_coverage"] != 62.5 || c.Measures["new_lines_to_cover"] != 40 {
		t.Errorf("measures: %v", c.Measures)
	}
	if c.Has("new_uncovered_lines") || c.Has("uncovered_lines") || c.Has("lines_to_cover") {
		t.Errorf("missing or non-numeric values must be absent: %v", c.Measures)
	}
}
