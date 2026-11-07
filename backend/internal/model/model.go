// Package model — типы API. Это контракт между Go-бэкендом и Flutter-приложением.
// JSON в camelCase. Все позиции Start/End — индексы в рунах (символах Unicode)
// строки ParsedDoc.Text, End не включается. Клиент (Dart) работает с UTF-16, поэтому сервер
// дополнительно отдаёт уже вырезанную цитату Quote и контекст в SourceRef — клиент подсвечивает по ним.
package model

type DocKind string

const (
	KindContract DocKind = "contract"
	KindInvoice  DocKind = "invoice"
	KindLetter   DocKind = "letter"
	KindOther    DocKind = "other"
)

type Severity string

const (
	Red    Severity = "red"    // блокирует
	Yellow Severity = "yellow" // обсудить с поставщиком
	Green  Severity = "green"  // в порядке
)

type Stage string

const (
	StageContract  Stage = "contract"
	StageDocuments Stage = "documents"
	StagePayment   Stage = "payment"
)

// RuleIDs — канонические идентификаторы правил; eval/cases.json ссылается на них.
var RuleIDs = []string{
	"placeholder", "iban_format", "iban_checksum", "swift_format", "swift_iban_country",
	"acc_number_required", "over_remaining", "amount_exceeds_contract", "currency_mismatch",
	"invoice_before_contract", "contract_expired", "invoice_contract_ref", "requisites_changed",
	"beneficiary_name_mismatch", "hidden_instruction", "bilingual_numbering_gap",
	"bilingual_number_mismatch", "bilingual_length_ratio", "clause_consent_party", "clause_air_waybill",
	"clause_cross_reference", // 11.1 d) ссылается на «пункт 10» вместо статьи 9 о форс-мажоре
	"clause_duplicate",       // 4.2 и 4.3 почти дублируют друг друга
	"fees_not_specified", "payment_term_missing", "purpose_latin", "purpose_length", "insufficient_balance",
}

var ExtractKeys = []string{
	"contractNo", "contractDate", "expiryDate", "currency", "totalAmount",
	"sellerName", "sellerRepresentative", "sellerAddress", "sellerCountry", "sellerLicenseNo",
	"sellerBank", "sellerBankAddress", "sellerAccountName", "sellerAccountNo", "sellerIban", "sellerSwift",
	"buyerName", "buyerAddress", "buyerIban", "buyerBic",
	"invoiceNo", "invoiceDate", "invoiceAmount", "invoiceDueDate", "invoiceContractRef",
	"goodsDescription", "deliveryTerms", "paymentTerms", "intermediaryBank",
}

// FieldIDs — 25 полей заявки «Валютный перевод в другой банк» в порядке памятки.
var FieldIDs = []string{
	"requestNo", "debitAccount", "amount", "valueDate", "urgent",
	"beneficiarySwift", "intermediaryBank", "knp", "purpose", "linkContract",
	"docNumber", "docDate", "attachments", "beneficiaryName", "residenceCountry",
	"beneficiaryIban", "kbe", "beneficiaryCountry", "addressType", "feeType",
	"feeAccount", "signerHead", "signerAccountant", "useBonuses", "acceptTerms",
}

// DocBlock — фрагмент текста. Для двуязычной таблицы договора Row = номер строки, Lang = 'en'|'ru'.
type DocBlock struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Row   *int   `json:"row,omitempty"`
	Lang  string `json:"lang,omitempty"`
	Page  int    `json:"page,omitempty"` // страница PDF, с 1
}

type ParsedDoc struct {
	ID     string     `json:"id"`   // стабильный id, например 'A/contract.docx'
	Name   string     `json:"name"` // имя файла
	Kind   DocKind    `json:"kind"`
	Format string     `json:"format"` // docx | pdf | txt
	Text   string     `json:"text"`   // блоки, соединённые '\n'
	Blocks []DocBlock `json:"blocks"`
	Pages  int        `json:"pages"`
}

