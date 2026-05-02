package config

import "testing"

func TestExcludes_IsExcluded(t *testing.T) {
	t.Parallel()

	e := NewExcludes([]string{
		"*/.github",
		"SchwarzDigits/oss-compliance",
		"SchwarzDigits/oss-inventory",
		"   ",
		"bogus pattern with spaces and stars*",
	})

	cases := []struct {
		org, repo string
		want      bool
	}{
		{"SchwarzDigits", ".github", true},                  // wildcard match
		{"SchwarzIT", ".github", true},                       // wildcard match across orgs
		{"SchwarzDigits", "oss-compliance", true},            // exact match
		{"SchwarzDigits", "oss-inventory", true},             // exact match
		{"SchwarzDigits", "OSS-Compliance", false},           // case-sensitive
		{"OtherOrg", "oss-compliance", false},                // exact match scoped to org
		{"SchwarzDigits", "oss-tooling", false},              // not excluded
		{"SchwarzIT", "platform-cli", false},                 // not excluded
		{"SchwarzDigits", "github", false},                   // pattern is `*/.github` not `*/github`
	}
	for _, tc := range cases {
		tc := tc
		name := tc.org + "/" + tc.repo
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := e.IsExcluded(tc.org, tc.repo)
			if got != tc.want {
				t.Errorf("IsExcluded(%q, %q) = %v, want %v", tc.org, tc.repo, got, tc.want)
			}
		})
	}
}

func TestExcludes_NilReceiverIsSafe(t *testing.T) {
	t.Parallel()

	var e *Excludes
	if e.IsExcluded("a", "b") {
		t.Errorf("nil *Excludes should never match")
	}
}

func TestExcludes_EmptyPatternsCompileToNoop(t *testing.T) {
	t.Parallel()

	e := NewExcludes(nil)
	if e.IsExcluded("a", "b") {
		t.Errorf("empty list should not match")
	}
}
