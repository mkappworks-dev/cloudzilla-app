package model

// CodeOwnerRule represents a single line from a CODEOWNERS file.
type CodeOwnerRule struct {
	Pattern string
	Owners  []string // usernames without the leading "@"
}
