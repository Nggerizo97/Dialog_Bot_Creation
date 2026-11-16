package privacy

import "testing"

func TestNormalizeEmail(t *testing.T) {
	if got, ok := NormalizeEmail("  Ana.Gomez@Example.COM "); !ok || got != "ana.gomez@example.com" {
		t.Errorf("NormalizeEmail = %q, %v", got, ok)
	}
	for _, bad := range []string{"", "ana", "ana@", "@example.com", "ana@localhost", "a b@example.com", "ana@example.com, bob@example.com"} {
		if _, ok := NormalizeEmail(bad); ok {
			t.Errorf("NormalizeEmail(%q) accepted an invalid address", bad)
		}
	}
}

func TestHashesAreScopedToTheWorkspace(t *testing.T) {
	a := EmailHash("ws-a", "ana@example.com")
	if a != EmailHash("ws-a", "ana@example.com") {
		t.Error("the same address in the same workspace must hash the same way")
	}
	if a == EmailHash("ws-b", "ana@example.com") {
		t.Error("the same address must not be linkable across workspaces")
	}
	if a == SubjectHash("ws-a", "chat_user", "ana@example.com") {
		t.Error("different kinds of subject must not collide")
	}
	if len(Short(a)) != 12 {
		t.Errorf("Short = %q", Short(a))
	}
}
