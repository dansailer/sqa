package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// node is an order-preserving JSON value.
type node struct {
	kind  byte // 'o' object, 'a' array, 's' string, 'n' number, 'b' bool, 'z' null
	keys  []string
	vals  []*node
	items []*node
	str   string
}

func parseOrdered(b []byte) (*node, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	return parseValue(dec)
}

func parseValue(dec *json.Decoder) (*node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			n := &node{kind: 'o'}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				n.keys = append(n.keys, k)
				n.vals = append(n.vals, v)
			}
			_, err := dec.Token()
			return n, err
		case '[':
			n := &node{kind: 'a'}
			for dec.More() {
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				n.items = append(n.items, v)
			}
			_, err := dec.Token()
			return n, err
		}
	case string:
		return &node{kind: 's', str: t}, nil
	case json.Number:
		return &node{kind: 'n', str: t.String()}, nil
	case bool:
		return &node{kind: 'b', str: strconv.FormatBool(t)}, nil
	case nil:
		return &node{kind: 'z', str: "null"}, nil
	}
	return nil, fmt.Errorf("unexpected token %v", tok)
}

// EncodeTOON renders v (anything JSON-marshalable) as TOON
// (Token-Oriented Object Notation), a compact, LLM-friendly format.
func EncodeTOON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	n, err := parseOrdered(b)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	switch n.kind {
	case 'o':
		writeObject(&sb, n, 0)
	case 'a':
		writeArray(&sb, "", n, 0)
	default:
		sb.WriteString(scalar(n))
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

func indent(sb *strings.Builder, depth int) { sb.WriteString(strings.Repeat("  ", depth)) }

func writeObject(sb *strings.Builder, n *node, depth int) {
	for i, k := range n.keys {
		writeField(sb, k, n.vals[i], depth)
	}
}

func writeField(sb *strings.Builder, k string, v *node, depth int) {
	switch v.kind {
	case 'o':
		indent(sb, depth)
		sb.WriteString(quoteKey(k) + ":\n")
		writeObject(sb, v, depth+1)
	case 'a':
		writeArray(sb, quoteKey(k), v, depth)
	default:
		indent(sb, depth)
		sb.WriteString(quoteKey(k) + ": " + scalar(v) + "\n")
	}
}

func isPrimitive(n *node) bool { return n.kind != 'o' && n.kind != 'a' }

// tabularFields returns the shared field list when every item is an object
// with the same keys in the same order and only primitive values.
func tabularFields(items []*node) []string {
	if len(items) == 0 || items[0].kind != 'o' || len(items[0].keys) == 0 {
		return nil
	}
	keys := items[0].keys
	for _, it := range items {
		if it.kind != 'o' || len(it.keys) != len(keys) {
			return nil
		}
		for i, k := range it.keys {
			if k != keys[i] || !isPrimitive(it.vals[i]) {
				return nil
			}
		}
	}
	return keys
}

func writeArray(sb *strings.Builder, key string, n *node, depth int) {
	indent(sb, depth)
	header := fmt.Sprintf("%s[%d]", key, len(n.items))
	allPrim := true
	for _, it := range n.items {
		if !isPrimitive(it) {
			allPrim = false
			break
		}
	}
	switch {
	case len(n.items) == 0:
		sb.WriteString(header + ":\n")
	case allPrim:
		vals := make([]string, len(n.items))
		for i, it := range n.items {
			vals[i] = scalar(it)
		}
		sb.WriteString(header + ": " + strings.Join(vals, ",") + "\n")
	default:
		if fields := tabularFields(n.items); fields != nil {
			qf := make([]string, len(fields))
			for i, f := range fields {
				qf[i] = quoteKey(f)
			}
			sb.WriteString(header + "{" + strings.Join(qf, ",") + "}:\n")
			for _, it := range n.items {
				vals := make([]string, len(it.vals))
				for i, v := range it.vals {
					vals[i] = scalar(v)
				}
				indent(sb, depth+1)
				sb.WriteString(strings.Join(vals, ",") + "\n")
			}
			return
		}
		sb.WriteString(header + ":\n")
		for _, it := range n.items {
			writeListItem(sb, it, depth+1)
		}
	}
}

func writeListItem(sb *strings.Builder, it *node, depth int) {
	switch it.kind {
	case 'o':
		if len(it.keys) == 0 {
			indent(sb, depth)
			sb.WriteString("-\n")
			return
		}
		// First field on the dash line, the rest indented under it.
		var first strings.Builder
		writeField(&first, it.keys[0], it.vals[0], depth+1)
		s := first.String()
		indent(sb, depth)
		sb.WriteString("- " + strings.TrimLeft(s, " "))
		for i := 1; i < len(it.keys); i++ {
			writeField(sb, it.keys[i], it.vals[i], depth+1)
		}
	case 'a':
		var inner strings.Builder
		writeArray(&inner, "", it, depth+1)
		indent(sb, depth)
		sb.WriteString("- " + strings.TrimLeft(inner.String(), " "))
	default:
		indent(sb, depth)
		sb.WriteString("- " + scalar(it) + "\n")
	}
}

var (
	reSafeKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)
	reNumber  = regexp.MustCompile(`^-?\d+(\.\d+)?([eE][+-]?\d+)?$|^0\d+$`)
)

func quoteKey(k string) string {
	if reSafeKey.MatchString(k) {
		return k
	}
	return quote(k)
}

func scalar(n *node) string {
	if n.kind != 's' {
		return n.str
	}
	s := n.str
	if needsQuote(s) {
		return quote(s)
	}
	return s
}

func needsQuote(s string) bool {
	if s == "" || s != strings.TrimSpace(s) {
		return true
	}
	switch s {
	case "true", "false", "null":
		return true
	}
	if reNumber.MatchString(s) || strings.HasPrefix(s, "-") {
		return true
	}
	return strings.ContainsAny(s, ":\",\\[]{}#\n\r\t")
}

func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + r.Replace(s) + `"`
}
