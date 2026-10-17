// SPDX-License-Identifier: AGPL-3.0-only

// Коннектор для e2e: заголовок и текст страницы через действия нативного
// коннектора browser (service_call).
package main

import (
	"context"
	_ "embed"

	"github.com/gate4ai/sdk-go/abi"
	"github.com/gate4ai/sdk-go/connector"
)

//go:embed manifest.yaml
var manifestYAML []byte

type reader struct{}

func (reader) Execute(_ context.Context, h *abi.Host, r abi.ExecRequest) (abi.ExecResult, error) {
	if _, err := h.ServiceCall("browser", "open", map[string]any{"url": r.Args["url"]}, nil); err != nil {
		return abi.ExecResult{}, err
	}
	var page struct {
		Title string `json:"title"`
		Text  string `json:"text"`
	}
	if _, err := h.ServiceCall("browser", "observe", map[string]any{}, &page); err != nil {
		return abi.ExecResult{}, err
	}
	return abi.ExecResult{Structured: map[string]any{"title": page.Title, "text": page.Text}}, nil
}

func (reader) Verify(context.Context, *abi.Host, abi.VerifyRequest) (abi.VerifyResult, error) {
	return abi.VerifyResult{}, abi.Errorf(abi.CodeBadRequest, "no verification")
}

func init() { connector.Register(manifestYAML, reader{}) }
func main() {}
