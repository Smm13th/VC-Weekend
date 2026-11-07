// Package refdata — справочники (счета клиента, курсы, тарифы, КНП, КБе, страны, банки, поля, база знаний, запасные ответы).
package refdata

import (
	"embed"
	"encoding/json"
	"strings"
	"sync"
)

//go:embed data/*.json
var files embed.FS

var (
	mu    sync.Mutex
	cache = map[string]any{}
)

// Raw возвращает байты data/<name>.json.
func Raw(name string) []byte {
	b, err := files.ReadFile("data/" + name + ".json")
	if err != nil {
		panic("refdata: нет файла " + name + ".json")
	}
	return b
}

// Load разбирает data/<name>.json в значение типа T (кешируется по имени и типу).
func Load[T any](name string) T {
	mu.Lock()
	defer mu.Unlock()
	key := name
	if v, ok := cache[key].(T); ok {
		return v
	}
	var v T
	if err := json.Unmarshal(Raw(name), &v); err != nil {
		panic("refdata: " + name + ": " + err.Error())
	}
	cache[key] = v
	return v
}

type Client struct {
	Name, NameEn, Bin, Address, AddressEn, Director, DirectorEn, Accountant, BankBic, BankName string
}

type AccountsFile struct {
	Client   Client `json:"client"`
	Accounts []struct {
		ID       string  `json:"id"`
		IBAN     string  `json:"iban"`
		Currency string  `json:"currency"`
		Balance  float64 `json:"balance"`
		Title    string  `json:"title"`
	} `json:"accounts"`
	Bonuses float64 `json:"bonuses"`
}

func Accounts() AccountsFile { return Load[AccountsFile]("accounts") }

type RatesFile struct {
	Date                string             `json:"date"`
	Rates               map[string]float64 `json:"rates"`
	ConversionSpreadPct float64            `json:"conversionSpreadPct"`
}

func Rates() RatesFile { return Load[RatesFile]("rates") }

type Tariffs struct {
	SenderFee struct {
		Pct float64            `json:"pct"`
		Min map[string]float64 `json:"min"`
		Max map[string]float64 `json:"max"`
	} `json:"senderFee"`
	OurGuaranteeFee    map[string]float64 `json:"ourGuaranteeFee"`
	IntermediaryFee    map[string]float64 `json:"intermediaryFee"`
	BeneficiaryBankFee map[string]float64 `json:"beneficiaryBankFee"`
	UrgentFeeKzt       float64            `json:"urgentFeeKzt"`
	BonusMaxSharePct   float64            `json:"bonusMaxSharePct"`
	Cutoff             map[string]struct {
		Time         string `json:"time"`
		StandardDays int    `json:"standardDays"`
		UrgentDays   int    `json:"urgentDays"`
	} `json:"cutoff"`
	AccountingNumberThresholdUsd float64 `json:"accountingNumberThresholdUsd"`
}

func TariffsData() Tariffs { return Load[Tariffs]("tariffs") }

type Bank struct {
	Swift, Name, City, Country, Address, Role string
	Currencies                                []string
}

func Banks() []Bank {
	return Load[struct {
		Items []Bank `json:"items"`
	}]("banks").Items
}

func BankBySwift(swift string) *Bank {
	s := strings.ToUpper(strings.TrimSpace(swift))
	for _, b := range Banks() {
		if b.Swift == s || (len(s) >= 8 && b.Swift == s[:8]) {
			bb := b
			return &bb
		}
	}
	return nil
}

type Country struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	NameEn     string `json:"nameEn"`
	IbanLength *int   `json:"ibanLength"`
	Currency   string `json:"currency"`
}

func Countries() []Country { return Load[[]Country]("countries") }

type KnpItem struct {
	Code  string   `json:"code"`
	Title string   `json:"title"`
	When  string   `json:"when"`
	Tags  []string `json:"tags"`
}

func Knp() []KnpItem {
	return Load[struct {
		Items []KnpItem `json:"items"`
	}]("knp").Items
}

type FieldMeta struct {
	ID      string `json:"id"`
	N       int    `json:"n"`
	Label   string `json:"label"`
	Section string `json:"section"`
	Help    string `json:"help"`
}

func Fields() []FieldMeta { return Load[[]FieldMeta]("fields") }
