package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

const maxTopics = 20

var topicNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,19}$`)

// TopicService manages repository topic tags.
type TopicService struct {
	topics *store.TopicStore
}

// NewTopicService creates a TopicService backed by the given topic store.
func NewTopicService(topics *store.TopicStore) *TopicService {
	return &TopicService{topics: topics}
}

func validateTopicName(name string) error {
	if len(name) == 0 {
		return errors.New("topic name must not be empty")
	}
	if len(name) > 20 {
		return fmt.Errorf("topic %q exceeds 20-character limit", name)
	}
	if !topicNameRE.MatchString(name) {
		return fmt.Errorf("topic %q must be lowercase alphanumeric with hyphens, starting with a letter or digit", name)
	}
	return nil
}

// SetTopics validates and replaces all topics for a repository.
// The caller (handler) must verify CanManage before calling this.
func (s *TopicService) SetTopics(ctx context.Context, repoID int64, names []string) error {
	if len(names) > maxTopics {
		return fmt.Errorf("a repository cannot have more than %d topics", maxTopics)
	}
	for _, name := range names {
		if err := validateTopicName(name); err != nil {
			return err
		}
	}
	return s.topics.SetTopics(ctx, repoID, names)
}

// ListByRepo returns all topics for a repository.
func (s *TopicService) ListByRepo(ctx context.Context, repoID int64) ([]model.Topic, error) {
	return s.topics.ListByRepo(ctx, repoID)
}

// ListReposByTopic returns public repos for a topic name, paginated.
func (s *TopicService) ListReposByTopic(ctx context.Context, name string, page, pageSize int) ([]model.Repository, error) {
	if err := validateTopicName(name); err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return s.topics.ListReposByTopic(ctx, name, page, pageSize)
}
