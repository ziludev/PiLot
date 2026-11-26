package app

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pilot/internal/i18n"
	"pilot/internal/protocol"
)

// relTime places a conversation in time. An unknown time draws nothing: a
// wrong "just now" is worse than a blank rail.
func relTime(now time.Time, ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	age := now.Sub(ts)
	switch {
	case age < time.Minute:
		return i18n.T("justNow")
	case age < time.Hour:
		return i18n.TF("minutesAgo", map[string]any{"n": int(age / time.Minute)})
	case age < 24*time.Hour:
		return i18n.TF("hoursAgo", map[string]any{"n": int(age / time.Hour)})
	default:
		return i18n.TF("daysAgo", map[string]any{"n": int(age / (24 * time.Hour))})
	}
}

// clockTime is a task's next fire, which is a point in time and not a distance.
func clockTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Local().Format("15:04")
}

// shortDuration writes a span the way the step summary wants it: whole seconds
// under a minute, then minutes and seconds.
func shortDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
	if d < time.Hour {
		seconds := int(d/time.Second) % 60
		if seconds == 0 {
			return fmt.Sprintf("%dm", int(d/time.Minute))
		}
		return fmt.Sprintf("%dm%ds", int(d/time.Minute), seconds)
	}
	return fmt.Sprintf("%dh", int(d/time.Hour))
}

// oneLine flattens whitespace so a summary fits on one rail or step line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// clip cuts s to at most n runes, marking the cut.
func clip(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 1 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}

func itoa(n int) string { return strconv.Itoa(n) }

func unixTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func lowerTrim(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), needle)
}

func joinLines(lines []string) string { return strings.TrimSpace(strings.Join(lines, "\n")) }

func trimSpaceBytes(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\n' || b[0] == '\t' || b[0] == '\r') {
		b = b[1:]
	}
	for len(b) > 0 {
		last := b[len(b)-1]
		if last != ' ' && last != '\n' && last != '\t' && last != '\r' {
			break
		}
		b = b[:len(b)-1]
	}
	return b
}

func decodeBase64(s string) []byte {
	if s == "" {
		return nil
	}
	// The bridge may hand back a data URL; the payload follows the comma.
	if i := strings.IndexByte(s, ','); i >= 0 && strings.HasPrefix(s, "data:") {
		s = s[i+1:]
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return data
}

// money is a run's price in dollars: three decimals below a dollar, where the
// difference between two routines lives, and two above it.
func money(spend *protocol.TaskSpend) string {
	if spend == nil || spend.Cost <= 0 {
		return ""
	}
	if spend.Cost < 1 {
		return fmt.Sprintf("$%.3f", spend.Cost)
	}
	return fmt.Sprintf("$%.2f", spend.Cost)
}

// dollars is a bare amount in the same shape as money: the usage calendar has
// costs without a task behind them.
func dollars(cost float64) string {
	if cost <= 0 {
		return "$0"
	}
	if cost < 1 {
		return fmt.Sprintf("$%.3f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

// dayKey names the calendar day an instant falls on in the reader's zone. It
// is the string the transcript keys its date rules by, so two entries minutes
// apart across midnight still get a rule between them.
func dayKey(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Local().Format("2006-01-02")
}

// dayLabel is what the rule above a day's messages says: "today" and
// "yesterday" while those words are still shorter than a date, a date after.
func dayLabel(ts time.Time) string {
	now := time.Now()
	local := ts.Local()
	ny, nm, nd := now.Date()
	y, m, d := local.Date()
	yesterday := now.AddDate(0, 0, -1)
	yy, ym, yd := yesterday.Date()
	switch {
	case y == ny && m == nm && d == nd:
		return i18n.T("today")
	case y == yy && m == ym && d == yd:
		return i18n.T("yesterday")
	case y == ny:
		return i18n.TF("monthDay", map[string]any{"m": int(m), "d": d})
	default:
		return i18n.TF("yearMonthDay", map[string]any{"y": y, "m": int(m), "d": d})
	}
}
