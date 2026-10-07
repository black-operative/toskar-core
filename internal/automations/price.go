package automations

import (
	"math"
	"strconv"
	"strings"
)

// formatPrice writes a price for an automation's name the way the web app's
// formatPrice (Intl) mostly does: "$500" in English, "500 €" in German,
// "R$ 2.500" in Portuguese. Whole amounts have no cents. It covers the
// currencies requests name; others are written with their code.
func formatPrice(value float64, currency, lang string) string {
	if currency == "" {
		currency = "USD"
	}
	base := strings.ToLower(strings.SplitN(lang, "-", 2)[0])
	group, decimal := ",", "."
	switch base {
	case "de", "es", "it", "pt":
		group, decimal = ".", ","
	case "fr":
		group, decimal = " ", ","
	}
	number := groupedNumber(value, group, decimal)
	symbol := currencySymbol(currency, lang)
	switch base {
	case "de", "es", "fr", "it":
		return number + " " + symbol
	case "pt":
		return symbol + " " + number
	}
	return symbol + number
}

func currencySymbol(currency, lang string) string {
	switch currency {
	case "USD":
		if lang == "en" || strings.HasPrefix(lang, "en-") || lang == "ja" {
			return "$"
		}
		return "US$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	case "JPY":
		if lang == "ja" {
			return "￥"
		}
		return "¥"
	case "CNY":
		if lang == "zh-Hans" {
			return "¥"
		}
		return "CN¥"
	case "TWD":
		if lang == "zh-Hant" {
			return "$"
		}
		return "NT$"
	case "KRW":
		return "₩"
	case "BRL":
		return "R$"
	}
	return currency
}

func groupedNumber(value float64, group, decimal string) string {
	cents := math.Round(value*100) / 100
	whole := int64(math.Floor(cents))
	fraction := int64(math.Round((cents - float64(whole)) * 100))
	digits := strconv.FormatInt(whole, 10)
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteString(group)
		}
		b.WriteRune(d)
	}
	if fraction > 0 {
		b.WriteString(decimal)
		if fraction < 10 {
			b.WriteByte('0')
		}
		b.WriteString(strconv.FormatInt(fraction, 10))
	}
	return b.String()
}
