package rules

import (
	"regexp"

	du "vedhelper/internal/docutil"
	"vedhelper/internal/model"
)

// Шаблоны текста, обращённого к программе-помощнику (prompt injection).
var injPatterns = func() []*regexp.Regexp {
	ps := []string{
		`\b(ignore|disregard|forget|override|bypass)\b[^\n]{0,40}\b(previous|prior|above|earlier|all|any|your)\b[^\n]{0,30}\b(instructions?|prompts?|rules|messages?|guidelines)`,
		`\bnote\s+(to|for)\s+(the\s+)?(ai|assistant|bot|llm|model|chatbot|gpt)\b`,
		`\b(ai|llm|virtual)\s+assistant\s*[:,]`,
		`\bsystem\s+prompt\b`,
		`\byou\s+are\s+(now\s+)?(an?\s+)?(ai|assistant|chatbot|language\s+model|llm)\b`,
		`\b(as\s+an?\s+ai|dear\s+(ai|assistant|model))\b`,
		`\b(confirm|mark|treat)\b[^\n]{0,60}\bas\s+(the\s+)?(verified|trusted|approved|confirmed)\b`,
		`игнорир\w*[^\n]{0,40}(инструкц|указани|правил)`,
		`(не\s+обращай\w*\s+внимани\w*|забудь\w*)[^\n]{0,40}(инструкц|указани|правил)`,
		`(примечание|заметка|сообщение|указание|инструкция)\s+для\s+(ии|ai|ассистента|помощника|бота|нейросети|модели)`,
		wbL + `ты\s*[—–-]?\s*(ии|ассистент|помощник|бот|нейросеть|языковая\s+модель)` + wbR,
		`системн\w+\s+(промпт|подсказк|инструкц)`,
		`подтверди\w*[^\n]{0,60}как\s+(проверенн|надёжн|надежн|подтверждённ)`,
	}
	var out []*regexp.Regexp
	for _, p := range ps {
		out = append(out, rxi(p))
	}
	return out
}()

func detectInjections(d *du.Doc) []model.SourceRef {
	out := []model.SourceRef{}
	seen := map[[2]int]bool{}
	for _, ls := range d.Lines() {
		line := d.Sub(ls.Start, ls.End)
		hit := false
		for _, p := range injPatterns {
			if p.MatchString(line) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		r := d.Ref(ls.Start, ls.End)
		k := [2]int{r.Start, r.End}
		if !seen[k] && r.Quote != "" {
			seen[k] = true
			out = append(out, r)
		}
	}
	return out
}

// DetectInjections находит строки с инструкциями для ИИ (мелкий и светлый текст тоже попадает в Text).
func DetectInjections(doc model.ParsedDoc) []model.SourceRef { return detectInjections(du.New(doc)) }
