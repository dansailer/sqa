package output

import (
	"bytes"
	"testing"
)

type row struct {
	Rank int    `json:"rank"`
	Key  string `json:"key"`
	Msg  string `json:"message"`
	New  bool   `json:"new_code"`
}

func TestEncodeTOONTabular(t *testing.T) {
	v := map[string]any{"findings": []row{{1, "AX1", "Use a, safe call", true}, {2, "AX2", "plain", false}}}
	got, err := EncodeTOON(v)
	if err != nil {
		t.Fatal(err)
	}
	want := "findings[2]{rank,key,message,new_code}:\n  1,AX1,\"Use a, safe call\",true\n  2,AX2,plain,false\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEncodeTOONNested(t *testing.T) {
	type inner struct {
		A string   `json:"a"`
		T []string `json:"tags"`
	}
	type outer struct {
		Key   string  `json:"key"`
		In    inner   `json:"inner"`
		Empty []int   `json:"empty"`
		Mixed []inner `json:"mixed"`
	}
	got, err := EncodeTOON(outer{Key: "123", In: inner{A: "x: y", T: []string{"a", "b"}}, Empty: []int{}, Mixed: []inner{{A: "q", T: []string{"z"}}}})
	if err != nil {
		t.Fatal(err)
	}
	want := "key: \"123\"\ninner:\n  a: \"x: y\"\n  tags[2]: a,b\nempty[0]:\nmixed[1]:\n  - a: q\n    tags[1]: z\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestResolveDefaultsToTOONWhenPiped(t *testing.T) {
	f, err := Resolve("", &bytes.Buffer{})
	if err != nil || f != FormatTOON {
		t.Fatalf("%s %v", f, err)
	}
	if _, err := Resolve("xml", &bytes.Buffer{}); err == nil {
		t.Fatal("expected error")
	}
}
