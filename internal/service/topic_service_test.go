package service

import "testing"

func TestValidateTopicName_Valid(t *testing.T) {
	cases := []string{"go", "web", "machine-learning", "go123", "a"}
	for _, name := range cases {
		if err := validateTopicName(name); err != nil {
			t.Errorf("expected valid for %q, got: %v", name, err)
		}
	}
}

func TestValidateTopicName_InvalidChars(t *testing.T) {
	cases := []string{"Go", "web_app", "my topic", "WEB", "my.topic", "-start"}
	for _, name := range cases {
		if err := validateTopicName(name); err == nil {
			t.Errorf("expected error for %q, got nil", name)
		}
	}
}

func TestValidateTopicName_TooLong(t *testing.T) {
	name := "abcdefghijklmnopqrstu" // 21 chars
	if err := validateTopicName(name); err == nil {
		t.Fatalf("expected error for name > 20 chars, got nil")
	}
}

func TestValidateTopicName_Empty(t *testing.T) {
	if err := validateTopicName(""); err == nil {
		t.Fatal("expected error for empty topic name, got nil")
	}
}

func TestValidateTopicName_MaxLength(t *testing.T) {
	name := "abcdefghijklmnopqrst" // exactly 20 chars
	if err := validateTopicName(name); err != nil {
		t.Fatalf("expected valid for 20-char name, got: %v", err)
	}
}

func TestSetTopics_TooMany(t *testing.T) {
	svc := &TopicService{}
	names := make([]string, maxTopics+1)
	for i := range names {
		names[i] = "go"
	}
	if err := svc.SetTopics(nil, 1, names); err == nil {
		t.Fatal("expected error for too many topics, got nil")
	}
}
