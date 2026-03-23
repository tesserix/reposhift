package services

import (
	"reflect"
	"testing"

	"github.com/go-logr/logr"
)

func TestMaskToken(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		token    string
		expected string
	}{
		{
			name:     "Normal token masking",
			input:    "https://mytoken12345678@github.com/user/repo",
			token:    "mytoken12345678",
			expected: "https://myto***5678@github.com/user/repo",
		},
		{
			name:     "Empty token",
			input:    "https://github.com/user/repo",
			token:    "",
			expected: "https://github.com/user/repo",
		},
		{
			name:     "Short token (less than 8 chars)",
			input:    "https://short@github.com/user/repo",
			token:    "short",
			expected: "https://short@github.com/user/repo",
		},
		{
			name:     "Exactly 8 chars token",
			input:    "https://12345678@github.com/user/repo",
			token:    "12345678",
			expected: "https://1234***5678@github.com/user/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskToken(tt.input, tt.token)
			if result != tt.expected {
				t.Errorf("maskToken() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMatchBranchPattern(t *testing.T) {
	tests := []struct {
		name    string
		branch  string
		pattern string
		want    bool
	}{
		// Exact matches
		{"exact match simple", "main", "main", true},
		{"exact match with slash", "develop/7.0", "develop/7.0", true},
		{"exact match no match", "main", "develop", false},

		// Wildcard suffix patterns (feature/*)
		{"wildcard feature/*", "feature/login-page", "feature/*", true},
		{"wildcard feature/* nested", "feature/auth/oauth", "feature/*", true},
		{"wildcard bugfix/*", "bugfix/fix-123", "bugfix/*", true},
		{"wildcard hotfix/*", "hotfix/critical-fix", "hotfix/*", true},
		{"wildcard release/*", "release/1.0", "release/*", true},
		{"wildcard personal/*", "personal/john/experiment", "personal/*", true},
		{"wildcard no match", "main", "feature/*", false},
		{"wildcard no match prefix only", "feature", "feature/*", false},
		{"wildcard develop/*", "develop/8.0", "develop/*", true},

		// ADO hyphen-separated branches (feature-xxx instead of feature/xxx)
		{"hyphen feature-7.1-Prasanthi", "feature-7.1-Prasanthi", "feature/*", true},
		{"hyphen feature-gitdemo-7.1", "feature-gitdemo-7.1", "feature/*", true},
		{"hyphen bugfix-123", "bugfix-123", "bugfix/*", true},
		{"hyphen hotfix-critical-fix", "hotfix-critical-fix", "hotfix/*", true},
		{"hyphen release-1.0", "release-1.0", "release/*", true},
		{"hyphen personal-john-experiment", "personal-john-experiment", "personal/*", true},
		{"hyphen gitdemo-7.1-feature (not feature prefix)", "gitdemo-7.1-feature", "feature/*", false},

		// Full wildcard
		{"full wildcard matches anything", "main", "*", true},
		{"full wildcard matches branch with slash", "feature/x", "*", true},

		// No match cases
		{"different prefix", "bugfix/123", "feature/*", false},
		{"partial name match", "features/x", "feature/*", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchBranchPattern(tt.branch, tt.pattern)
			if got != tt.want {
				t.Errorf("matchBranchPattern(%q, %q) = %v, want %v", tt.branch, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestFilterBranches(t *testing.T) {
	logger := logr.Discard()

	tests := []struct {
		name            string
		branches        []string
		includeBranches []string
		excludeBranches []string
		defaultBranch   string
		want            []string
	}{
		{
			name:            "no filters returns all branches",
			branches:        []string{"main", "develop", "feature/login"},
			includeBranches: nil,
			excludeBranches: nil,
			defaultBranch:   "main",
			want:            []string{"main", "develop", "feature/login"},
		},
		{
			name:            "exclude feature/* removes all feature branches",
			branches:        []string{"main", "develop", "feature/login", "feature/signup", "bugfix/123"},
			excludeBranches: []string{"feature/*"},
			defaultBranch:   "main",
			want:            []string{"main", "develop", "bugfix/123"},
		},
		{
			name:            "exclude multiple wildcard patterns",
			branches:        []string{"main", "develop", "feature/x", "bugfix/y", "hotfix/z", "release/1.0"},
			excludeBranches: []string{"feature/*", "bugfix/*", "hotfix/*"},
			defaultBranch:   "main",
			want:            []string{"main", "develop", "release/1.0"},
		},
		{
			name:            "exclude exact branch",
			branches:        []string{"main", "develop", "develop/7.0", "develop/8.0"},
			excludeBranches: []string{"develop/7.0"},
			defaultBranch:   "main",
			want:            []string{"main", "develop", "develop/8.0"},
		},
		{
			name:            "exclude mix of wildcard and exact",
			branches:        []string{"main", "develop", "feature/a", "feature/b", "develop/7.0", "release/1.0"},
			excludeBranches: []string{"feature/*", "develop/7.0"},
			defaultBranch:   "main",
			want:            []string{"main", "develop", "release/1.0"},
		},
		{
			name:            "default branch is never excluded",
			branches:        []string{"main", "feature/x"},
			excludeBranches: []string{"main", "feature/*"},
			defaultBranch:   "main",
			want:            []string{"main"},
		},
		{
			name:            "default branch protected from wildcard",
			branches:        []string{"develop", "develop/7.0", "develop/8.0"},
			excludeBranches: []string{"develop/*"},
			defaultBranch:   "develop",
			want:            []string{"develop"},
		},
		{
			name:            "empty branches returns empty",
			branches:        []string{},
			excludeBranches: []string{"feature/*"},
			defaultBranch:   "main",
			want:            []string{},
		},
		{
			name:            "exclude patterns that match nothing - all branches kept",
			branches:        []string{"main", "develop", "release/1.0"},
			excludeBranches: []string{"feature/*", "bugfix/*"},
			defaultBranch:   "main",
			want:            []string{"main", "develop", "release/1.0"},
		},
		{
			name:            "nested feature branches excluded by wildcard",
			branches:        []string{"main", "feature/auth/oauth", "feature/auth/saml", "feature/simple"},
			excludeBranches: []string{"feature/*"},
			defaultBranch:   "main",
			want:            []string{"main"},
		},
		{
			name:            "personal branches excluded",
			branches:        []string{"main", "personal/john/experiment", "personal/jane/test"},
			excludeBranches: []string{"personal/*"},
			defaultBranch:   "main",
			want:            []string{"main"},
		},
		{
			name:            "monorepo scenario - repo has no matching branches to exclude",
			branches:        []string{"main", "develop", "release/2.0"},
			excludeBranches: []string{"feature/*", "bugfix/*", "hotfix/*"},
			defaultBranch:   "main",
			want:            []string{"main", "develop", "release/2.0"},
		},
		{
			name:            "ADO hyphen-style branches excluded by wildcard",
			branches:        []string{"main", "develop", "feature-7.1-Prasanthi", "feature-7.2-gitdemo", "bugfix-123", "hotfix-critical", "release-1.0"},
			excludeBranches: []string{"feature/*", "bugfix/*", "hotfix/*"},
			defaultBranch:   "main",
			want:            []string{"main", "develop", "release-1.0"},
		},
		{
			name:            "mixed slash and hyphen branches excluded",
			branches:        []string{"main", "feature/login", "feature-signup", "bugfix/456", "bugfix-789", "develop"},
			excludeBranches: []string{"feature/*", "bugfix/*"},
			defaultBranch:   "main",
			want:            []string{"main", "develop"},
		},
		{
			name:            "ADO real-world monorepo scenario",
			branches:        []string{"main", "develop", "feature-7.1-Prasanthi", "feature-7.1-gitdemo", "feature-7.2-Prasanthi", "feature-gitdemo-7.1", "gitdemo-7.1-feature", "release/7.0", "release/7.1"},
			excludeBranches: []string{"feature/*"},
			defaultBranch:   "main",
			want:            []string{"main", "develop", "gitdemo-7.1-feature", "release/7.0", "release/7.1"},
		},
		// ---- INCLUDE MODE TESTS ----
		{
			name:            "include only main and develop",
			branches:        []string{"main", "develop", "feature/x", "bugfix/y", "hotfix/z"},
			includeBranches: []string{"main", "develop"},
			defaultBranch:   "main",
			want:            []string{"main", "develop"},
		},
		{
			name:            "include release/* keeps all release branches",
			branches:        []string{"main", "develop", "release/7.0", "release/7.1", "feature/x"},
			includeBranches: []string{"release/*", "main"},
			defaultBranch:   "main",
			want:            []string{"main", "release/7.0", "release/7.1"},
		},
		{
			name:            "include mode - default branch always included even if not in list",
			branches:        []string{"main", "develop", "feature/x"},
			includeBranches: []string{"develop"},
			defaultBranch:   "main",
			want:            []string{"main", "develop"},
		},
		{
			name:            "include with ADO hyphen branches",
			branches:        []string{"main", "develop", "release-7.0", "release-7.1", "feature-login"},
			includeBranches: []string{"main", "release/*"},
			defaultBranch:   "main",
			want:            []string{"main", "release-7.0", "release-7.1"},
		},
		{
			name:            "include empty list returns all (no filtering)",
			branches:        []string{"main", "develop", "feature/x"},
			includeBranches: []string{},
			defaultBranch:   "main",
			want:            []string{"main", "develop", "feature/x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterBranches(tt.branches, tt.includeBranches, tt.excludeBranches, tt.defaultBranch, logger)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FilterBranches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildCloneArgs(t *testing.T) {
	tests := []struct {
		name     string
		depth    int
		url      string
		target   string
		wantArgs []string
	}{
		{
			name:   "full clone (depth=0) uses --mirror",
			depth:  0,
			url:    "https://dev.azure.com/org/proj/_git/repo",
			target: "/tmp/repo",
			wantArgs: []string{
				"clone", "--mirror",
				"https://dev.azure.com/org/proj/_git/repo",
				"/tmp/repo",
			},
		},
		{
			name:   "shallow clone (depth=100) uses --bare --depth --no-single-branch",
			depth:  100,
			url:    "https://dev.azure.com/org/proj/_git/repo",
			target: "/tmp/repo",
			wantArgs: []string{
				"clone", "--bare",
				"--depth", "100",
				"--no-single-branch",
				"https://dev.azure.com/org/proj/_git/repo",
				"/tmp/repo",
			},
		},
		{
			name:   "shallow clone with 1 commit",
			depth:  1,
			url:    "https://dev.azure.com/org/proj/_git/repo",
			target: "/tmp/repo",
			wantArgs: []string{
				"clone", "--bare",
				"--depth", "1",
				"--no-single-branch",
				"https://dev.azure.com/org/proj/_git/repo",
				"/tmp/repo",
			},
		},
		{
			name:   "large depth",
			depth:  50000,
			url:    "https://dev.azure.com/org/proj/_git/big-repo",
			target: "/tmp/big-repo",
			wantArgs: []string{
				"clone", "--bare",
				"--depth", "50000",
				"--no-single-branch",
				"https://dev.azure.com/org/proj/_git/big-repo",
				"/tmp/big-repo",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildCloneArgs(tt.depth, tt.url, tt.target)
			if !reflect.DeepEqual(got, tt.wantArgs) {
				t.Errorf("BuildCloneArgs(%d) = %v, want %v", tt.depth, got, tt.wantArgs)
			}
		})
	}
}