// SourceRef — точное место в документе. Start/End в рунах; Quote == []rune(Text)[Start:End].
// Block — индекс блока, в котором цитата начинается (для шторки с фрагментом на телефоне).
type SourceRef struct {
	DocID string `json:"docId"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Quote string `json:"quote"`
	Page  int    `json:"page,omitempty"`
	Block int    `json:"block"`
}

type Extracted struct {
	Key        string    `json:"key"`
	Value      string    `json:"value"`          // как в документе
	Norm       any       `json:"norm,omitempty"` // даты 'YYYY-MM-DD', суммы float64, IBAN/SWIFT без пробелов
	Source     SourceRef `json:"source"`
	Confidence string    `json:"confidence"` // high | medium | low
	Status     string    `json:"status"`     // doc | conflict
	Note       string    `json:"note,omitempty"`
}

type ExtractionResult struct {
	DocID  string               `json:"docId"`
	Kind   DocKind              `json:"kind"`
	Fields map[string]Extracted `json:"fields"`
}

type Suggestion struct {
	RU     string `json:"ru"`
	EN     string `json:"en"`
	Clause string `json:"clause,omitempty"`
}

type Finding struct {
	ID          string      `json:"id"` // '<rule>:<docId>[:уточнение]'
	Rule        string      `json:"rule"`
	Stage       Stage       `json:"stage"`
	Severity    Severity    `json:"severity"`
	Title       string      `json:"title"`
	Detail      string      `json:"detail"`
	Sources     []SourceRef `json:"sources"`
	Field       string      `json:"field,omitempty"`
	Suggestion  *Suggestion `json:"suggestion,omitempty"`
	MentorCheck bool        `json:"mentorCheck"`
}

type ClausePair struct {
	Num       string     `json:"num"`
	EN        *SourceRef `json:"en,omitempty"`
	RU        *SourceRef `json:"ru,omitempty"`
	Ratio     float64    `json:"ratio,omitempty"`
	NumbersEN []string   `json:"numbersEn"`
	NumbersRU []string   `json:"numbersRu"`
	Issues    []string   `json:"issues"`
}

type FieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Why   string `json:"why"`
}

// FieldState: Status doc (есть Source) | ai (догадка, подтвердить) | conflict (нет данных или противоречие)
// | user (ввёл человек) | system | empty.
type FieldState struct {
	Value     string        `json:"value"` // чекбоксы 'true'/'false'
	Status    string        `json:"status"`
	Source    *SourceRef    `json:"source,omitempty"`
	Confirmed bool          `json:"confirmed"`
	Note      string        `json:"note,omitempty"`
	Options   []FieldOption `json:"options,omitempty"`
	Suggested string        `json:"suggested,omitempty"`
}

type Account struct {
	ID       string  `json:"id"`
	IBAN     string  `json:"iban"`
	Currency string  `json:"currency"`
	Balance  float64 `json:"balance"`
	Title    string  `json:"title"`
}

type Payment struct {
	Date     string  `json:"date"`
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
	Ref      string  `json:"ref"`
}

type CaseInput struct {
	Docs             []ParsedDoc           `json:"docs"`
	Payments         []Payment             `json:"payments"`
	AccountingNumber string                `json:"accountingNumber"`
	ValueDate        string                `json:"valueDate"`
	Today            string                `json:"today,omitempty"`
	Form             map[string]FieldState `json:"form,omitempty"` // поля со Status 'user' не перезаписываются
	Task             map[string]string     `json:"task,omitempty"` // ответы стартового экрана: goods, timing (prepay|postpay), ...
}

type FeeLine struct {
	Who    string  `json:"who"` // sender | beneficiary
	Label  string  `json:"label"`
	Amount float64 `json:"amount"`
}

type FeeSim struct {
	FeeType      string    `json:"feeType"`
	Currency     string    `json:"currency"`
	SenderTotal  float64   `json:"senderTotal"`
	SenderFees   float64   `json:"senderFees"`
	ReceiverGets float64   `json:"receiverGets"`
	Shortfall    float64   `json:"shortfall"`
	UrgentFeeKZT float64   `json:"urgentFeeKzt"`
	Lines        []FeeLine `json:"lines"`
}

type BalanceOption struct {
	Kind          string  `json:"kind"` // convert | partial
	Title         string  `json:"title"`
	Detail        string  `json:"detail"`
	FromAccountID string  `json:"fromAccountId,omitempty"`
	Amount        float64 `json:"amount,omitempty"`
}

type BalanceCheck struct {
	OK        bool            `json:"ok"`
	Currency  string          `json:"currency"`
	Available float64         `json:"available"`
	Needed    float64         `json:"needed"`
	Shortfall float64         `json:"shortfall"`
	Options   []BalanceOption `json:"options"`
}

type ContractSummary struct {
	ContractNo            string   `json:"contractNo,omitempty"`
	ContractDate          string   `json:"contractDate,omitempty"`
	Currency              string   `json:"currency,omitempty"`
	Total                 *float64 `json:"total,omitempty"`
	TotalUSD              *float64 `json:"totalUsd,omitempty"`
	Paid                  float64  `json:"paid"`
	Remaining             *float64 `json:"remaining,omitempty"`
	NeedsAccountingNumber bool     `json:"needsAccountingNumber"`
	ThresholdUSD          float64  `json:"thresholdUsd"`
}

type ValueDates struct {
	Standard string `json:"standard"`
	Urgent   string `json:"urgent"`
	Cutoff   string `json:"cutoff"`
}

type KbeInfo struct {
	Code        string `json:"code"`
	Why         string `json:"why"`
	MentorCheck bool   `json:"mentorCheck"`
}

type DocumentResult struct {
	Doc        ParsedDoc        `json:"doc"`
	Extraction ExtractionResult `json:"extraction"`
	Clauses    []ClausePair     `json:"clauses"`
}

type ContractReport struct {
	Findings   []Finding        `json:"findings"`
	Clauses    []ClausePair     `json:"clauses"`
	Extraction ExtractionResult `json:"extraction"`
}

type PaymentReport struct {
	Findings    []Finding             `json:"findings"` // все этапы
	Form        map[string]FieldState `json:"form"`     // 25 полей
	Extractions []ExtractionResult    `json:"extractions"`
	Contract    ContractSummary       `json:"contract"`
	Fees        map[string]FeeSim     `json:"fees"` // OUR | SHA | BEN
	Balance     *BalanceCheck         `json:"balance,omitempty"`
	ValueDates  ValueDates            `json:"valueDates"`
	Kbe         *KbeInfo              `json:"kbe,omitempty"`
}
