package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"parfumes_finder/internal/shop"
)

const maxBodyBytes = 5 * 1024 * 1024

var ErrBodyTooLarge = errors.New("response body exceeds 5 MiB")
var errRedirectDenied = errors.New("redirect leaves approved host")

type Client struct {
	http *http.Client
}

func New(client *http.Client) *Client {
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{http: client}
}

func ValidateURLHost(raw, allowedBase string) error {
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" {
		return fmt.Errorf("invalid product URL")
	}
	allowed, err := url.Parse(allowedBase)
	if err != nil || allowed.Hostname() == "" {
		return fmt.Errorf("invalid allowed host")
	}
	if !strings.EqualFold(target.Hostname(), allowed.Hostname()) {
		return fmt.Errorf("product URL host %s is not allowed", target.Hostname())
	}
	return nil
}

func (client *Client) PostForm(ctx context.Context, endpoint string, values url.Values) ([]byte, error) {
	body := []byte(values.Encode())
	return client.do(ctx, http.MethodPost, endpoint, body, "application/x-www-form-urlencoded")
}

func (client *Client) Get(ctx context.Context, endpoint string) ([]byte, error) {
	return client.get(ctx, endpoint)
}

func (client *Client) get(ctx context.Context, endpoint string) ([]byte, error) {
	return client.do(ctx, http.MethodGet, endpoint, nil, "")
}

func (client *Client) do(ctx context.Context, method, endpoint string, body []byte, contentType string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build shop request: %w", err)
	}
	request.Header.Set("User-Agent", "PerfumePriceBot/0.1")
	request.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}

	httpClient := *client.http
	previousRedirect := httpClient.CheckRedirect
	httpClient.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 || (next.URL.Scheme != "http" && next.URL.Scheme != "https") || !strings.EqualFold(next.URL.Host, request.URL.Host) {
			return errRedirectDenied
		}
		if previousRedirect != nil {
			return previousRedirect(next, via)
		}
		return nil
	}
	response, err := httpClient.Do(request)
	if err != nil {
		if errors.Is(err, errRedirectDenied) {
			return nil, shop.NewError(shop.ErrorAccess, err)
		}
		return nil, err
	}
	defer response.Body.Close()
	if !strings.EqualFold(response.Request.URL.Host, request.URL.Host) {
		return nil, shop.NewError(shop.ErrorAccess, errRedirectDenied)
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		kind := shop.ErrorUnknown
		switch response.StatusCode {
		case http.StatusForbidden, http.StatusUnauthorized:
			kind = shop.ErrorAccess
		case http.StatusTooManyRequests:
			kind = shop.ErrorAntiBot
		}
		return nil, shop.NewError(kind, fmt.Errorf("shop returned HTTP %d", response.StatusCode))
	}

	payload, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read shop response: %w", err)
	}
	if len(payload) > maxBodyBytes {
		return nil, shop.NewError(shop.ErrorParse, ErrBodyTooLarge)
	}
	return payload, nil
}
