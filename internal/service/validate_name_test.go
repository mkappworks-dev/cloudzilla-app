package service

import "testing"

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid simple", input: "my-repo", wantErr: false},
		{name: "valid with dot", input: "repo.name", wantErr: false},
		{name: "valid with underscore", input: "repo_name", wantErr: false},
		{name: "valid numeric start", input: "123repo", wantErr: false},
		{name: "single char", input: "a", wantErr: false},
		{name: "empty string", input: "", wantErr: true},
		{name: "too long (101 chars)", input: string(make([]byte, 101)), wantErr: true},
		{name: "dot traversal", input: ".", wantErr: true},
		{name: "double dot traversal", input: "..", wantErr: true},
		{name: "starts with dash", input: "-repo", wantErr: true},
		{name: "starts with dot", input: ".hidden", wantErr: true},
		{name: "starts with underscore", input: "_repo", wantErr: true},
		{name: "contains slash", input: "repo/name", wantErr: true},
		{name: "contains backslash", input: "repo\\name", wantErr: true},
		{name: "contains space", input: "repo name", wantErr: true},
		{name: "path traversal attempt", input: "../../etc", wantErr: true},
		{name: "unicode characters", input: "répo", wantErr: true},
		{name: "max length (100 chars)", input: "a" + string(make([]byte, 99)), wantErr: true}, // 100 null bytes = invalid chars
	}

	// Manually create a valid 100-char name
	validLong := make([]byte, 100)
	for i := range validLong {
		validLong[i] = 'a'
	}
	tests = append(tests, struct {
		name    string
		input   string
		wantErr bool
	}{name: "valid max length", input: string(validLong), wantErr: false})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for input %q, got nil", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for input %q: %v", tt.input, err)
			}
		})
	}
}
