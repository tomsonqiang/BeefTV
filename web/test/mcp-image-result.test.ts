import { expect, test } from "bun:test";
import { parseBackendGenerationResult } from "../src/services/api/generation-task";
import type { GenerationTask } from "../src/services/api/task-center";

test("image task outputs keep the stored resource on the parsed result", () => {
    const task: GenerationTask = {
        id: "task-mcp-image",
        type: "canvas_image",
        status: "succeeded",
        prompt: "一只猫",
        attempts: 1,
        createdAt: "",
        updatedAt: "",
        outcome: { phase: "ready", regenerate: "allowed", reason: "result_ready" },
        resultJson: JSON.stringify({ mode: "image", images: [{ dataUrl: "", mimeType: "image/png" }] }),
        outputs: [{ outputIndex: 0, mediaType: "image", resourceId: "res-image-1" }],
    };
    const result = parseBackendGenerationResult(task);
    expect(result.mode).toBe("image");
    expect(result.images?.[0]?.storageKey).toBe("resource:res-image-1");
    expect(task.outcome?.phase).toBe("ready");
});
