package service

import (
	"testing"
)

func TestSavedReplyValidate(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		body    string
		wantErr string
	}{
		{name: "valid", title: "My reply", body: "Thanks for the report!", wantErr: ""},
		{name: "empty title", title: "", body: "body", wantErr: "title is required"},
		{name: "whitespace title", title: "   ", body: "body", wantErr: "title is required"},
		{name: "empty body", title: "title", body: "", wantErr: "body is required"},
		{name: "whitespace body", title: "title", body: "   ", wantErr: "body is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := savedReplyValidate(tt.title, tt.body)
			if got != tt.wantErr {
				t.Errorf("savedReplyValidate(%q, %q) = %q, want %q", tt.title, tt.body, got, tt.wantErr)
			}
		})
	}
}
