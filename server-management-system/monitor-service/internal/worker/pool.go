package worker

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog"
	"github.com/vcs-sms/monitor-service/internal/checker"
)

// Pool manages a fixed number of goroutine workers for health-checking.
type Pool struct {
	workerCount int
	checker     checker.HealthChecker
	logger      zerolog.Logger
}

// NewPool creates a new worker pool.
func NewPool(workerCount int, chk checker.HealthChecker, logger zerolog.Logger) *Pool {
	if workerCount < 1 {
		logger.Warn().Int("worker_count", workerCount).Msg("Invalid worker count, defaulting to 1")
		workerCount = 1
	}
	return &Pool{
		workerCount: workerCount,
		checker:     chk,
		logger:      logger,
	}
}

func (p *Pool) WorkerCount() int {
	if p.workerCount < 1 {
		return 1
	}
	return p.workerCount
}

// Execute runs health-checks on all servers concurrently using the worker pool.
// Returns results and an error if context was cancelled before all servers were checked.
// Each worker runs with panic recovery to prevent a single server check from
// crashing the entire pool.
func (p *Pool) Execute(ctx context.Context, servers []*checker.ServerInfo) ([]*checker.HealthResult, error) {
	if len(servers) == 0 {
		return nil, nil
	}

	jobs := make(chan *checker.ServerInfo, len(servers))
	results := make(chan *checker.HealthResult, len(servers))

	// Spawn workers
	var wg sync.WaitGroup
	for i := 0; i < p.workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					p.logger.Error().
						Int("worker_id", workerID).
						Interface("panic", r).
						Msg("Worker panic recovered — continuing with remaining workers")
				}
			}()
			for server := range jobs {
				select {
				case <-ctx.Done():
					// Drain remaining jobs on cancellation
					for range jobs {
					}
					return
				default:
					result := p.checker.Check(ctx, server)
					results <- result
				}
			}
		}(i)
	}

	// Fan-out: send all jobs
	go func() {
		for _, srv := range servers {
			select {
			case <-ctx.Done():
				close(jobs) // signal workers to stop
				return
			case jobs <- srv:
			}
		}
		close(jobs)
	}()

	// Wait for workers to finish, then close results channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect all results
	var allResults []*checker.HealthResult
	for r := range results {
		allResults = append(allResults, r)
	}

	// Check if context was cancelled (partial execution)
	if ctx.Err() != nil {
		return allResults, fmt.Errorf("execution cancelled: %d/%d servers checked: %w",
			len(allResults), len(servers), ctx.Err())
	}

	return allResults, nil
}
