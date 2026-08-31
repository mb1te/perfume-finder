package shop

import (
	"errors"
	"testing"
)

func TestTypedErrorPreservesKindAndCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("markup changed")
	err := NewError(ErrorParse, cause)

	if !errors.Is(err, cause) {
		t.Fatal("typed error does not unwrap to its cause")
	}
	if got := ErrorKindOf(err); got != ErrorParse {
		t.Fatalf("ErrorKindOf() = %q, want %q", got, ErrorParse)
	}
}

func TestErrorKindOfDefaultsToUnknown(t *testing.T) {
	t.Parallel()

	if got := ErrorKindOf(errors.New("plain")); got != ErrorUnknown {
		t.Fatalf("ErrorKindOf() = %q, want %q", got, ErrorUnknown)
	}
}
