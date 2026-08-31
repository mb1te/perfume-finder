package domain

import "testing"

func TestNormalizeTextCanonicalizesRussianAndWhitespace(t *testing.T) {
	t.Parallel()

	got := NormalizeText("  Ёлка\tEAU   DE PARFUM  ")
	const want = "елка eau de parfum"
	if got != want {
		t.Fatalf("NormalizeText() = %q, want %q", got, want)
	}
}

func TestParseConcentration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  Concentration
	}{
		{name: "edt abbreviation", input: "Dior Sauvage EDT", want: ConcentrationEDT},
		{name: "eau de toilette", input: "Eau de Toilette", want: ConcentrationEDT},
		{name: "eau de parfum", input: "eau de parfum", want: ConcentrationEDP},
		{name: "extrait", input: "Extrait de Parfum", want: ConcentrationExtrait},
		{name: "elixir", input: "Sauvage Elixir", want: ConcentrationElixir},
		{name: "unknown", input: "Sauvage", want: ConcentrationUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ParseConcentration(tt.input); got != tt.want {
				t.Fatalf("ParseConcentration(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseVolumeMicroliters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  int
	}{
		{input: "100 мл", want: 100000},
		{input: "1.5 ml", want: 1500},
		{input: "1,5 ML", want: 1500},
		{input: "8ML", want: 8000},
		{input: "no volume", want: 0},
	}

	for _, tt := range tests {
		if got := ParseVolumeMicroliters(tt.input); got != tt.want {
			t.Fatalf("ParseVolumeMicroliters(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestClassifyKindIsConservative(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  ProductKind
	}{
		{input: "тестер 100 мл", want: ProductKindTester},
		{input: "пробник 1.5 мл", want: ProductKindSample},
		{input: "миниатюра 8 мл", want: ProductKindMiniature},
		{input: "отливант 10 мл", want: ProductKindDecant},
		{input: "парфюмерная вода 100 мл в слюде", want: ProductKindRetail},
		{input: "retail box 100 ml", want: ProductKindRetail},
		{input: "Sauvage 100 мл", want: ProductKindUnknown},
	}

	for _, tt := range tests {
		if got := ClassifyKind(tt.input); got != tt.want {
			t.Fatalf("ClassifyKind(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
