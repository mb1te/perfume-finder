package enrichment

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
)

const (
	maxRequestBytes  = 16 << 10
	maxResponseBytes = 7 << 20
	maxPNGBytes      = 5 << 20
)

var (
	ErrRequestTooLarge    = errors.New("enrichment request exceeds 16 KiB")
	ErrResponseTooLarge   = errors.New("enrichment response exceeds 7 MiB")
	ErrInvalidContentType = errors.New("invalid enrichment response content type")
	ErrInvalidImage       = errors.New("invalid enrichment image")
	ErrInvalidSourceURL   = errors.New("invalid Fragrantica source URL")
)

type HTTPStatusError struct {
	StatusCode int
}

func (err *HTTPStatusError) Error() string {
	return fmt.Sprintf("enricher returned HTTP %d", err.StatusCode)
}

type HTTPClient struct {
	endpoint string
	http     *http.Client
}

type enrichRequest struct {
	Brand         string `json:"brand"`
	Name          string `json:"name"`
	Edition       string `json:"edition"`
	Concentration string `json:"concentration"`
}

type enrichResponse struct {
	SourceURL string   `json:"source_url"`
	Title     string   `json:"title"`
	Accords   []Accord `json:"accords"`
	PNGBase64 string   `json:"png_base64"`
}

func NewHTTPClient(baseURL string, client *http.Client) (*HTTPClient, error) {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Scheme == "" || base.Hostname() == "" || base.User != nil ||
		(base.Scheme != "http" && base.Scheme != "https") || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("invalid enricher base URL")
	}
	if client == nil {
		client = http.DefaultClient
	}
	httpClient := *client
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &HTTPClient{
		endpoint: strings.TrimRight(base.String(), "/") + "/v1/enrich",
		http:     &httpClient,
	}, nil
}

func (client *HTTPClient) Enrich(ctx context.Context, input Request) (Card, bool, error) {
	payload, err := json.Marshal(enrichRequest{
		Brand: input.Brand, Name: input.Name, Edition: input.Edition, Concentration: string(input.Concentration),
	})
	if err != nil {
		return Card{}, false, fmt.Errorf("encode enrichment request: %w", err)
	}
	if len(payload) > maxRequestBytes {
		return Card{}, false, ErrRequestTooLarge
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(payload))
	if err != nil {
		return Card{}, false, fmt.Errorf("build enrichment request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return Card{}, false, fmt.Errorf("call enricher: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return Card{}, false, nil
	}
	if response.StatusCode != http.StatusOK {
		return Card{}, false, &HTTPStatusError{StatusCode: response.StatusCode}
	}
	if err := requireJSONContentType(response.Header.Get("Content-Type")); err != nil {
		return Card{}, false, err
	}

	payload, err = readBoundedResponse(response.Body)
	if err != nil {
		return Card{}, false, err
	}
	var output enrichResponse
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&output); err != nil {
		return Card{}, false, fmt.Errorf("decode enrichment response: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Card{}, false, fmt.Errorf("decode enrichment response: %w", err)
	}

	png, err := base64.StdEncoding.DecodeString(output.PNGBase64)
	if err != nil {
		return Card{}, false, fmt.Errorf("%w: decode base64", ErrInvalidImage)
	}
	if len(png) > maxPNGBytes {
		return Card{}, false, ErrImageTooLarge
	}
	if !bytes.HasPrefix(png, pngSignature) {
		return Card{}, false, ErrInvalidImage
	}
	if !safeSourceURL(output.SourceURL) {
		return Card{}, false, ErrInvalidSourceURL
	}
	return Card{SourceURL: output.SourceURL, Title: output.Title, Accords: output.Accords, PNG: png}, true, nil
}

var pngSignature = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

func requireJSONContentType(value string) error {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return ErrInvalidContentType
	}
	return nil
}

func readBoundedResponse(body io.Reader) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read enrichment response: %w", err)
	}
	if len(payload) < maxResponseBytes {
		return payload, nil
	}
	var extra [1]byte
	count, err := body.Read(extra[:])
	if count > 0 {
		return nil, ErrResponseTooLarge
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read enrichment response: %w", err)
	}
	return payload, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return errors.New("multiple JSON values")
}

func safeSourceURL(raw string) bool {
	target, err := url.Parse(raw)
	if err != nil || target.User != nil || target.Scheme != "https" || target.Port() != "" ||
		target.RawQuery != "" || target.ForceQuery || target.Fragment != "" || target.RawFragment != "" {
		return false
	}
	host := strings.ToLower(target.Hostname())
	if host != "fragrantica.ru" && host != "www.fragrantica.ru" {
		return false
	}
	if path.Clean(target.Path) != target.Path {
		return false
	}
	return strings.HasPrefix(target.Path, "/perfume/") && strings.HasSuffix(target.Path, ".html")
}
