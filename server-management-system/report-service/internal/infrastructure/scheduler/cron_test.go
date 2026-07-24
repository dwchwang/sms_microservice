package scheduler

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/report-service/internal/dto"
	"github.com/vcs-sms/report-service/internal/model"
	"gorm.io/gorm"
)

var (
	loc     = mustLoad("Asia/Ho_Chi_Minh")
	errBoom = errors.New("boom")
)

func mustLoad(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

type fakeRuns struct {
	mu sync.Mutex

	claimed   []string
	done      []string
	failed    []string
	failedMsg string

	refuseClaim bool
	claimErr    error
	doneJobs    map[string]bool
	// heartbeats counts calls; loseAfter turns the claim over to somebody else.
	heartbeats int
	loseAfter  int
}

func (f *fakeRuns) TryClaim(ctx context.Context, job, date, owner string, staleAfter time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimErr != nil {
		return false, f.claimErr
	}
	if f.refuseClaim {
		return false, nil
	}
	f.claimed = append(f.claimed, job+"@"+date)
	return true, nil
}

func (f *fakeRuns) Heartbeat(ctx context.Context, job, date, owner string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heartbeats++
	if f.loseAfter > 0 && f.heartbeats >= f.loseAfter {
		return false, nil
	}
	return true, nil
}

func (f *fakeRuns) MarkDone(ctx context.Context, job, date, owner string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.done = append(f.done, job+"@"+date)
	return nil
}

func (f *fakeRuns) MarkFailed(ctx context.Context, job, date, owner, errMsg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = append(f.failed, job+"@"+date)
	f.failedMsg = errMsg
	return nil
}

func (f *fakeRuns) IsDone(ctx context.Context, job, date string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.doneJobs[job+"@"+date], nil
}

type fakeReportJobs struct {
	latestDaily *model.ReportJob
	findErr     error
}

func (f *fakeReportJobs) FindLatestDaily(ctx context.Context, date string) (*model.ReportJob, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	if f.latestDaily == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return f.latestDaily, nil
}

type fakeSends struct {
	mu    sync.Mutex
	calls []dto.SendReportRequest
	err   error
}

func (f *fakeSends) Send(ctx context.Context, req dto.SendReportRequest, reportType, requesterID, key string) (*dto.JobResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if f.err != nil {
		return nil, f.err
	}
	return &dto.JobResponse{ID: uuid.NewString(), State: model.StateSent}, nil
}

func (f *fakeSends) sent() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// newScheduler wires a Scheduler with a single job whose run function is under
// the test's control, so the loop can be exercised without real work.
func newScheduler(runs *fakeRuns, now time.Time, run func(context.Context, time.Time) error) *Scheduler {
	s := &Scheduler{
		runs:  runs,
		owner: "node-test",
		loc:   loc,
		log:   zerolog.New(io.Discard),
		now:   func() time.Time { return now },
	}
	sched, err := cron.ParseStandard("30 0 * * *")
	if err != nil {
		panic(err)
	}
	s.jobs = []job{{name: model.JobSnapshot, schedule: sched, timeout: time.Minute, run: run}}
	return s
}

func at(h, m int) time.Time {
	return time.Date(2026, 7, 24, h, m, 0, 0, loc)
}

func TestTick_DoesNothingBeforeTheFireTime(t *testing.T) {
	runs := &fakeRuns{}
	ran := false
	s := newScheduler(runs, at(0, 10), func(context.Context, time.Time) error { ran = true; return nil })

	s.tick(context.Background())

	if ran || len(runs.claimed) != 0 {
		t.Errorf("job ran before its fire time (claimed=%v)", runs.claimed)
	}
}

func TestTick_RunsForYesterdayOnceDue(t *testing.T) {
	runs := &fakeRuns{}
	var gotDate time.Time
	s := newScheduler(runs, at(0, 31), func(_ context.Context, d time.Time) error {
		gotDate = d
		return nil
	})

	s.tick(context.Background())

	if want := "snapshot@2026-07-23"; len(runs.claimed) != 1 || runs.claimed[0] != want {
		t.Errorf("claimed = %v, want [%s]", runs.claimed, want)
	}
	if got := gotDate.Format(time.DateOnly); got != "2026-07-23" {
		t.Errorf("run date = %q, want yesterday 2026-07-23", got)
	}
	if len(runs.done) != 1 {
		t.Errorf("done = %v, want one entry", runs.done)
	}
}

// A replica that boots hours after the fire time still catches the run up.
// This is the property a plain cron trigger does not have.
func TestTick_CatchesUpALateStart(t *testing.T) {
	runs := &fakeRuns{}
	s := newScheduler(runs, at(9, 0), func(context.Context, time.Time) error { return nil })

	s.tick(context.Background())

	if len(runs.claimed) != 1 {
		t.Errorf("claimed = %v, want the missed run to be picked up", runs.claimed)
	}
}

func TestTick_LosingTheClaimSkipsTheWork(t *testing.T) {
	runs := &fakeRuns{refuseClaim: true}
	ran := false
	s := newScheduler(runs, at(1, 0), func(context.Context, time.Time) error { ran = true; return nil })

	s.tick(context.Background())

	if ran {
		t.Error("the work ran without holding the claim")
	}
}

func TestTick_FailureIsRecordedForRetry(t *testing.T) {
	runs := &fakeRuns{}
	s := newScheduler(runs, at(1, 0), func(context.Context, time.Time) error { return errBoom })

	s.tick(context.Background())

	if len(runs.failed) != 1 || runs.failedMsg != errBoom.Error() {
		t.Errorf("failed = %v msg = %q, want the error recorded", runs.failed, runs.failedMsg)
	}
	if len(runs.done) != 0 {
		t.Error("a failed job was marked done")
	}
}

func TestTick_WaitsForThePrerequisiteJob(t *testing.T) {
	runs := &fakeRuns{doneJobs: map[string]bool{}}
	ran := false
	s := newScheduler(runs, at(11, 0), func(context.Context, time.Time) error { ran = true; return nil })
	s.jobs[0].name = model.JobDailyReport
	s.jobs[0].dependsOn = model.JobSnapshot

	s.tick(context.Background())
	if ran {
		t.Fatal("the daily report ran before the snapshot finished")
	}

	runs.doneJobs["snapshot@2026-07-23"] = true
	s.tick(context.Background())
	if !ran {
		t.Error("the daily report did not run after the snapshot finished")
	}
}

func TestRunDailyReport_SkipsWhenTheMailMayHaveGoneOut(t *testing.T) {
	notResendable := []string{model.StateSending, model.StateSent, model.StateDeliveryUnknown}

	for _, state := range notResendable {
		t.Run(state, func(t *testing.T) {
			sends := &fakeSends{}
			s := &Scheduler{
				runs:       &fakeRuns{},
				reportJobs: &fakeReportJobs{latestDaily: &model.ReportJob{ID: uuid.New(), State: state}},
				sends:      sends,
				recipient:  "ops@vcs.com.vn",
				loc:        loc,
				log:        zerolog.New(io.Discard),
			}

			if err := s.runDailyReport(context.Background(), at(0, 0)); err != nil {
				t.Fatalf("runDailyReport failed: %v", err)
			}
			if sends.sent() != 0 {
				t.Errorf("state %q was resent", state)
			}
		})
	}
}

func TestRunDailyReport_ResendsWhenNothingReachedTheWire(t *testing.T) {
	resendableStates := []string{model.StateProcessing, model.StateGenerated, model.StateFailed}

	for _, state := range resendableStates {
		t.Run(state, func(t *testing.T) {
			sends := &fakeSends{}
			s := &Scheduler{
				runs:       &fakeRuns{},
				reportJobs: &fakeReportJobs{latestDaily: &model.ReportJob{ID: uuid.New(), State: state}},
				sends:      sends,
				recipient:  "ops@vcs.com.vn",
				loc:        loc,
				log:        zerolog.New(io.Discard),
			}

			if err := s.runDailyReport(context.Background(), at(0, 0)); err != nil {
				t.Fatalf("runDailyReport failed: %v", err)
			}
			if sends.sent() != 1 {
				t.Errorf("state %q was not resent", state)
			}
		})
	}
}

func TestRunDailyReport_SendsWhenThereIsNoEarlierAttempt(t *testing.T) {
	sends := &fakeSends{}
	s := &Scheduler{
		runs:       &fakeRuns{},
		reportJobs: &fakeReportJobs{},
		sends:      sends,
		recipient:  "ops@vcs.com.vn",
		loc:        loc,
		log:        zerolog.New(io.Discard),
	}

	if err := s.runDailyReport(context.Background(), at(0, 0)); err != nil {
		t.Fatalf("runDailyReport failed: %v", err)
	}

	if sends.sent() != 1 {
		t.Fatalf("sent %d reports, want 1", sends.sent())
	}
	got := sends.calls[0]
	if got.StartDate != "2026-07-24" || got.EndDate != "2026-07-24" {
		t.Errorf("range = %s..%s, want the given day on both ends", got.StartDate, got.EndDate)
	}
	if got.RecipientEmail != "ops@vcs.com.vn" {
		t.Errorf("recipient = %q", got.RecipientEmail)
	}
}

func TestRegister_SkipsTheDailyReportWithoutARecipient(t *testing.T) {
	s := NewScheduler(&fakeRuns{}, &fakeReportJobs{}, nil, &fakeSends{}, "", "node", loc, zerolog.New(io.Discard))

	if err := s.Register("30 0 * * *", "0 10 * * *"); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if len(s.jobs) != 1 || s.jobs[0].name != model.JobSnapshot {
		t.Errorf("jobs = %d, want only the snapshot", len(s.jobs))
	}
}

func TestRegister_RejectsABadExpression(t *testing.T) {
	s := NewScheduler(&fakeRuns{}, &fakeReportJobs{}, nil, &fakeSends{}, "ops@vcs.com.vn", "node", loc, zerolog.New(io.Discard))

	if err := s.Register("not a cron", "0 10 * * *"); err == nil {
		t.Error("expected an invalid cron expression to be rejected")
	}
}
