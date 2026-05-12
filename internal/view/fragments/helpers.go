package fragments

import (
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// topicNames returns the topic names as a comma-separated string.
func topicNames(topics []model.Topic) string {
	names := make([]string, len(topics))
	for i, t := range topics {
		names[i] = t.Name
	}
	return strings.Join(names, ", ")
}
