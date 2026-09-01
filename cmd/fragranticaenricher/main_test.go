package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

type fakeHTTPClient struct {
	response *http.Response
	err      error
	request  *http.Request
}

func (client *fakeHTTPClient) Do(request *http.Request) (*http.Response, error) {
	client.request = request
	return client.response, client.err
}

func TestHealthcheckProbesBoundedLocalBrowserReadiness(t *testing.T) {
	client := &fakeHTTPClient{response: &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
	}}
	if err := runHealthcheck(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	if client.request.Method != http.MethodGet || client.request.URL.String() != "http://127.0.0.1:8081/healthz" {
		t.Fatalf("request = %s %s", client.request.Method, client.request.URL)
	}
	if _, ok := client.request.Context().Deadline(); !ok {
		t.Fatal("healthcheck request has no deadline")
	}
}

func TestHealthcheckRejectsNonOKAndTransportFailure(t *testing.T) {
	for _, test := range []struct {
		name     string
		response *http.Response
		err      error
	}{
		{name: "not ready", response: &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("unavailable"))}},
		{name: "transport", err: errors.New("connection refused")},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeHTTPClient{response: test.response, err: test.err}
			if err := runHealthcheck(context.Background(), client); err == nil {
				t.Fatal("healthcheck succeeded")
			}
		})
	}
}

func TestChromeAllocatorOptionsKeepSandboxByDefault(t *testing.T) {
	for _, value := range []string{"", "0", "true"} {
		options := chromeAllocatorOptions(value)
		if len(options) != len(chromedp.DefaultExecAllocatorOptions) {
			t.Fatalf("CHROME_NO_SANDBOX=%q: allocator options = %d, want default %d", value, len(options), len(chromedp.DefaultExecAllocatorOptions))
		}
	}
}

func TestChromeAllocatorOptionsDisableSandboxOnlyOnExplicitOptIn(t *testing.T) {
	options := chromeAllocatorOptions("1")
	if len(options) != len(chromedp.DefaultExecAllocatorOptions)+1 {
		t.Fatalf("allocator options = %d, want default + NoSandbox (%d)", len(options), len(chromedp.DefaultExecAllocatorOptions)+1)
	}
}
