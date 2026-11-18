package output

import (
	"strings"
	"testing"
)

type onlyTable struct{}

func (onlyTable) Table() string { return "T" }

type onlyMD struct{}

func (onlyMD) Markdown() string { return "M" }

type both struct{}

func (both) Table() string    { return "T" }
func (both) Markdown() string { return "M" }

func TestHumanFormatsFallBackToTheOtherMethod(t *testing.T) {
	for _, c := range []struct {
		v         any
		md, table string
	}{{onlyTable{}, "T\n", "T\n"}, {onlyMD{}, "M\n", "M\n"}, {both{}, "M\n", "T\n"}} {
		for format, want := range map[string]string{FormatMD: c.md, FormatTable: c.table} {
			var b strings.Builder
			if err := Write(&b, format, c.v); err != nil || b.String() != want {
				t.Errorf("%T %s: %q %v, want %q", c.v, format, b.String(), err, want)
			}
		}
	}
}
