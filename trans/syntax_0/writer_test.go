package syntax_0

import "testing"

func TestCpp(t *testing.T) {
	for code, want := range map[string]string{
		"emit hit(1);":                      "(void) hit(1);",
		"rpc ping(); rpc(peer) x->ping(2);": "_gdpp_rpc_ping(0); x->_gdpp_rpc_ping(peer, 2);",
		`rpc("ping"); rpc_id(1, "ping");`:   `rpc("ping"); rpc_id(1, "ping");`,
		"if (t && is_done t) x = claim t;":  "if (t && t.is_done()) x = t.claim();",
		"claim\n  this->tasks[i];":          "\n  this->tasks[i].claim();",
		"t.is_done(); claim (a);":           "t.is_done(); claim (a);",
		"cancel t; t.cancel(); cancel (t);": "t.cancel(); t.cancel(); cancel (t);",
		"if (is_cancelled) return;":         "if (gdpp::is_cancelled()) return;",
		"s.is_cancelled; p->is_cancelled; is_cancelled(); gdpp::is_cancelled();": "s.is_cancelled; p->is_cancelled; is_cancelled(); gdpp::is_cancelled();",
	} {
		if got := cpp(code); got != want {
			t.Errorf("cpp(%q) = %q, want %q", code, got, want)
		}
	}
}
