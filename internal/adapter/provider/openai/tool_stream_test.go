package openai

import (
	"encoding/json"
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/model"
	"github.com/fwtllh-png/QCode/internal/adapter/provider"
)

func TestChatToolStreamIsScopedToGLM(t *testing.T) {
	for _, test := range []struct {
		name       string
		providerID string
		adapterID  model.AdapterID
		protocol   model.WireProtocol
		withTools  bool
		want       bool
	}{
		{"glm tools", "glm", model.AdapterOpenAICompatible, model.ProtocolOpenAIChat, true, true},
		{"glm text", "glm", model.AdapterOpenAICompatible, model.ProtocolOpenAIChat, false, true},
		{"deepseek", "deepseek", model.AdapterOpenAICompatible, model.ProtocolOpenAIChat, true, false},
		{"custom compatible", "compatible", model.AdapterOpenAICompatible, model.ProtocolOpenAIChat, true, false},
		{"openai", "openai", model.AdapterOpenAI, model.ProtocolOpenAIChat, true, false},
		{"responses", "glm", model.AdapterOpenAICompatible, model.ProtocolOpenAIResponses, true, false},
		{"openai adapter", "glm", model.AdapterOpenAI, model.ProtocolOpenAIChat, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := compatibleRequest(t)
			fixtureModel := request.Route.Model()
			catalog, err := model.NewCatalog(model.Provider{
				ID: test.providerID, Adapter: test.adapterID,
				Endpoint: "https://example.invalid", Protocol: test.protocol,
				Provenance: model.ProvenanceFixture,
				Models:     map[string]model.Model{fixtureModel.ID: fixtureModel},
			})
			if err != nil {
				t.Fatal(err)
			}
			resolver, err := model.NewResolver(catalog)
			if err != nil {
				t.Fatal(err)
			}
			request.Route, err = resolver.Resolve(model.RouteRequest{
				ProviderID: test.providerID, ModelID: fixtureModel.ID,
			})
			if err != nil {
				t.Fatal(err)
			}
			if test.withTools {
				request.Tools = []provider.ToolDefinition{{
					Name: "file_write", Description: "Write a file",
					InputSchema: map[string]any{"type": "object"},
				}}
			}
			adapter, err := NewAdapter(test.adapterID)
			if err != nil {
				t.Fatal(err)
			}
			call, err := adapter.Prepare(request)
			if err != nil {
				t.Fatal(err)
			}
			var body struct {
				Stream     bool  `json:"stream"`
				ToolStream *bool `json:"tool_stream"`
			}
			if err := json.Unmarshal(call.Body, &body); err != nil {
				t.Fatal(err)
			}
			if !body.Stream {
				t.Fatal("streaming must remain enabled")
			}
			if test.want {
				if body.ToolStream == nil || !*body.ToolStream {
					t.Fatalf("GLM tool_stream must be true: %s", call.Body)
				}
			} else if body.ToolStream != nil {
				t.Fatalf("provider-specific tool_stream leaked: %s", call.Body)
			}
		})
	}
}
