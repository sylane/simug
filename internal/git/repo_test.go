package git

import "testing"

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		name       string
		remote     string
		wantOwner  string
		wantRepo   string
		shouldFail bool
	}{
		{name: "ssh", remote: "git@github.com:octo/example.git", wantOwner: "octo", wantRepo: "example"},
		{name: "https", remote: "https://github.com/octo/example.git", wantOwner: "octo", wantRepo: "example"},
		{name: "https no git suffix", remote: "https://github.com/octo/example", wantOwner: "octo", wantRepo: "example"},
		{name: "unsupported", remote: "https://gitlab.com/octo/example.git", shouldFail: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo, err := parseGitHubURL(tc.remote)
			if tc.shouldFail {
				if err == nil {
					t.Fatalf("expected error for remote %q", tc.remote)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseGitHubURL(%q) returned error: %v", tc.remote, err)
			}
			if repo.Owner != tc.wantOwner || repo.Name != tc.wantRepo {
				t.Fatalf("parseGitHubURL(%q) = %s/%s, want %s/%s", tc.remote, repo.Owner, repo.Name, tc.wantOwner, tc.wantRepo)
			}
		})
	}
}
