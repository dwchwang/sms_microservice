package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/report-service/internal/dto"
	"github.com/vcs-sms/report-service/internal/infrastructure/email"
	"github.com/vcs-sms/report-service/internal/infrastructure/excel"
	"github.com/vcs-sms/report-service/internal/model"
	"github.com/vcs-sms/report-service/internal/repository"
)

type fakeJobs struct {
	created *model.ReportJob
	states  []string
	failed  string
	errMsg  string
	msgID   string
	sentID  string
}

func (f *fakeJobs) Create(ctx context.Context, job *model.ReportJob) error {
	f.created = job
	f.states = append(f.states, job.State)
	return nil
}

func (f *fakeJobs) FindByID(ctx context.Context, id uuid.UUID) (*model.ReportJob, error) {
	if f.created == nil {
		return nil, errBoom
	}
	return f.created, nil
}

func (f *fakeJobs) SetState(ctx context.Context, id uuid.UUID, state string) error {
	f.states = append(f.states, state)
	return nil
}

func (f *fakeJobs) SetGenerated(ctx context.Context, id uuid.UUID, body []byte) error {
	f.states = append(f.states, model.StateGenerated)
	f.created.ResponseJSON = body
	return nil
}

func (f *fakeJobs) SetSent(ctx context.Context, id uuid.UUID, messageID string) error {
	f.states = append(f.states, model.StateSent)
	f.sentID = messageID
	return nil
}

func (f *fakeJobs) SetFailed(ctx context.Context, id uuid.UUID, state, errMsg, messageID string) error {
	f.states = append(f.states, state)
	f.failed, f.errMsg, f.msgID = state, errMsg, messageID
	return nil
}

type fakeSender struct {
	messageID string
	err       error
	sent      *email.Message
}

func (f *fakeSender) Send(msg email.Message) (string, error) {
	f.sent = &msg
	return f.messageID, f.err
}

type fakeRenderer struct {
	err error
}

func (f *fakeRenderer) Render(s *dto.SummaryResponse) (string, string, error) {
	if f.err != nil {
		return "", "", f.err
	}
	return "subject", "<p>body</p>", nil
}

func newTestSendService(sendErr error) (SendService, *fakeJobs, *fakeSender) {
	reports := newTestService(&fakeSnapshots{totals: sampleTotals()})
	jobs := &fakeJobs{}
	sender := &fakeSender{messageID: "<abc@vcs.com.vn>", err: sendErr}
	svc := NewSendService(reports, jobs, sender, &fakeRenderer{}, excel.NewGenerator(), loc, zerolog.New(io.Discard))
	return svc, jobs, sender
}

func sampleTotals() *repository.Totals {
	return &repository.Totals{
		TotalServers: 3, AvgUptimePct: ptr(99.0),
		SumActualChecks: 100, SumExpectedChecks: 100,
	}
}

func req() dto.SendReportRequest {
	return dto.SendReportRequest{
		StartDate: "2026-07-16", EndDate: "2026-07-16", RecipientEmail: "ops@vcs.com.vn",
	}
}

func TestSend_WalksStatesToSent(t *testing.T) {
	svc, jobs, sender := newTestSendService(nil)

	resp, err := svc.Send(context.Background(), req(), model.TypeOnDemand, "u1", "key-1")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	want := []string{model.StateProcessing, model.StateGenerated, model.StateSending, model.StateSent}
	if fmt.Sprint(jobs.states) != fmt.Sprint(want) {
		t.Errorf("states = %v, want %v", jobs.states, want)
	}
	if resp.State != model.StateSent {
		t.Errorf("State = %q, want sent", resp.State)
	}
	if jobs.sentID != sender.messageID {
		t.Errorf("stored message id = %q, want %q", jobs.sentID, sender.messageID)
	}
	if sender.sent.To != "ops@vcs.com.vn" {
		t.Errorf("sent to %q", sender.sent.To)
	}
}

// An ambiguous send must land in delivery_unknown, never failed: the mail may
// have arrived, so a retry could duplicate it.
func TestSend_AmbiguousDeliveryBecomesDeliveryUnknown(t *testing.T) {
	svc, jobs, _ := newTestSendService(fmt.Errorf("%w: connection reset", email.ErrAmbiguousDelivery))

	resp, err := svc.Send(context.Background(), req(), model.TypeOnDemand, "u1", "")
	if err != nil {
		t.Fatalf("Send returned an error: %v", err)
	}

	if jobs.failed != model.StateDeliveryUnknown {
		t.Errorf("state = %q, want delivery_unknown", jobs.failed)
	}
	if resp.State != model.StateDeliveryUnknown {
		t.Errorf("response state = %q, want delivery_unknown", resp.State)
	}
	// The Message-ID is retained precisely because the outcome is unknown.
	if jobs.msgID == "" {
		t.Error("expected the Message-ID to be stored for an unknown delivery")
	}
}

