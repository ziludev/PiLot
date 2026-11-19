package app

// The usage calendar: what the last four months cost.
//
// Spend is the one number this app has that nobody can check by looking at the
// screen — a chat is a transcript, a task is a row, but a month of work is a
// shape. The bridge samples it per day; this draws those samples as the weeks
// they fell in, so a heavy Tuesday is visible as a darker square rather than as
// a line in a table.
//
// It lives behind a calendar button rather than in a tab: it answers "how much
// has this cost me" once a week, not every minute.

import (
	"context"
	"sort"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
	"pitta/internal/theme"
)

const (
	// usageWeeks is how far back the grid goes: 17 weeks fits a 460-point
	// dialog at a 14-point cell and covers a quarter.
	usageWeeks = 17
	// usageDays is what the bridge is asked for: a week more than the grid, so
	// the first column is full whatever day of the week today is.
	usageDays = usageWeeks*7 + 7
)

type usagePanel struct {
	open bool
	seq  int

	busy   bool
	failed string
	days   []api.UsageDay
}

// toggleUsage opens the calendar, or closes it; opening reads the numbers.
func (a *App) toggleUsage() {
	a.usage.open = !a.usage.open
	if a.usage.open {
		a.loadUsage()
	}
	a.invalidate()
}

// loadUsage asks the bridge for the daily samples. seq retires a reply for a
// grid that was closed and reopened while it was in flight.
func (a *App) loadUsage() {
	a.usage.busy = true
	seq := a.usage.seq
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
		defer cancel()
		days, err := a.api.UsageCalendar(ctx, usageDays)
		a.post(event{kind: evUsage, seq: seq, usage: days, err: err})
	}()
}

// applyUsage fills the calendar, unless the panel moved on.
func (a *App) applyUsage(seq int, days []api.UsageDay, err error) {
	if seq != a.usage.seq || !a.usage.open {
		return
	}
	a.usage.busy = false
	if err != nil {
		a.usage.failed = err.Error()
		a.invalidate()
		return
	}
	a.usage.failed = ""
	a.usage.days = days
	a.invalidate()
}

// usageDialog draws the calendar while it is up.
func (a *App) usageDialog(c *ui.Context, t theme.Tokens) {
	if !a.usage.open {
		return
	}
	ui.Modal(c, &a.usage.open, func() {
		ui.Column(c).Width(460).Padding(16).Gap(12).Radius(10).
			Background(t.Popover).Border(1, t.Border).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("usage")).FontSize(12.5).FontWeight(600).
					TextColor(t.Foreground).Grow(1)
				start, _ := usageWindow(a.now)
				ui.Text(c, start.Format("01-02")+" – "+a.now.Format("01-02")).FontSize(9.5).
					TextColor(t.MutedFg)
			})
			if a.usage.failed != "" {
				ui.Text(c, a.usage.failed).FontSize(11).TextColor(t.Destructive).MaxLines(3)
			}
			if a.usage.days == nil {
				if a.usage.busy {
					ui.Text(c, i18n.T("loading")).FontSize(11).TextColor(t.MutedFg)
				}
				return
			}
			a.usageGrid(c, t)
			a.usageTotals(c, t)
			ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
				a.dialogButton(c, t, i18n.T("close"), true, func() {
					a.usage.open = false
				})
			})
		})
	})
}

// usageGrid is the heat map: 7 rows of weeks, oldest week on the left, so a
// column reads as one week Monday to Sunday.
func (a *App) usageGrid(c *ui.Context, t theme.Tokens) {
	byDate := make(map[string]api.UsageDay, len(a.usage.days))
	peak := 0.0
	for _, day := range a.usage.days {
		byDate[day.Date] = day
		if day.Cost > peak {
			peak = day.Cost
		}
	}
	start, _ := usageWindow(a.now)

	ui.Row(c).FillWidth().Gap(4).Children(func() {
		// Weekday labels: three letters, every other row, because seven labels
		// next to 10-point squares is more ink than grid.
		ui.Column(c).Gap(3).Padding(0, 0, 0, 2).Children(func() {
			for row := 0; row < 7; row++ {
				label := ""
				switch row {
				case 0:
					label = i18n.T("weekdayMon")
				case 2:
					label = i18n.T("weekdayWed")
				case 4:
					label = i18n.T("weekdayFri")
				}
				ui.Text(c, label).FontSize(8.5).TextColor(t.MutedFg).Height(15)
			}
		})
		for week := 0; week < usageWeeks; week++ {
			first := start.AddDate(0, 0, week*7)
			ui.Column(c).Gap(3).Children(func() {
				for row := 0; row < 7; row++ {
					date := first.AddDate(0, 0, row)
					day, ok := byDate[date.Format("2006-01-02")]
					a.usageCell(c, t, date, day.Cost, ok, peak)
				}
			})
		}
	})
}

