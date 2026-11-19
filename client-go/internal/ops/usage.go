package ops

import (

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// UsageProps is the spend over the last usageDays days, oldest first.
type UsageProps struct {
	Days []protocol.UsageDay
}

// usageDays is how far back the calendar reaches: about four months of spend,
// which is the window the client's grid was sized for.
const usageDays = 120

// usageColors is how many shaded steps sit between a quiet day and a peak one.
const usageColors = 4

// UsageCalendar draws the recent spend as a heat grid: a row per week, a cell
// per day, shaded by what that day cost.
func UsageCalendar(c *ui.Context, p UsageProps) {
	t := theme.Current(c)
	days := p.Days
	if len(days) > usageDays {
		days = days[len(days)-usageDays:]
	}
	totalCost, totalTokens, peakCost, peakDate := usageTotals(days)
	span := ""
	if len(days) > 0 {
		first, last := days[0].Date, days[len(days)-1].Date
		if len(first) >= 10 && len(last) >= 10 {
			span = first[5:10] + " → " + last[5:10]
		}
	}

	ui.Column(c).FillWidth().Grow(1).Children(func() {
		heading(c, t, icons.Clock, i18n.T("usage"), "", nil)
		ui.Row(c).FillWidth().Padding(0, 12, 10, 12).Gap(18).Children(func() {
			usageStat(c, t, i18n.T("cost"), money(totalCost))
			usageStat(c, t, tokensLabel, tokenCount(totalTokens))
			usageStat(c, t, peakLabel+" "+peakDate, money(peakCost))
		})
		ui.Scroll(c).FillWidth().Grow(1).Padding(0, 12).Children(func() {
			ui.Column(c).FillWidth().Gap(8).PaddingY(2).Children(func() {
				card(c, t).Children(func() {
					ui.Grid(c).Columns(7).FillWidth().GapX(3).GapY(3).Children(func() {
						for _, day := range days {
							usageCell(c, t, day, peakCost)
						}
					})
				})
				usageLegend(c, t, span)
			})
		})
	})
}

// usageTotals sums the window and finds the day that cost the most, which sets
// the top of the color scale.
func usageTotals(days []protocol.UsageDay) (totalCost float64, totalTokens int, peakCost float64, peakDate string) {
	for _, day := range days {
		totalCost += day.Cost
		totalTokens += day.Tokens
		if day.Cost > peakCost {
			peakCost = day.Cost
			peakDate = day.Date
		}
	}
	if peakDate != "" && len(peakDate) >= 10 {
		peakDate = peakDate[5:10]
	}
	return totalCost, totalTokens, peakCost, peakDate
}

func usageStat(c *ui.Context, t theme.Tokens, label, value string) {
	ui.Column(c).Shrink(0).Gap(1).Children(func() {
		caption(c, t, label)
		ui.Text(c, value).FontSize(13).FontWeight(600).TextColor(t.Foreground).MaxLines(1)
	})
}

// usageCell is one day. Only the first of a month carries its date, because a
// label in every cell would not fit the grid.
func usageCell(c *ui.Context, t theme.Tokens, day protocol.UsageDay, peakCost float64) {
	level := usageLevel(day.Cost, peakCost)
	cell := ui.Box(c).Height(22).Radius(3).Center().Background(usageColor(t, level))
	if level == 0 {
		cell.Border(1, t.Border)
	}
	cell.Tooltip(day.Date + " · " + moneyFine(day.Cost) + " · " + tokenCount(day.Tokens) + " " + tokensLabel)
}

// usageLevel buckets a day against the window's peak, so the grid keeps its
// contrast whatever the project spends in total.
func usageLevel(cost, peakCost float64) int {
	if cost <= 0 || peakCost <= 0 {
		return 0
	}
	switch ratio := cost / peakCost; {
	case ratio < 0.25:
		return 1
	case ratio < 0.5:
		return 2
	case ratio < 0.8:
		return 3
	}
	return usageColors
}

// usageColor shades a cell from the accent up to the primary tone.
func usageColor(t theme.Tokens, level int) ui.Color {
	if level <= 0 {
		return t.Muted
	}
	return t.Accent.Mix(t.Primary, float32(level)/float32(usageColors))
}

// usageText keeps the date legible on either end of the scale.
func usageText(t theme.Tokens, level int) ui.Color {
	if level >= 3 {
		return t.PrimaryFg
	}
	return t.MutedFg
}

// usageLegend is the foot of the grid: the window it covers on the left, the
// ramp on the right. Dates used to sit inside the cells of the 1st of each
// month, where they overflowed a 22px cell and covered the day behind them.
func usageLegend(c *ui.Context, t theme.Tokens, span string) {
	ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, span).FontSize(9).TextColor(t.MutedFg)
		ui.Spacer(c)
		ui.Text(c, i18n.T("less")).FontSize(9).TextColor(t.MutedFg)
		for level := 0; level <= usageColors; level++ {
			swatch := ui.Box(c).Size(10, 10).Radius(2).Background(usageColor(t, level))
			if level == 0 {
				swatch.Border(1, t.Border)
			}
		}
		ui.Text(c, i18n.T("more")).FontSize(9).TextColor(t.MutedFg)
	})
}

