package rules

import (
	"time"

	du "vedhelper/internal/docutil"
	"vedhelper/internal/model"
	"vedhelper/internal/refdata"
)

func parseDate(s string) string { return du.ParseDate(s) }

func toDate(s string) (time.Time, bool) {
	iso := parseDate(s)
	if iso == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", iso)
	return t, err == nil
}

func isBusiness(d time.Time) bool { return d.Weekday() != time.Saturday && d.Weekday() != time.Sunday }

func nextBusiness(d time.Time) time.Time {
	d = d.AddDate(0, 0, 1)
	for !isBusiness(d) {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

func cutoffFor(cur string) string {
	c := refdata.TariffsData().Cutoff
	if v, ok := c[cur]; ok && v.Time != "" {
		return v.Time
	}
	if v, ok := c["USD"]; ok && v.Time != "" {
		return v.Time
	}
	return "15:00"
}

// ValueDates: срочный — сегодня, если рабочий день и до cutoff, иначе следующий рабочий день;
// обычный — следующий рабочий день после дня обработки. now нулевой — время не учитывается.
// today пустой — дата берётся из now (или системная).
func ValueDates(currency, today string, now time.Time) model.ValueDates {
	d, ok := toDate(today)
	if !ok {
		base := now
		if base.IsZero() {
			base = time.Now()
		}
		d = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, time.UTC)
	}
	cutoff := cutoffFor(currency)
	after := false
	if !now.IsZero() {
		after = now.Format("15:04") > cutoff
	}
	base := d
	if !isBusiness(d) || after {
		base = nextBusiness(d)
	}
	return model.ValueDates{Standard: nextBusiness(base).Format("2006-01-02"), Urgent: base.Format("2006-01-02"), Cutoff: cutoff}
}

func todayISO(caseToday string) string {
	if t := parseDate(caseToday); t != "" {
		return t
	}
	return time.Now().Format("2006-01-02")
}

var timeZero time.Time