// usageCell is one day: empty when nothing was spent, and four steps of the
// accent color up to the heaviest day in the window.
func (a *App) usageCell(c *ui.Context, t theme.Tokens, date time.Time, cost float64, ok bool, peak float64) {
	cell := ui.Box(c).Width(cellSize).Height(cellSize).Radius(3).Background(t.SidebarAccent)
	switch {
	case !ok || cost <= 0:
		// Nothing spent, or nothing sampled: the quiet square says both.
	case peak <= 0:
		cell = cell.Background(blend(t.Popover, t.Primary, 0.2))
	default:
		share := cost / peak
		level := 0.2
		switch {
		case share > 0.66:
			level = 0.86
		case share > 0.33:
			level = 0.6
		case share > 0.12:
			level = 0.4
		}
		// The shade is mixed rather than layered with alpha: a translucent
		// square over a popover loses its definition on a photo wallpaper, and
		// the whole point of the grid is telling one square from another.
		cell = cell.Background(blend(t.Popover, t.Primary, level))
	}
	// The day's number is in the tooltip rather than on the square: at 11 points
	// a cell holds a color, not a figure.
	label := date.Format("01-02")
	if ok && cost > 0 {
		label += " · " + dollars(cost)
	}
	cell.Tooltip(label)
}

// usageTotals is the summary under the grid: what the window cost, what it
// bought, and the day that stands out.
func (a *App) usageTotals(c *ui.Context, t theme.Tokens) {
	total, tokens, best := 0.0, int64(0), api.UsageDay{}
	for _, day := range a.usage.days {
		total += day.Cost
		tokens += day.Tokens
		if day.Cost > best.Cost {
			best = day
		}
	}
	ui.Row(c).FillWidth().Gap(18).Padding(0, 0, 0, 2).Children(func() {
		a.usageStat(c, t, i18n.T("usageCost"), dollars(total))
		a.usageStat(c, t, i18n.T("usageTokens"), compactTokens(tokens))
		if best.Date != "" {
			a.usageStat(c, t, i18n.T("usagePeak"), best.Date[5:]+" · "+dollars(best.Cost))
		}
	})
}

func (a *App) usageStat(c *ui.Context, t theme.Tokens, label, value string) {
	ui.Column(c).Gap(1).Children(func() {
		ui.Text(c, label).FontSize(9).TextColor(t.MutedFg)
		ui.Text(c, value).FontSize(11.5).FontWeight(600).TextColor(t.Foreground)
	})
}

// cellSize is how wide one day is; the grid is 17 of these plus gaps.
const cellSize = 17

// blend mixes two colors by share (0 = from, 1 = to). The calendar needs shades
// between the text color and the surface, and does not want alpha to get them.
func blend(from, to ui.Color, share float64) ui.Color {
	mix := func(a, b uint8) uint8 {
		return uint8(float64(a) + (float64(b)-float64(a))*share + 0.5)
	}
	return ui.RGB(mix(from.R, to.R), mix(from.G, to.G), mix(from.B, to.B))
}

// usageWindow is the grid's first and last day: it ends on this week's Sunday so
// today sits where it belongs, and starts 17 weeks earlier.
func usageWindow(now time.Time) (time.Time, time.Time) {
	end := startOfDay(now)
	for end.Weekday() != time.Sunday {
		end = end.AddDate(0, 0, 1)
	}
	return end.AddDate(0, 0, -(usageWeeks*7 - 1)), end
}

// startOfDay drops the clock, so the grid is built from dates rather than from
// whatever second the window happened to open.
func startOfDay(at time.Time) time.Time {
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
}

// compactTokens is a token count at a width a summary can hold: 4.1M rather
// than 4101630, which nobody reads past the first digit.
func compactTokens(n int64) string {
	switch {
	case n >= 1000000:
		return trimZero(float64(n)/1000000) + "M"
	case n >= 1000:
		return trimZero(float64(n)/1000) + "K"
	}
	return itoa(int(n))
}

// trimZero prints one decimal, and none when it is .0.
func trimZero(v float64) string {
	rounded := int(v*10 + 0.5)
	whole, tenth := rounded/10, rounded%10
	if tenth == 0 {
		return itoa(whole)
	}
	return itoa(whole) + "." + itoa(tenth)
}

// usageSorted is the days oldest first, which is what the totals and any future
// chart want regardless of how the bridge ordered them.
func usageSorted(days []api.UsageDay) []api.UsageDay {
	out := append([]api.UsageDay(nil), days...)
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}
