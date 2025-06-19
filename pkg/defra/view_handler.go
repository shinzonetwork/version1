package defra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/lens-vm/lens/host-go/config/model"
	"github.com/sourcenetwork/immutable"
	"go.uber.org/zap"
)

type ViewHandler struct {
	defraURL string
}

type View struct {
	Query     string
	SDL       string
	Transform immutable.Option[model.Lens]
}

func NewViewHandler(host string, port int) *ViewHandler {
	return &ViewHandler{
		defraURL: fmt.Sprintf("http://%s:%d/api/v0/", host, port),
	}
}

func (h *ViewHandler) CreateView(query, sdl string, transform immutable.Option[model.Lens]) View {
	return View{
		Query:     query,
		SDL:       sdl,
		Transform: transform,
	}
}

func (h *ViewHandler) AddView(ctx context.Context, view View, sugar *zap.SugaredLogger) (interface{}, error) {
	url := h.defraURL + "view"

	payloadBuf := new(bytes.Buffer)
	if err := json.NewEncoder(payloadBuf).Encode(view); err != nil {
		sugar.Errorf("failed to encode view: %v", err)
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, payloadBuf)
	if err != nil {
		sugar.Errorf("failed to create request: %v", err)
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		sugar.Errorf("failed to send request: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		sugar.Errorf("unexpected status code %d: %s", resp.StatusCode, body)
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var result interface{}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		sugar.Errorf("failed to decode response: %v", err)
		return nil, err
	}

	return result, nil
}
