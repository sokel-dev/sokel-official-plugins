package main

import (
	"os"
	"strings"
	"testing"
)

func TestInteractionPreviewIsBusinessNeutral(t *testing.T) {
	preview := previewDescription("查看消息", "请审核本次发布计划。")
	if strings.Contains(preview, "告警") || strings.Contains(preview, "认领") || strings.Contains(preview, "[]") {
		t.Fatalf("preview injects a business workflow: %s", preview)
	}
	if !strings.Contains(preview, "请审核本次发布计划。") {
		t.Fatal("preview lost message content")
	}
}

func TestPublicInteractionContractIsGeneric(t *testing.T) {
	manifest, err := os.ReadFile("manifest.yml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(manifest)
	for _, id := range []string{"send_interactive_message", "get_interactive_message", "interaction_action", "send_template_card"} {
		if !strings.Contains(s, "id: "+id+"\n") {
			t.Errorf("missing generic contract %s", id)
		}
	}
	for _, id := range []string{"send_alert", "get_alert", "alert_action"} {
		if strings.Contains(s, "id: "+id+"\n") {
			t.Errorf("obsolete business-specific contract still exposed: %s", id)
		}
	}
}
