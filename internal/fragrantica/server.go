package fragrantica

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/enrichment"
)

const (
	maxRequestBody = 16 * 1024
	maxPNGBytes    = 5 * 1024 * 1024
)

type Renderer interface {
	Enrich(context.Context, enrichment.Request) (enrichment.Card, bool, error)
	Ready(context.Context) error
}

type enrichResponse struct {
	SourceURL string              `json:"source_url"`
	Title     string              `json:"title"`
	Accords   []enrichment.Accord `json:"accords"`
	PNGBase64 string              `json:"png_base64"`
}

type enrichRequest struct {
	Brand         string               `json:"brand"`
	Name          string               `json:"name"`
	Edition       string               `json:"edition"`
	Concentration domain.Concentration `json:"concentration"`
}

func NewServer(renderer Renderer) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/healthz":
			serveHealth(writer, request, renderer)
		case "/v1/enrich":
			serveEnrich(writer, request, renderer)
		default:
			http.NotFound(writer, request)
		}
	})
}

func serveHealth(writer http.ResponseWriter, request *http.Request, renderer Renderer) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if renderer == nil || renderer.Ready(request.Context()) != nil {
		http.Error(writer, "browser unavailable", http.StatusServiceUnavailable)
		return
	}
	writer.WriteHeader(http.StatusOK)
}

func serveEnrich(writer http.ResponseWriter, request *http.Request, renderer Renderer) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if renderer == nil {
		http.Error(writer, "renderer unavailable", http.StatusBadGateway)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input enrichRequest
	if err := decoder.Decode(&input); err != nil {
		http.Error(writer, "invalid request", http.StatusBadRequest)
		return
	}
	if err := requireJSONEOF(decoder); err != nil {
		http.Error(writer, "invalid request", http.StatusBadRequest)
		return
	}
	card, ok, err := renderer.Enrich(request.Context(), enrichment.Request{
		Brand: input.Brand, Name: input.Name, Edition: input.Edition, Concentration: input.Concentration,
	})
	if err != nil {
		serveEnrichmentError(writer, err)
		return
	}
	if !ok {
		http.Error(writer, "exact match not found", http.StatusNotFound)
		return
	}
	if len(card.PNG) > maxPNGBytes {
		http.Error(writer, "rendered image too large", http.StatusBadGateway)
		return
	}
	response := boundedResponse(card)
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		return
	}
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func serveEnrichmentError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBusy):
		http.Error(writer, "renderer busy", http.StatusTooManyRequests)
	case errors.Is(err, ErrAccessChallenge):
		http.Error(writer, "source access challenge", http.StatusServiceUnavailable)
	case errors.Is(err, context.DeadlineExceeded):
		http.Error(writer, "renderer deadline exceeded", http.StatusGatewayTimeout)
	default:
		http.Error(writer, "renderer failed", http.StatusBadGateway)
	}
}

func boundedResponse(card enrichment.Card) enrichResponse {
	accordCount := min(len(card.Accords), 16)
	accords := make([]enrichment.Accord, accordCount)
	for index := range accordCount {
		accords[index] = card.Accords[index]
		accords[index].Name = truncateUTF8(accords[index].Name, 128)
		accords[index].Color = truncateUTF8(accords[index].Color, 128)
	}
	return enrichResponse{
		SourceURL: truncateUTF8(card.SourceURL, 2048),
		Title:     truncateUTF8(card.Title, 256),
		Accords:   accords,
		PNGBase64: base64.StdEncoding.EncodeToString(card.PNG),
	}
}

func truncateUTF8(value string, limit int) string {
	value = strings.ToValidUTF8(value, "\uFFFD")
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
