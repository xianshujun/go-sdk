// Copyright 2026 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStreamableCustomMethodDirectConnect verifies that a custom method
// registered with [AddReceivingCustomMethod] is accepted when a session is
// created with a direct [Server.Connect] on a [StreamableServerTransport],
// rather than through [NewStreamableHTTPHandler].
//
// A direct Connect is used to resume a session on another replica. The
// Streamable HTTP POST pre-check consults the server's receiving method set,
// so the transport needs the same server back-reference that
// connectStreamable installs. Without it, the pre-check falls back to the
// standard methods and rejects custom methods with HTTP 400. See #1506.
func TestStreamableCustomMethodDirectConnect(t *testing.T) {
	type searchParams struct {
		ParamsBase
		Query string `json:"query"`
	}
	type searchResult struct {
		ResultBase
		Hits []string `json:"hits"`
	}

	server := NewServer(testImpl, nil)
	if err := AddReceivingCustomMethod(server, "acme/search",
		func(context.Context, *ServerSession, *searchParams) (*searchResult, error) {
			return &searchResult{Hits: []string{"hit"}}, nil
		}); err != nil {
		t.Fatal(err)
	}

	transport := &StreamableServerTransport{SessionID: "s1"}
	state := &ServerSessionState{
		InitializeParams:  &InitializeParams{ProtocolVersion: protocolVersion20251125},
		InitializedParams: &InitializedParams{},
	}
	session, err := server.Connect(context.Background(), transport, &ServerSessionOptions{State: state})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	// The POST pre-check and Mcp-Param header validation both need these
	// back-references; Server.Connect is responsible for installing them.
	if transport.connection == nil {
		t.Fatal("transport connection was not initialized")
	}
	if transport.connection.server != server {
		t.Error("transport connection has no server back-reference")
	}
	if transport.connection.toolLookup == nil {
		t.Error("transport connection has no tool lookup")
	}

	httpServer := httptest.NewServer(transport)
	t.Cleanup(httpServer.Close)

	req, err := http.NewRequest(http.MethodPost, httpServer.URL,
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"acme/search","params":{"query":"x"}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", "s1")
	req.Header.Set("Mcp-Protocol-Version", protocolVersion20251125)

	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got HTTP %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"hit"`) {
		t.Errorf("response does not contain the custom method result: %s", body)
	}
}
