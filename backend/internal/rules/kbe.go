package rules

import (
	"strings"

	"vedhelper/internal/model"
	"vedhelper/internal/refdata"
)

var (
	kbeBank  = rxi(wbL + `(bank|banque|banca|банк)` + wbR)
	kbeGov   = rxi(wbL + `(ministry|government|министерств|акимат|казначейств|treasury)`)
	kbeFin   = rxi(wbL + `(insurance|leasing|investment fund|страхов|лизинг|microfinance|микрофинанс)`)
	kbeNpo   = rxi(wbL + `(foundation|charity|non-profit|фонд|благотворител)`)
	kbeLegal = rxi(wbL + `(LLC|LLP|FZE|FZCO|FZ-LLC|LTD|LIMITED|JSC|PJSC|INC|CORP|GMBH|AG|SA|SRL|BV|PLC|CO|ТОО|АО|ООО|ЗАО|ПАО|ИП)` + wbR +
		`|company|trading|machinery|industries`)
)

type kbeData struct {
	Residency []struct{ Digit, Title string } `json:"residency"`
	Sectors   []struct{ Digit, Title string } `json:"sectors"`
}

// SuggestKbe предлагает код бенефициара: резидентство + сектор экономики.
func SuggestKbe(country, name string) model.KbeInfo {
	country = upper(country)
	data := refdata.Load[kbeData]("kbe")
	res := "2"
	if country == "KZ" {
		res = "1"
	}
	var sec string
	switch {
	case kbeBank.MatchString(name):
		sec = "4"
	case kbeGov.MatchString(name):
		sec = "1"
	case kbeFin.MatchString(name):
		sec = "5"
	case kbeNpo.MatchString(name):
		sec = "8"
	case kbeLegal.MatchString(name) || name == "":
		sec = "7"
	default:
		sec = "9"
	}
	resTitle, secTitle := "", ""
	for _, r := range data.Residency {
		if r.Digit == res {
			resTitle = r.Title
		}
	}
	for _, s := range data.Sectors {
		if s.Digit == sec {
			secTitle = s.Title
		}
	}
	cname := country
	for _, c := range refdata.Countries() {
		if c.Code == country {
			cname = c.Name
		}
	}
	where := "страна не определена"
	if country != "" {
		where = "зарегистрирован в стране " + cname
	}
	why := "Получатель " + where + ", поэтому первая цифра " + res + ": " + strings.ToLower(resTitle) + ". " +
		"Вторая цифра " + sec + ": " + strings.ToLower(secTitle)
	if name != "" {
		why += ", судя по названию «" + name + "»."
	} else {
		why += "."
	}
	why += " Сверьте код со справочником Национального Банка РК."
	return model.KbeInfo{Code: res + sec, Why: why, MentorCheck: true}
}
