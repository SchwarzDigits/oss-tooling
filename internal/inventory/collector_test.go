package inventory

import "testing"

func TestFilterExcluded(t *testing.T) {
	t.Parallel()

	repos := []Repository{
		{Name: ".github"},
		{Name: "oss-tooling"},
		{Name: "Compliance-Bot"},
		{Name: "playground"},
	}

	cases := []struct {
		name    string
		exclude []string
		want    []string
	}{
		{
			name:    "nil exclude keeps all",
			exclude: nil,
			want:    []string{".github", "oss-tooling", "Compliance-Bot", "playground"},
		},
		{
			name:    "empty entries ignored",
			exclude: []string{"", "  "},
			want:    []string{".github", "oss-tooling", "Compliance-Bot", "playground"},
		},
		{
			name:    "default .github filter",
			exclude: []string{".github"},
			want:    []string{"oss-tooling", "Compliance-Bot", "playground"},
		},
		{
			name:    "case-insensitive match",
			exclude: []string{"compliance-bot"},
			want:    []string{".github", "oss-tooling", "playground"},
		},
		{
			name:    "multiple names",
			exclude: []string{".github", "playground"},
			want:    []string{"oss-tooling", "Compliance-Bot"},
		},
		{
			name:    "no match leaves repos untouched",
			exclude: []string{"nope"},
			want:    []string{".github", "oss-tooling", "Compliance-Bot", "playground"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterExcluded(repos, tc.exclude)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %d (%v), want %d (%v)", len(got), names(got), len(tc.want), tc.want)
			}
			for i, name := range tc.want {
				if got[i].Name != name {
					t.Errorf("idx %d: got %q, want %q", i, got[i].Name, name)
				}
			}
		})
	}
}

func names(repos []Repository) []string {
	out := make([]string, len(repos))
	for i, r := range repos {
		out[i] = r.Name
	}
	return out
}
