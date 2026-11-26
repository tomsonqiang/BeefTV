package taskdelivery

import (
	"testing"

	"infinite-canvas/backend/internal/model"
	localtask "infinite-canvas/backend/internal/task"
)

func TestIngestedImageDataURLBecomesResourceAndReadyOutcome(t *testing.T) {
	ingestor, _, _, _, _ := newIngestHarness(t, ingestNopQuota{})
	stored, err := ingestor.IngestResult("user-1", map[string]interface{}{
		"mode":   "image",
		"images": []any{map[string]any{"dataUrl": tinyPNGDataURL, "mimeType": "image/png"}},
	}, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	images, _ := stored["images"].([]any)
	if len(images) != 1 {
		t.Fatalf("images = %#v", stored["images"])
	}
	item, _ := images[0].(map[string]any)
	resourceID, _ := item["resourceId"].(string)
	if resourceID == "" || item["storageKey"] != "resource:"+resourceID {
		t.Fatalf("入库结果 = %#v", item)
	}
	outcome := localtask.ComputeOutcome(model.Task{Status: model.TaskStatusSucceeded, Type: "canvas_image"}, localtask.ResultStateReady, localtask.OutcomeFacts{})
	if outcome.Phase != "ready" || outcome.Regenerate != "allowed" || outcome.Reason != "result_ready" {
		t.Fatalf("outcome = %#v", outcome)
	}
}
