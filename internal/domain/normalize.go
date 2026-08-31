package domain

import (
	"regexp"
	"strconv"
	"strings"
)

var volumePattern = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*(?:ml|мл)`)

func NormalizeText(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "ё", "е")
	return strings.Join(strings.Fields(value), " ")
}

func ParseConcentration(value string) Concentration {
	normalized := NormalizeText(value)

	patterns := []struct {
		needles []string
		value   Concentration
	}{
		{needles: []string{"elixir", "эликсир"}, value: ConcentrationElixir},
		{needles: []string{"extrait de parfum", "extrait", "экстракт духов"}, value: ConcentrationExtrait},
		{needles: []string{"eau de toilette", "туалетная вода", " edt ", " edt"}, value: ConcentrationEDT},
		{needles: []string{"eau de parfum", "парфюмерная вода", " edp ", " edp"}, value: ConcentrationEDP},
		{needles: []string{"eau de cologne", "cologne", "одеколон"}, value: ConcentrationCologne},
		{needles: []string{"parfum", "духи"}, value: ConcentrationParfum},
	}

	padded := " " + normalized + " "
	for _, pattern := range patterns {
		for _, needle := range pattern.needles {
			if strings.Contains(padded, needle) {
				return pattern.value
			}
		}
	}

	return ConcentrationUnknown
}

func ParseVolumeMicroliters(value string) int {
	match := volumePattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return 0
	}

	parts := strings.FieldsFunc(match[1], func(r rune) bool {
		return r == '.' || r == ','
	})
	whole, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}

	fraction := 0
	if len(parts) == 2 {
		digits := parts[1]
		if len(digits) > 3 {
			digits = digits[:3]
		}
		for len(digits) < 3 {
			digits += "0"
		}
		fraction, err = strconv.Atoi(digits)
		if err != nil {
			return 0
		}
	}

	return whole*1000 + fraction
}

func ClassifyKind(value string) ProductKind {
	normalized := NormalizeText(value)

	switch {
	case containsAny(normalized, "тестер", "tester"):
		return ProductKindTester
	case containsAny(normalized, "пробник", "сэмпл", "sample", "vial"):
		return ProductKindSample
	case containsAny(normalized, "миниатюра", "miniature", "mini "):
		return ProductKindMiniature
	case containsAny(normalized, "отливант", "распив", "decant", "atomizer", "атомайзер"):
		return ProductKindDecant
	case containsAny(normalized, "в слюде", "запечат", "retail box", "товарный флакон"):
		return ProductKindRetail
	default:
		return ProductKindUnknown
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
