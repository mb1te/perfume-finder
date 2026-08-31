package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/PuerkitoBio/goquery"

	"parfumes_finder/internal/shop"
)

func (client *Client) GetDocument(ctx context.Context, endpoint string) (*goquery.Document, error) {
	payload, err := client.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	document, err := goquery.NewDocumentFromReader(bytes.NewReader(payload))
	if err != nil {
		return nil, shop.NewError(shop.ErrorParse, fmt.Errorf("parse shop HTML: %w", err))
	}
	return document, nil
}

func DecodeJSON(payload []byte, destination any) error {
	if err := json.Unmarshal(payload, destination); err != nil {
		return shop.NewError(shop.ErrorParse, fmt.Errorf("decode shop JSON: %w", err))
	}
	return nil
}
