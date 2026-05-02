package inventory

import "testing"

func TestComplianceWorkflowRegex(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "canonical reusable-workflow reference",
			body: "jobs:\n  compliance:\n    uses: SchwarzDigits/oss-compliance/.github/workflows/compliance.yml@main\n",
			want: true,
		},
		{
			name: "canonical reference with extra leading whitespace",
			body: "jobs:\n  compliance:\n      uses:    SchwarzDigits/oss-compliance/.github/workflows/check.yml@v1\n",
			want: true,
		},
		{
			name: "self-reference using relative path inside oss-compliance repo",
			body: "jobs:\n  test:\n    uses: ./.github/workflows/internal.yml\n",
			want: false,
		},
		{
			name: "comment that mentions the org/repo but no uses directive",
			body: "# We rely on SchwarzDigits/oss-compliance for license checks.\njobs:\n  hello: {}\n",
			want: false,
		},
		{
			name: "uses directive with a different repo",
			body: "jobs:\n  x:\n    uses: actions/checkout@v4\n",
			want: false,
		},
		{
			name: "uses directive that mentions oss-compliance only in repo name not path",
			body: "jobs:\n  x:\n    uses: SchwarzDigits/oss-compliance@main\n",
			want: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := complianceWorkflowRE.MatchString(tc.body)
			if got != tc.want {
				t.Fatalf("complianceWorkflowRE.MatchString = %v, want %v", got, tc.want)
			}
		})
	}
}
