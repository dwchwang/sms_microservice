package email

import (
	"strings"
	"testing"

	"github.com/vcs-sms/report-service/internal/dto"
	"github.com/vcs-sms/report-service/internal/model"
	"github.com/vcs-sms/report-service/internal/repository"
)

func ptr(v float64) *float64 { return &v }

func summary() *dto.SummaryResponse {
	return &dto.SummaryResponse{
		StartDate: "2026-07-15", EndDate: "2026-07-15",
		TotalServers: 10000, ServersOnAtEndAt: 9940, ServersOffAtEndAt: 58,
		AvgUptimePct:     ptr(99.2),
		ServersUptime100: 9512, ServersUptimePartial: 428,
		ServersUptime0: 60, ServersNoData: 2,
		CoveragePct: 99.8,
		Top10LowestUptime: []repository.LowUptimeServer{
			{ServerID: "SRV-04412", ServerName: "web-prod-12", UptimePct: 0},
		},
	}
}

func render(t *testing.T, s *dto.SummaryResponse) (string, string) {
	return renderAs(t, s, model.TypeOnDemand)
}

func renderAs(t *testing.T, s *dto.SummaryResponse, reportType string) (string, string) {
	t.Helper()
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer failed: %v", err)
	}
	subject, html, err := r.Render(s, reportType)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	return subject, html
}

func TestRender_ContainsTheFourRequiredFigures(t *testing.T) {
	_, html := render(t, summary())

	// The brief asks for exactly these four; everything else is context.
	for _, want := range []string{"10.000", "9.940", "58", "99.2%"} {
		if !strings.Contains(html, want) {
			t.Errorf("email is missing %q", want)
		}
	}
}

func TestRender_LabelsTheOnOffSnapshotTime(t *testing.T) {
	_, html := render(t, summary())

	// Without the label, a reader takes ON/OFF as representing the whole day.
	if !strings.Contains(html, "23:59:59") {
		t.Error("ON/OFF must be labelled with the instant they describe")
	}
}

func TestRender_ShowsCoverage(t *testing.T) {
	_, html := render(t, summary())

	if !strings.Contains(html, "99.8%") {
		t.Error("email is missing the coverage percentage")
	}
}

// The uptime distribution was dropped from the report: the brief asks for
// average uptime and the worst servers, not a breakdown by bucket.
func TestRender_OmitsUptimeDistribution(t *testing.T) {
	_, html := render(t, summary())

	for _, unwanted := range []string{"Phân bố uptime", "Uptime một phần", "Không có dữ liệu"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("email still renders %q", unwanted)
		}
	}
}

func TestRender_DegradedShowsWarning(t *testing.T) {
	s := summary()
	s.Degraded = true
	s.CoveragePct = 82.3

	_, html := render(t, s)

	if !strings.Contains(html, "CẢNH BÁO") || !strings.Contains(html, "82.3%") {
		t.Error("a degraded report must warn that the data may be incomplete")
	}
}

func TestRender_HealthyHasNoWarning(t *testing.T) {
	_, html := render(t, summary())

	if strings.Contains(html, "CẢNH BÁO") {
		t.Error("a healthy report must not warn")
	}
}

// Unknown uptime shows a dash, not 0%, so a gap never reads as an outage.
func TestRender_NullAverageShowsDash(t *testing.T) {
	s := summary()
	s.AvgUptimePct = nil
	s.Top10LowestUptime = nil // a 0% server would legitimately print 0.0%

	_, html := render(t, s)

	if strings.Contains(html, "0.0%") {
		t.Error("a null average must not render as 0%")
	}
	if !strings.Contains(html, "—") {
		t.Error("expected a dash for an unknown average")
	}
}

func TestRender_SubjectSingleDay(t *testing.T) {
	subject, _ := render(t, summary())

	if !strings.Contains(subject, "15/07/2026") {
		t.Errorf("subject = %q, want the day in dd/mm/yyyy", subject)
	}
}

func TestRender_SubjectDateRange(t *testing.T) {
	s := summary()
	s.StartDate = "2026-07-10"

	subject, _ := render(t, s)

	if !strings.Contains(subject, "10/07/2026") || !strings.Contains(subject, "15/07/2026") {
		t.Errorf("subject = %q, want both ends of the range", subject)
	}
}

// A server name is user-supplied, so it must not be able to inject markup.
func TestRender_EscapesServerName(t *testing.T) {
	s := summary()
	s.Top10LowestUptime = []repository.LowUptimeServer{
		{ServerID: "SRV-1", ServerName: `<script>alert(1)</script>`, UptimePct: 0},
	}

	_, html := render(t, s)

	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("server name was not HTML-escaped")
	}
}

func TestRender_EmptyTopOmitsTheSection(t *testing.T) {
	s := summary()
	s.Top10LowestUptime = nil

	_, html := render(t, s)

	if strings.Contains(html, "uptime thấp nhất") {
		t.Error("the ranking section should be omitted when there is nothing to rank")
	}
}

func TestRender_DailyIsLabelledDistinctly(t *testing.T) {
	subject, html := renderAs(t, summary(), model.TypeDaily)

	if !strings.Contains(subject, "[Daily]") {
		t.Errorf("daily subject = %q, want a [Daily] tag", subject)
	}
	if !strings.Contains(html, "Daily Email") || !strings.Contains(html, "Báo cáo hằng ngày") {
		t.Error("daily body is missing its distinguishing title")
	}
}

func TestRender_OnDemandIsLabelledDistinctly(t *testing.T) {
	subject, html := renderAs(t, summary(), model.TypeOnDemand)

	if !strings.Contains(subject, "[On-demand]") {
		t.Errorf("on-demand subject = %q, want an [On-demand] tag", subject)
	}
	if strings.Contains(html, "Daily Email") {
		t.Error("an on-demand report must not carry the daily title")
	}
	if !strings.Contains(html, "On-demand") {
		t.Error("on-demand body is missing its label")
	}
}

func TestFormatThousand(t *testing.T) {
	cases := map[int64]string{0: "0", 58: "58", 999: "999", 1000: "1.000", 10000: "10.000", 1234567: "1.234.567"}
	for in, want := range cases {
		if got := formatThousand(in); got != want {
			t.Errorf("formatThousand(%d) = %q, want %q", in, got, want)
		}
	}
}
