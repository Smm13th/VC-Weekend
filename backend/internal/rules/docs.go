package rules

import (
	"fmt"
	"regexp"
	"strings"

	du "vedhelper/internal/docutil"
	"vedhelper/internal/model"
)

func placeholderFinding(d *du.Doc, stage model.Stage) *model.Finding {
	var hits []*model.SourceRef
	var names []string
	for _, m := range du.FindAll(du.Placeholder, d.Text) {
		r := d.Ref(m[0], m[1])
		hits = append(hits, &r)
		if !containsStr(names, r.Quote) {
			names = append(names, r.Quote)
		}
	}
	if len(hits) == 0 {
		return nil
	}
	shown := names
	if len(shown) > 8 {
		shown = shown[:8]
	}
	s := strings.Join(shown, ", ")
	if len(names) > 8 {
		s += fmt.Sprintf(" и ещё %d", len(names)-8)
	}
	f := newFinding("placeholder", d.ID, stage, model.Red, "В "+docLabel(d.ParsedDoc)+" не заполнены поля шаблона",
		fmt.Sprintf("В тексте остались заготовки в квадратных скобках (%d шт.): %s. Это шаблон, а не "+
			"подписанный документ. Платить по нему нельзя: попросите у поставщика заполненную и подписанную "+
			"версию с номером, датой, сторонами и банковскими реквизитами.", len(hits), s), hits, fopt{})
	return &f
}

func injectionFinding(d *du.Doc, stage model.Stage) *model.Finding {
	hits := detectInjections(d)
	if len(hits) == 0 {
		return nil
	}
	var srcs []*model.SourceRef
	for i := range hits {
		srcs = append(srcs, &hits[i])
	}
	f := newFinding("hidden_instruction", d.ID, stage, model.Red, "В "+docLabel(d.ParsedDoc)+" спрятана инструкция для помощника",
		"В документе есть текст, обращённый к программе-помощнику, а не к людям: «"+hits[0].Quote+"». "+
			"Часто его набирают мелким или светлым шрифтом, чтобы человек не заметил. Помощник такие "+
			"указания не выполняет и ничего по ним не подтверждает. Такая строка уже повод насторожиться. "+
			"Покажите документ поставщику и службе безопасности банка, прежде чем платить.", srcs, fopt{})
	return &f
}

var reAlnum8 = regexp.MustCompile(`\b[A-Z0-9]{8,11}\b`)

func requisitesFindings(d *du.Doc, ext model.ExtractionResult, stage model.Stage, referenceIBAN string) []model.Finding {
	var out []model.Finding
	lbl := docLabel(d.ParsedDoc)
	ib, hasIB := ext.Fields["sellerIban"]
	sw, hasSW := ext.Fields["sellerSwift"]
	var ibanRes *IBANCheck
	if hasIB {
		ref := referenceIBAN
		if d.Kind == model.KindContract {
			ref = ""
		}
		r := validateIBAN(ib.Value, ref, "в "+lbl, "в договоре")
		ibanRes = &r
		src := []*model.SourceRef{srcp(ib.Source)}
		switch r.Error {
		case "format", "length":
			out = append(out, newFinding("iban_format", d.ID, stage, model.Red, "IBAN в "+lbl+" записан неправильно",
				"Номер счёта получателя "+ib.Value+" не похож на правильный IBAN: "+r.Hint+". "+
					"Перевод с таким номером банк не примет или вернёт. Попросите у поставщика верный номер.",
				src, fopt{field: "beneficiaryIban"}))
		case "checksum":
			tip := ""
			if r.Hint != "" {
				tip = " Похоже на опечатку: " + r.Hint + "."
			}
			if len(r.Candidates) > 0 && referenceIBAN != "" && d.Kind != model.KindContract {
				tip += " Правильный вариант, скорее всего, " + FormatIBAN(r.Candidates[0]) + ", но подтвердите его у поставщика."
			}
			out = append(out, newFinding("iban_checksum", d.ID, stage, model.Red, "В IBAN в "+lbl+" опечатка",
				"У номера "+ib.Value+" не сходятся контрольные цифры: такого счёта не существует, "+
					"перевод вернётся, а банк возьмёт комиссию."+tip, src, fopt{field: "beneficiaryIban"}))
		default:
			out = append(out, newFinding("iban_checksum", d.ID, stage, model.Green, "IBAN в "+lbl+" проходит проверку",
				"Номер "+FormatIBAN(ib.Value)+" правильной длины для страны "+countryName(r.Country)+
					", контрольные цифры сходятся.", src, fopt{field: "beneficiaryIban"}))
		}
	}
	if hasSW {
		s := ValidateSwift(sw.Value)
		if !s.OK {
			out = append(out, newFinding("swift_format", d.ID, stage, model.Red, "SWIFT в "+lbl+" записан неправильно",
				"Код банка "+sw.Value+" не похож на SWIFT: нужно 8 или 11 символов, сначала 4 буквы банка, "+
					"2 буквы страны, потом 2 символа города и, если есть, 3 символа филиала.",
				[]*model.SourceRef{srcp(sw.Source)}, fopt{field: "beneficiarySwift"}))
		} else {
			if ibanRes != nil && ibanRes.OK && s.Country != ibanRes.Country {
				out = append(out, newFinding("swift_iban_country", d.ID, stage, model.Red, "SWIFT и IBAN в "+lbl+" из разных стран",
					"По коду SWIFT "+s.Normalized+" банк находится в стране "+countryName(s.Country)+", а IBAN открыт в стране "+
						countryName(ibanRes.Country)+". Так не бывает для одного счёта: уточните реквизиты у поставщика.",
					[]*model.SourceRef{srcp(sw.Source), srcp(ib.Source)}, fopt{field: "beneficiarySwift"}))
			}
			if s.Bank != nil {
				out = append(out, newFinding("swift_format", d.ID, stage, model.Green, "SWIFT в "+lbl+" найден в справочнике",
					"Код "+s.Normalized+" принадлежит банку "+s.Bank.Name+", "+s.Bank.City+".",
					[]*model.SourceRef{srcp(sw.Source)}, fopt{field: "beneficiarySwift"}))
			}
		}
	}
	if inter, ok := ext.Fields["intermediaryBank"]; ok && strings.Contains(strings.ToUpper(inter.Value), "SWIFT") {
		parts := strings.Split(inter.Value, "SWIFT")
		if m := reAlnum8.FindString(parts[len(parts)-1]); m != "" && !ValidateSwift(m).OK {
			out = append(out, newFinding("swift_format", d.ID, stage, model.Red, "SWIFT банка-посредника в "+lbl+" неправильный",
				"Код "+m+" не похож на SWIFT. Уточните банк-посредник у поставщика.",
				[]*model.SourceRef{srcp(inter.Source)}, fopt{field: "intermediaryBank", extra: "intermediary"}))
		}
	}
	return out
}
