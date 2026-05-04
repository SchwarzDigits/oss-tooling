package github

import "testing"

// TestMatchesJobName guards the regression that prompted this matcher's
// existence: GitHub returns reusable-workflow job names as
// "<caller-job-id> / <job-name>" rather than the bare name, and uses the
// `name:` display string rather than the YAML job key. The matcher accepts
// both forms (prefixed and bare) and both naming conventions (display name
// and YAML key) so a rename of either in the central workflow doesn't
// silently break inventory reporting.
func TestMatchesJobName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		jobName    string
		candidates []string
		want       bool
	}{
		{
			name:       "prefixed display name (real-world API shape)",
			jobName:    "compliance / Secret and vulnerability scan",
			candidates: secretsVulnJobNames,
			want:       true,
		},
		{
			name:       "prefixed YAML key (fallback when no name: set)",
			jobName:    "compliance / secret-and-vuln-scan",
			candidates: secretsVulnJobNames,
			want:       true,
		},
		{
			name:       "bare display name (non-reusable trigger)",
			jobName:    "Secret and vulnerability scan",
			candidates: secretsVulnJobNames,
			want:       true,
		},
		{
			name:       "bare YAML key",
			jobName:    "secret-and-vuln-scan",
			candidates: secretsVulnJobNames,
			want:       true,
		},
		{
			name:       "different caller-job-id still matches via suffix",
			jobName:    "release / License analysis and SBOM",
			candidates: licenseJobNames,
			want:       true,
		},
		{
			name:       "decide-ort routing job is not a known check",
			jobName:    "compliance / Decide if ORT runs",
			candidates: secretsVulnJobNames,
			want:       false,
		},
		{
			name:       "decide-ort against license candidates also rejected",
			jobName:    "compliance / Decide if ORT runs",
			candidates: licenseJobNames,
			want:       false,
		},
		{
			name:       "near-match without separator does not match",
			jobName:    "myprefixSecret and vulnerability scan",
			candidates: secretsVulnJobNames,
			want:       false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := matchesJobName(tc.jobName, tc.candidates)
			if got != tc.want {
				t.Errorf("matchesJobName(%q, %v) = %v, want %v", tc.jobName, tc.candidates, got, tc.want)
			}
		})
	}
}
