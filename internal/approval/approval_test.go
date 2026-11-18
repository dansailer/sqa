package approval

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheck(t *testing.T) {
	cases := []struct {
		approved bool
		comment  string
		ok       bool
	}{
		{false, "", false},
		{true, "", false},
		{true, "   ", false},
		{false, "reason", false},
		{true, "reason", true},
	}
	for _, c := range cases {
		err := Check(c.approved, c.comment)
		if c.ok != (err == nil) {
			t.Errorf("Check(%v,%q) = %v", c.approved, c.comment, err)
		}
		if err != nil && !errors.Is(err, ErrMissing) {
			t.Errorf("want ErrMissing, got %v", err)
		}
	}
}

func TestServerComment(t *testing.T) {
	if got := ServerComment(" test only code "); got != "[sqa] test only code" {
		t.Fatal(got)
	}
	if got := ServerComment("[sqa] x"); got != "[sqa] x" {
		t.Fatal(got)
	}
}

func TestAppend(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state", "audit.jsonl")
	for _, k := range []string{"A", "B"} {
		if err := Append(p, NewEntry("issue", k, "fp", "c")); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	if st.Mode().Perm() != 0o600 {
		t.Errorf("perm %v", st.Mode().Perm())
	}
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 2 {
		t.Fatalf("lines %d", n)
	}
}

func TestCheckRejectsPrefixOnlyComment(t *testing.T) {
	for _, c := range []string{"[sqa]", " [sqa]  ", "[sqa] "} {
		if err := Check(true, c); !errors.Is(err, ErrMissing) {
			t.Errorf("%q: want ErrMissing, got %v", c, err)
		}
	}
	if err := Check(true, "[sqa] test fixture, not shipped"); err != nil {
		t.Error(err)
	}
}