// A clear SMTP failure is `failed`: nothing was delivered.
func TestSend_ClearFailureBecomesFailed(t *testing.T) {
	svc, jobs, _ := newTestSendService(errors.New("auth failed"))

	if _, err := svc.Send(context.Background(), req(), model.TypeOnDemand, "u1", ""); err != nil {
		t.Fatalf("Send returned an error: %v", err)
	}

	if jobs.failed != model.StateFailed {
		t.Errorf("state = %q, want failed", jobs.failed)
	}
}

func TestSend_DisallowedRecipientIsReturnedAsError(t *testing.T) {
	svc, _, _ := newTestSendService(email.ErrRecipientNotAllowed)

	_, err := svc.Send(context.Background(), req(), model.TypeOnDemand, "u1", "")

	if !errors.Is(err, email.ErrRecipientNotAllowed) {
		t.Fatalf("err = %v, want ErrRecipientNotAllowed", err)
	}
}

func TestSend_InvalidRangeCreatesNoJob(t *testing.T) {
	svc, jobs, _ := newTestSendService(nil)
	bad := req()
	// Send validates against the real clock, so "today" must come from it too;
	// a hardcoded date silently turns valid once that day passes.
	bad.EndDate = time.Now().In(loc).Format(time.DateOnly)

	_, err := svc.Send(context.Background(), bad, model.TypeOnDemand, "u1", "")

	if !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("err = %v, want ErrInvalidRange", err)
	}
	if jobs.created != nil {
		t.Error("a job was created for an invalid range")
	}
}

// A failed report leaves a job row behind, so the failure is on record.
func TestSend_SummaryFailureMarksJobFailed(t *testing.T) {
	reports := newTestService(&fakeSnapshots{totalsErr: errBoom})
	jobs := &fakeJobs{}
	svc := NewSendService(reports, jobs, &fakeSender{}, &fakeRenderer{}, excel.NewGenerator(), loc, zerolog.New(io.Discard))

	if _, err := svc.Send(context.Background(), req(), model.TypeOnDemand, "u1", ""); err == nil {
		t.Fatal("expected an error")
	}

	if jobs.created == nil {
		t.Fatal("expected the job to exist even though the report failed")
	}
	if jobs.failed != model.StateFailed {
		t.Errorf("state = %q, want failed", jobs.failed)
	}
}

func TestSend_RenderFailureMarksJobFailed(t *testing.T) {
	reports := newTestService(&fakeSnapshots{totals: sampleTotals()})
	jobs := &fakeJobs{}
	svc := NewSendService(reports, jobs, &fakeSender{}, &fakeRenderer{err: errBoom}, excel.NewGenerator(), loc, zerolog.New(io.Discard))

	if _, err := svc.Send(context.Background(), req(), model.TypeOnDemand, "u1", ""); err == nil {
		t.Fatal("expected an error")
	}
	if jobs.failed != model.StateFailed {
		t.Errorf("state = %q, want failed", jobs.failed)
	}
}

func TestSend_RecordsRequesterAndKey(t *testing.T) {
	svc, jobs, _ := newTestSendService(nil)

	if _, err := svc.Send(context.Background(), req(), model.TypeDaily, "u42", "key-9"); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if jobs.created.RequesterID != "u42" || jobs.created.IdempotencyKey != "key-9" {
		t.Errorf("job = %+v, want the requester and key recorded", jobs.created)
	}
	if jobs.created.ReportType != model.TypeDaily {
		t.Errorf("ReportType = %q, want daily", jobs.created.ReportType)
	}
}

func TestGet_DecodesStoredSummary(t *testing.T) {
	svc, jobs, _ := newTestSendService(nil)
	if _, err := svc.Send(context.Background(), req(), model.TypeOnDemand, "u1", ""); err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	jobs.created.State = model.StateSent

	got, err := svc.Get(context.Background(), jobs.created.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.Summary == nil {
		t.Fatal("expected the stored summary to be decoded")
	}
	if got.Summary.EndDate != "2026-07-16" {
		t.Errorf("summary end date = %q", got.Summary.EndDate)
	}
}
