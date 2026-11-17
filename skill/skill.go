// Package skill embeds the harness-neutral SKILL.md shipped with sqa.
package skill

import _ "embed"

// Content is the SKILL.md that `sqa skill print|install` writes.
//
//go:embed SKILL.md
var Content []byte
