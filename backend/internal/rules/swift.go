package rules

import (
	"regexp"

	du "vedhelper/internal/docutil"
	"vedhelper/internal/refdata"
)

// BankInfo — банк из справочника.
type BankInfo struct {
	Swift   string `json:"swift"`
	Name    string `json:"name"`
	City    string `json:"city"`
	Country string `json:"country"`
	Address string `json:"address"`
}

// SwiftCheck — результат проверки SWIFT/BIC.
type SwiftCheck struct {
	OK         bool      `json:"ok"`
	Normalized string    `json:"normalized"`
	Country    string    `json:"country,omitempty"`
	Error      string    `json:"error,omitempty"`
	Bank       *BankInfo `json:"bank,omitempty"`
}

var swiftRe = regexp.MustCompile(`^[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}([A-Z0-9]{3})?$`)

func normalizeSwift(s string) string { return du.NormalizeSwift(s) }

func bankBySwift(s string) *BankInfo {
	b := refdata.BankBySwift(s)
	if b == nil {
		return nil
	}
	return &BankInfo{Swift: b.Swift, Name: b.Name, City: b.City, Country: b.Country, Address: b.Address}
}

// ValidateSwift проверяет формат SWIFT и ищет банк в справочнике.
func ValidateSwift(s string) SwiftCheck {
	n := normalizeSwift(s)
	r := SwiftCheck{Normalized: n}
	if !swiftRe.MatchString(n) {
		r.Error = "format"
		return r
	}
	r.OK = true
	r.Country = n[4:6]
	r.Bank = bankBySwift(n)
	return r
}
