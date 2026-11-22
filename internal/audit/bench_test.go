package audit

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// BenchmarkAudit audits a 1000-line crontab of daily, weekly, monthly and frequent jobs.
func BenchmarkAudit(b *testing.B) {
	shapes := []string{"%d %d * * *", "%d %d * * 1", "%d %d 1 * *", "*/15 %d-%d * * *", "%d %d * * 1-5"}
	var sb strings.Builder
	sb.WriteString("PATH=/usr/bin:/bin\nSHELL=/bin/sh\n")
	for i := range 1000 {
		fmt.Fprintf(&sb, shapes[i%len(shapes)]+" /usr/local/bin/job%d >> /var/log/job.log 2>&1\n", i%60, i%24, i)
	}
	text := sb.String()
	o := Options{Location: time.UTC, From: from}
	for b.Loop() {
		Run(text, o)
	}
}
