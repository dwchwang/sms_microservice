package projection

import (
	"context"

	"github.com/vcs-sms/server-service/internal/model"
)

// activeTargetReader is the repository method Rebuild needs.
type activeTargetReader interface {
	FindActiveTargets(ctx context.Context, cursor string, limit int) ([]model.Server, error)
}

// RepoSource adapts the server repository to a TargetSource.
type RepoSource struct {
	repo activeTargetReader
}

// NewRepoSource creates a TargetSource backed by the server repository.
func NewRepoSource(repo activeTargetReader) *RepoSource {
	return &RepoSource{repo: repo}
}

func (s *RepoSource) NextTargets(ctx context.Context, cursor string, limit int) ([]Target, error) {
	servers, err := s.repo.FindActiveTargets(ctx, cursor, limit)
	if err != nil {
		return nil, err
	}
	targets := make([]Target, 0, len(servers))
	for _, srv := range servers {
		targets = append(targets, Target{
			ServerID:   srv.ServerID,
			ServerName: srv.ServerName,
			IPv4:       srv.IPv4,
			TCPPort:    srv.TCPPort,
		})
	}
	return targets, nil
}
