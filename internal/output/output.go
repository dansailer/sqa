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

// Renderable values provide human formats; json/toon use the value itself.
type Renderable interface {
	Markdown() string
	Table() string
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
		r, ok := v.(Renderable)
		if !ok {
			t, err := EncodeTOON(v)
			if err != nil {
				return err
			}
			s = t
			break
		}
		if format == FormatMD {
			s = r.Markdown()
		} else {
			s = r.Table()
		}
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
