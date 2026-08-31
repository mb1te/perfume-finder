package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"parfumes_finder/internal/shop"
)

const maxBodyBytes = 5 * 1024 * 1024

var ErrBodyTooLarge = errors.New("response body exceeds 5 MiB")

type Client struct {
	http *http.Client
}

func New(client *http.Client) *Client {
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{http: client}
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

	response, err := client.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

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
