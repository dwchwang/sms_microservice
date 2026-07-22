package email

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"

	"github.com/vcs-sms/report-service/internal/dto"
)

//go:embed templates/daily_report.html
var dailyReportHTML string

// Renderer turns a report summary into an email body.
type Renderer interface {
	Render(summary *dto.SummaryResponse) (subject, html string, err error)
}

type renderer struct {
	tmpl *template.Template
}

// NewRenderer compiles the report template.
func NewRenderer() (Renderer, error) {
	tmpl, err := template.New("daily_report").Funcs(template.FuncMap{
		"pct":      formatPct,
		"optPct":   formatOptPct,
		"thousand": formatThousand,
		"date":     formatDate,
	}).Parse(dailyReportHTML)
	if err != nil {
		return nil, fmt.Errorf("failed to parse report template: %w", err)
	}
	return &renderer{tmpl: tmpl}, nil
}

// Render builds the subject and HTML body.
func (r *renderer) Render(summary *dto.SummaryResponse) (string, string, error) {
	subject := fmt.Sprintf("Báo cáo VCS-SMS — ngày %s", formatDate(summary.EndDate))
	if summary.StartDate != summary.EndDate {
		subject = fmt.Sprintf("Báo cáo VCS-SMS — %s đến %s",
			formatDate(summary.StartDate), formatDate(summary.EndDate))
	}

	var buf bytes.Buffer
	if err := r.tmpl.Execute(&buf, summary); err != nil {
		return "", "", fmt.Errorf("failed to render report template: %w", err)
	}
	return subject, buf.String(), nil
}

// formatDate turns 2026-07-15 into 15/07/2026.
func formatDate(iso string) string {
	if len(iso) != 10 {
		return iso
	}
	return fmt.Sprintf("%s/%s/%s", iso[8:10], iso[5:7], iso[0:4])
}

func formatPct(v float64) string {
	return fmt.Sprintf("%.1f%%", v)
}

// formatOptPct renders a dash rather than 0% when uptime is unknown, so a
// collection gap never reads as an outage.
func formatOptPct(v *float64) string {
	if v == nil {
		return "—"
	}
	return formatPct(*v)
}

func formatThousand(n int64) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, c)
	}
	return string(out)
}
