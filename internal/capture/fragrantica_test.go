package capture

import (
	"errors"
	"testing"
)

func TestPageURL(t *testing.T) {
	base := "https://www.fragrantica.ru/board/viewtopic.php?id=235155"
	if got := PageURL(base, 1); got != base {
		t.Fatal(got)
	}
	if got := PageURL(base, 16); got != base+"&p=16" {
		t.Fatal(got)
	}
}
func TestRejectsChallengePage(t *testing.T) {
	for _, input := range [][2]string{{"Attention Required! | Cloudflare", "Verify you are human"}, {"CAPTCHA", "captcha"}} {
		if err := ValidateRenderedPage(input[0], input[1]); !errors.Is(err, ErrAccessChallenge) {
			t.Fatalf("got %v", err)
		}
	}
}
