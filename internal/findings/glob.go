package findings

import (
	"regexp"
	"strings"
)

// IsGlob reports whether a --file value is a glob pattern rather than a path.
func IsGlob(s string) bool { return strings.ContainsAny(s, "*?[") }

// CompileGlob turns a path glob into a matcher for project-relative paths:
// `*` and `?` stay within one path segment, `**` crosses directories
// (`test/**` matches everything below test/, `**/*_test.go` any test file),
// and `[...]` is a character class.
func CompileGlob(pattern string) (func(string) bool, error) {
	p := strings.TrimPrefix(pattern, "./")
	var re strings.Builder
	re.WriteString("^")
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '*' && i+1 < len(p) && p[i+1] == '*':
			i++
			if i+1 < len(p) && p[i+1] == '/' {
				i++
				re.WriteString("(?:.*/)?")
			} else {
				re.WriteString(".*")
			}
		case c == '*':
			re.WriteString("[^/]*")
		case c == '?':
			re.WriteString("[^/]")
		case c == '[':
			j := strings.IndexByte(p[i:], ']')
			if j < 0 {
				re.WriteString(regexp.QuoteMeta(string(c)))
				continue
			}
			class := p[i+1 : i+j]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			re.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += j
		default:
			re.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	re.WriteString("$")
	rx, err := regexp.Compile(re.String())
	if err != nil {
		return nil, err
	}
	return rx.MatchString, nil
}
