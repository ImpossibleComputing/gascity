package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/extmsg"
	"github.com/gastownhall/gascity/internal/session"
)

func TestHandleExtMsgInboundDefaultRouteBindsAndColdWakes(t *testing.T) {
	fs, srv, services, ref := newExtMsgAgentBindingFixture(t)
	fs.cfg.ExtMsg = config.ExtMsgConfig{
		DefaultRoutes: []config.ExtMsgDefaultRoute{
			{Provider: "discord", Agent: "myrig/worker"},
		},
	}

	if _, err := session.ResolveSessionID(fs.cityBeadStore, "myrig/worker"); err == nil {
		t.Fatal("named agent should not have a session before the inbound message")
	}

	rec := postExtMsg(t, fs, srv, "/extmsg/inbound", map[string]any{
		"message": map[string]any{
			"provider_message_id": "msg-default-1",
			"conversation":        conversationBody(ref),
			"actor":               map[string]any{"id": "user-1", "display_name": "User One", "is_bot": false},
			"text":                "hello, is anyone responsible for this chat?",
			"received_at":         time.Now().UTC().Format(time.RFC3339),
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var result extmsg.InboundResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode inbound result: %v", err)
	}
	if result.TargetAgentName != "myrig/worker" {
		t.Fatalf("TargetAgentName = %q, want myrig/worker", result.TargetAgentName)
	}

	binding, err := services.Bindings.ResolveByConversation(context.Background(), ref)
	if err != nil {
		t.Fatalf("ResolveByConversation: %v", err)
	}
	if binding == nil || binding.AgentName != "myrig/worker" {
		t.Fatalf("binding = %#v, want sticky agent binding myrig/worker", binding)
	}

	// The notify fan-out cold-wakes the routed agent's named session.
	awaitColdWakeMaterialized(t, fs, "myrig/worker")
}

func TestHandleExtMsgInboundDefaultRouteMatchesMixedCaseProvider(t *testing.T) {
	fs, srv, services, ref := newExtMsgAgentBindingFixture(t)
	fs.cfg.ExtMsg = config.ExtMsgConfig{
		DefaultRoutes: []config.ExtMsgDefaultRoute{
			{Provider: "discord", Agent: "myrig/worker"},
		},
	}

	// The inbound conversation carries a mixed-case provider ("Discord"),
	// which extmsg canonicalizes to lowercase. The default-route lookup must
	// match the lowercase configured route regardless of the incoming casing,
	// otherwise the conversation stays unrouted.
	mixedCase := ref
	mixedCase.Provider = "Discord"

	rec := postExtMsg(t, fs, srv, "/extmsg/inbound", map[string]any{
		"message": map[string]any{
			"provider_message_id": "msg-default-mixedcase-1",
			"conversation":        conversationBody(mixedCase),
			"actor":               map[string]any{"id": "user-1", "display_name": "User One", "is_bot": false},
			"text":                "hello from a mixed-case provider",
			"received_at":         time.Now().UTC().Format(time.RFC3339),
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var result extmsg.InboundResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode inbound result: %v", err)
	}
	if result.TargetAgentName != "myrig/worker" {
		t.Fatalf("TargetAgentName = %q, want myrig/worker (mixed-case provider must match lowercase route)", result.TargetAgentName)
	}

	binding, err := services.Bindings.ResolveByConversation(context.Background(), ref)
	if err != nil {
		t.Fatalf("ResolveByConversation: %v", err)
	}
	if binding == nil || binding.AgentName != "myrig/worker" {
		t.Fatalf("binding = %#v, want sticky agent binding myrig/worker", binding)
	}

	// A successfully routed inbound message cold-wakes the agent's named
	// session in a detached goroutine. Wait for that to finish before the test
	// returns; otherwise t.TempDir() cleanup races the session-name-lock write
	// (see awaitColdWakeMaterialized).
	awaitColdWakeMaterialized(t, fs, "myrig/worker")
}

func TestHandleExtMsgInboundDefaultRouteUnknownAgentStaysUnbound(t *testing.T) {
	fs, srv, services, ref := newExtMsgAgentBindingFixture(t)
	fs.cfg.ExtMsg = config.ExtMsgConfig{
		DefaultRoutes: []config.ExtMsgDefaultRoute{
			{Provider: "discord", Agent: "myrig/ghost"},
		},
	}

	rec := postExtMsg(t, fs, srv, "/extmsg/inbound", map[string]any{
		"message": map[string]any{
			"provider_message_id": "msg-default-2",
			"conversation":        conversationBody(ref),
			"actor":               map[string]any{"id": "user-1", "display_name": "User One", "is_bot": false},
			"text":                "hello?",
			"received_at":         time.Now().UTC().Format(time.RFC3339),
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var result extmsg.InboundResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode inbound result: %v", err)
	}
	if result.TargetSessionID != "" || result.TargetAgentName != "" {
		t.Fatalf("result routed (%q/%q), want unrouted on unresolvable route agent", result.TargetSessionID, result.TargetAgentName)
	}
	binding, err := services.Bindings.ResolveByConversation(context.Background(), ref)
	if err != nil {
		t.Fatalf("ResolveByConversation: %v", err)
	}
	if binding != nil {
		t.Fatalf("binding = %#v, want none for unresolvable route agent", binding)
	}
}

// awaitColdWakeMaterialized blocks until the detached inbound cold-wake
// goroutine (dispatched as `go s.extmsgNotifyInboundMembers` in
// huma_handlers_extmsg.go) has materialized agentName's named session. That
// goroutine reserves session-name locks under <city>/.gc/session-name-locks
// while creating the session bead. A test that posts a default-routed inbound
// message and returns without waiting lets t.TempDir()'s RemoveAll race that
// lock-file creation, failing cleanup with "directory not empty". Resolving the
// session confirms the reservation has completed, so the lock dir is stable
// before teardown. This is the same barrier TestHandleExtMsgInboundDefaultRoute
// BindsAndColdWakes already relies on.
func awaitColdWakeMaterialized(t *testing.T, fs *fakeState, agentName string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if id, err := session.ResolveSessionID(fs.cityBeadStore, agentName); err == nil && id != "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for default-routed inbound to cold-wake %s", agentName)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
