package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAcknowledgedHotspotsStayVisible(t *testing.T) {
	e := newEnv(t)
	t.Setenv("FAKE_ACK", "1")
	code, out, stderr := e.run("findings", "--format", "json", "--no-pr-check", "--no-summary")
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	var res FindingsResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	var keys []string
	status := map[string]string{}
	for _, f := range res.Findings {
		keys = append(keys, f.Key)
		status[f.Key] = f.Status
	}
	if strings.Join(keys, " ") != "AX-VULN AX-VULN-OLD HS-1 HS-ACK HS-2 AX-BUG AX-SMELL" {
		t.Fatalf("order: %v", keys)
	}
	if status["HS-ACK"] != "REVIEWED/ACKNOWLEDGED" || status["HS-1"] != "TO_REVIEW" {
		t.Errorf("status: %v", status)
	}
	var sawAck bool
	for _, c := range e.calls() {
		if strings.HasPrefix(c, "api get /api/hotspots/search") && strings.Contains(c, "resolution=ACKNOWLEDGED") && strings.Contains(c, "status=REVIEWED") {
			sawAck = true
		}
	}
	if !sawAck {
		t.Errorf("no acknowledged search: %v", e.calls())
	}
	code, out, _ = e.run("findings", "--format", "table", "--no-pr-check", "--no-summary")
	if code != 0 || !strings.Contains(out, "ACKNOWLEDGED no  HS-ACK") {
		t.Errorf("table:\n%s", out)
	}

	e2 := newEnv(t)
	t.Setenv("FAKE_ACK", "1")
	code, out, _ = e2.run("findings", "--format", "json", "--no-pr-check", "--no-summary", "--no-acknowledged")
	if code != 0 || strings.Contains(out, "HS-ACK") {
		t.Errorf("--no-acknowledged:\n%s", out)
	}
	for _, c := range e2.calls() {
		if strings.Contains(c, "resolution=ACKNOWLEDGED") {
			t.Errorf("--no-acknowledged still searched: %s", c)
		}
	}
}

func TestNextAcknowledgedHotspot(t *testing.T) {
	e := newEnv(t)
	t.Setenv("FAKE_ACK", "1")
	n := runNext(t, e, "--skip", "AX-VULN,AX-VULN-OLD,HS-1")
	if n.Finding == nil || n.Finding.Key != "HS-ACK" || !strings.Contains(n.Hint, "ACKNOWLEDGED") {
		t.Fatalf("next: %+v hint=%q", n.Finding, n.Hint)
	}
	if n.Brief == nil || n.Brief.Status != "REVIEWED/ACKNOWLEDGED" || strings.Join(n.Brief.Verdicts, ",") != "fix (then mark fixed),safe,reopen" {
		t.Errorf("brief: %+v", n.Brief)
	}
}

func TestHotspotReopenCommentAssign(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		args     []string
		endpoint string
		want     []string
		absent   string
		action   string
	}{
		{[]string{"hotspot", "HS-1", "reopen"}, "/api/hotspots/change_status", []string{`"status":"TO_REVIEW"`, `"hotspot":"HS-1"`}, `"resolution"`, "reopen"},
		{[]string{"hotspot", "HS-1", "comment"}, "/api/hotspots/add_comment", []string{`"hotspot":"HS-1"`}, `"status"`, "comment"},
		{[]string{"hotspot", "HS-1", "assign", "alice"}, "/api/hotspots/assign", []string{`"assignee":"alice"`, `"hotspot":"HS-1"`}, `"status"`, "assign alice"},
	}
	for i, c := range cases {
		// Approval is required for every action.
		if code, _, _ := e.run(append(c.args, "--comment", "why")...); code != ExitApproval {
			t.Errorf("%v without approval: exit %d", c.args, code)
		}
		if code, _, _ := e.run(append(c.args, "--approved-by-user")...); code != ExitApproval {
			t.Errorf("%v without comment: exit %d", c.args, code)
		}
		if code, _, _ := e.run(append(c.args, "--approved-by-user", "--comment", "why", "--dry-run")...); code != 0 {
			t.Errorf("%v dry-run: exit %d", c.args, code)
		}
		if p := e.posts(); len(p) != i {
			t.Fatalf("%v: unexpected posts before the real run: %v", c.args, p)
		}
		code, out, stderr := e.run(append(c.args, "--approved-by-user", "--comment", "user decided", "--format", "json")...)
		if code != 0 {
			t.Fatalf("%v: exit %d %s", c.args, code, stderr)
		}
		var res ChangeResult
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatal(err)
		}
		if res.Action != c.action || res.Kind != "hotspot" || res.Result != "ok" || !strings.Contains(res.APICall, c.endpoint) {
			t.Errorf("%v result: %+v", c.args, res)
		}
		p := e.posts()
		last := p[len(p)-1]
		if !strings.Contains(last, c.endpoint) || !strings.Contains(last, `"comment":"[sqa] user decided"`) || strings.Contains(last, c.absent) {
			t.Errorf("%v post: %s", c.args, last)
		}
		for _, w := range c.want {
			if !strings.Contains(last, w) {
				t.Errorf("%v post missing %s: %s", c.args, w, last)
			}
		}
	}
	audit := readAudit(t, e.home)
	var actions []string
	for _, a := range audit {
		actions = append(actions, a.Action+"/"+a.Result)
	}
	got := strings.Join(actions, ",")
	for _, want := range []string{"reopen/dry-run", "reopen/ok", "comment/dry-run", "comment/ok", "assign alice/dry-run", "assign alice/ok"} {
		if !strings.Contains(got, want) {
			t.Errorf("audit missing %s: %s", want, got)
		}
	}
	for _, bad := range [][]string{
		{"hotspot", "HS-1", "assign", "--approved-by-user", "--comment", "x"},
		{"hotspot", "HS-1", "reopen", "extra", "--approved-by-user", "--comment", "x"},
		{"hotspot", "HS-1", "wontfix", "--approved-by-user", "--comment", "x"},
	} {
		if code, _, _ := e.run(bad...); code != ExitUsage {
			t.Errorf("%v: exit %d, want 2", bad, code)
		}
	}
}
