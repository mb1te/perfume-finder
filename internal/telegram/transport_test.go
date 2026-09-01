package telegram

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot"
)

func TestTransportUploadsFragranticaPNGWithCanonicalFilenameAndCaption(t *testing.T) {
	wantPNG := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	wantCaption := "Sauvage Eau de Parfum Dior\nhttps://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html"
	var gotFilename, gotCaption string
	var gotPNG []byte
	client := telegramHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		reader, err := request.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			value, err := io.ReadAll(part)
			if err != nil {
				t.Fatal(err)
			}
			switch part.FormName() {
			case "photo":
				gotFilename = part.FileName()
				gotPNG = value
			case "caption":
				gotCaption = string(value)
			}
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)),
		}, nil
	})
	b, err := bot.New("private-test-token", bot.WithSkipGetMe(), bot.WithHTTPClient(time.Second, client))
	if err != nil {
		t.Fatal(err)
	}
	transport := &Transport{bot: b}

	if err := transport.Send(context.Background(), 42, Message{PhotoPNG: wantPNG, Caption: wantCaption}); err != nil {
		t.Fatal(err)
	}
	if gotFilename != "fragrantica.png" {
		t.Fatalf("filename = %q", gotFilename)
	}
	if !bytes.Equal(gotPNG, wantPNG) {
		t.Fatalf("PNG = %x, want %x", gotPNG, wantPNG)
	}
	if gotCaption != wantCaption {
		t.Fatalf("caption = %q, want %q", gotCaption, wantCaption)
	}
}

type telegramHTTPClientFunc func(*http.Request) (*http.Response, error)

func (function telegramHTTPClientFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

var _ bot.HttpClient = telegramHTTPClientFunc(nil)
