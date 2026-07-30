package agentruntime

import (
	"context"
	"errors"
	"testing"
)

func TestClaudeSDKProviderAcceptanceReportsPreDispatchFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*ClaudeCodeSDKAdapter, *claudeSDKAdapterSession)
	}{
		{
			name: "prompt image materialization",
			configure: func(adapter *ClaudeCodeSDKAdapter, _ *claudeSDKAdapterSession) {
				adapter.promptImageMaterializer = func(
					context.Context,
					[]PromptContentBlock,
				) ([]PromptContentBlock, error) {
					return nil, errors.New("signed image expired")
				}
			},
		},
		{
			name: "reader startup",
			configure: func(_ *ClaudeCodeSDKAdapter, session *claudeSDKAdapterSession) {
				session.reader = nil
			},
		},
		{
			name:      "sidecar send",
			configure: func(_ *ClaudeCodeSDKAdapter, _ *claudeSDKAdapterSession) {},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			adapter := NewClaudeCodeSDKAdapter(nil)
			conn := &failingClaudeSDKConnection{}
			session, adapterSession := newClaudeSDKLifecycleTestSession(t, adapter, conn)
			test.configure(adapter, adapterSession)
			dispatch := make(chan ProviderDispatchResult, 1)

			_, err := adapter.ExecWithProviderAcceptance(
				t.Context(),
				session,
				[]PromptContentBlock{{Type: "text", Text: "hello"}},
				"hello",
				"turn-pre-dispatch",
				nil,
				nil,
				func(result ProviderDispatchResult) {
					select {
					case dispatch <- result:
					default:
					}
				},
			)
			if err == nil {
				t.Fatal("ExecWithProviderAcceptance() error=nil, want pre-dispatch failure")
			}
			select {
			case result := <-dispatch:
				if result.Disposition != DispatchDispositionNotDispatched ||
					result.Acceptance != nil {
					t.Fatalf("provider dispatch=%#v, want not_dispatched", result)
				}
			default:
				t.Fatal("provider dispatch was not reported")
			}
		})
	}
}
