// Package output renders command results as toon, json, md or table.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Formats supported by every command.
const (
	FormatTOON  = "toon"
	FormatJSON  = "json"
	FormatMD    = "md"
	FormatTable = "table"
)

// A value with a Table or Markdown method renders md and table itself; one
// that has only one of them uses it for both. Anything else is printed as
// TOON. json and toon always use the value itself.
type tabler interface{ Table() string }
type markdowner interface{ Markdown() string }

// human renders v for md or table, or reports false.
func human(format string, v any) (string, bool) {
	t, hasT := v.(tabler)
	m, hasM := v.(markdowner)
	switch {
	case hasM && (format == FormatMD || !hasT):
		return m.Markdown(), true
	case hasT:
		return t.Table(), true
	}
	return "", false
}

// Resolve picks the format: an explicit flag wins, otherwise table on a TTY
// and toon when piped (agents).
func Resolve(flag string, w io.Writer) (string, error) {
	switch strings.ToLower(flag) {
	case "":
		if f, ok := w.(*os.File); ok && term.IsTerminal(int(f.Fd())) { //nolint:gosec // fd fits int
			return FormatTable, nil
		}
		return FormatTOON, nil
	case FormatTOON, FormatJSON, FormatMD, FormatTable:
		return strings.ToLower(flag), nil
	}
	return "", fmt.Errorf("unknown --format %q (toon, json, md, table)", flag)
}

// Write renders v in format to w.
func Write(w io.Writer, format string, v any) error {
	var s string
	switch format {
	case FormatJSON:
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		s = string(b) + "\n"
	case FormatTOON:
		t, err := EncodeTOON(v)
		if err != nil {
			return err
		}
		s = t
	case FormatMD, FormatTable:
		h, ok := human(format, v)
		if !ok {
			t, err := EncodeTOON(v)
			if err != nil {
				return err
			}
			s = t
			break
		}
		s = h
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
	default:
		return fmt.Errorf("unknown format %q", format)
	}
	_, err := io.WriteString(w, s)
	return err
}

// MDEscape escapes pipes for markdown table cells.
func MDEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")
}
