package generation

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/modelcatalog"
	"infinite-canvas/backend/internal/protocol"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpImageFixtureToken = "fixture-api-token"

func TestMCPImageChannelSubmitsThenPollsAndDownloads(t *testing.T) {
	pngBytes := testPNG(t)
	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.String(), mcpImageFixtureToken) || r.Header.Get("apiToken") != "" || len(r.Header["apiToken"]) > 0 {
			t.Errorf("image download carried the channel secret: url=%s", r.URL.String())
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer images.Close()

	var submits, polls atomic.Int32
	mcpServer := newMCPImageServer(t, &submits, &polls, images.URL+"/generated.png", false)
	defer mcpServer.Close()

	receipts := &recordingReceipts{}
	ctx := WithProtocolRegistry(context.Background(), kreadoImageRegistry(t))
	ctx = WithRuntime(ctx, Runtime{Receipts: receipts, Call: CallMeta{Capability: "image", TaskID: "task-mcp-image"}})
	result, err := RunImageTask(ctx, Input{
		Mode: "image", Prompt: "一只猫",
		Config: Config{BaseURL: mcpServer.URL, APIKey: mcpImageFixtureToken, Model: "1022", InterfaceType: "kreadoai-agent-art-image"},
	})
	if err != nil {
		t.Fatalf("文生图失败：%v", err)
	}
	if submits.Load() != 1 || polls.Load() < 1 {
		t.Fatalf("submit=%d poll=%d，想要提交一次并查询原任务", submits.Load(), polls.Load())
	}
	imagesOut, _ := result["images"].([]any)
	if result["mode"] != "image" || len(imagesOut) != 1 {
		t.Fatalf("结果 = %#v", result)
	}
	item, _ := imagesOut[0].(map[string]any)
	dataURL, _ := item["dataUrl"].(string)
	if !strings.HasPrefix(dataURL, "data:image/png;base64,") {
		t.Fatalf("图片没有按现有结果下载入库：%#v", item)
	}
	for _, observation := range receipts.snapshot() {
		if observation.Request == nil {
			t.Fatal("回执缺少请求")
		}
		if strings.Contains(observation.Request.URL.String(), mcpImageFixtureToken) || observation.Request.Header.Get("apiToken") != "" || len(observation.Request.Header["apiToken"]) > 0 {
			t.Fatalf("回执带上了密钥：%s", observation.Request.URL.String())
		}
		if strings.Contains(string(observation.Body), mcpImageFixtureToken) {
			t.Fatal("回执正文带上了密钥")
		}
	}
	var logged model.ApiCallLog
	ObserveTransportReceipt(receipts.snapshot()[0], nil, nil, func(entry model.ApiCallLog) error {
		logged = entry
		return nil
	})
	if logged.ProviderRequestID != "img-mcp-1" {
		t.Fatalf("上游任务号 = %q", logged.ProviderRequestID)
	}
	if strings.Contains(logged.UpstreamURL+logged.RequestBody+logged.ResponseBody, mcpImageFixtureToken) {
		t.Fatal("调用日志带上了密钥")
	}
}

func TestMCPImageChannelResumesWithoutSubmittingAgain(t *testing.T) {
	pngBytes := testPNG(t)
	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer images.Close()
	var submits, polls atomic.Int32
	mcpServer := newMCPImageServer(t, &submits, &polls, images.URL+"/generated.png", false)
	defer mcpServer.Close()
	ctx := WithProtocolRegistry(context.Background(), kreadoImageRegistry(t))
	ctx = WithCallMeta(ctx, CallMeta{ProviderRequestID: "img-mcp-1", Capability: "image"})
	if _, err := RunImageTask(ctx, Input{
		Mode: "image", Prompt: "一只猫",
		Config: Config{BaseURL: mcpServer.URL, APIKey: mcpImageFixtureToken, Model: "1022", InterfaceType: "kreadoai-agent-art-image"},
	}); err != nil {
		t.Fatal(err)
	}
	if submits.Load() != 0 || polls.Load() < 1 {
		t.Fatalf("恢复任务 submit=%d poll=%d", submits.Load(), polls.Load())
	}
}

func TestMCPImageChannelFailureAndReferenceStayOffTheSubmit(t *testing.T) {
	var submits, polls atomic.Int32
	mcpServer := newMCPImageServer(t, &submits, &polls, "", true)
	defer mcpServer.Close()
	ctx := WithProtocolRegistry(context.Background(), kreadoImageRegistry(t))
	_, err := RunImageTask(ctx, Input{
		Mode: "image", Prompt: "一只猫",
		Config: Config{BaseURL: mcpServer.URL, APIKey: mcpImageFixtureToken, Model: "1022", InterfaceType: "kreadoai-agent-art-image"},
	})
	if err == nil || !strings.Contains(err.Error(), "上游拒绝了这次文生图") {
		t.Fatalf("失败结果 = %v", err)
	}
	if strings.Contains(err.Error(), mcpImageFixtureToken) {
		t.Fatal("错误文本带上了密钥")
	}
	if submits.Load() != 1 {
		t.Fatalf("失败任务提交次数 = %d", submits.Load())
	}

	var blocked atomic.Int32
	blockedServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		blocked.Add(1)
	}))
	defer blockedServer.Close()
	_, err = RunImageTask(WithProtocolRegistry(context.Background(), kreadoImageRegistry(t)), Input{
		Mode: "image", Prompt: "一只猫",
		ReferenceImages: []Media{{URL: "https://example.invalid/ref.png"}},
		Config:          Config{BaseURL: blockedServer.URL, APIKey: mcpImageFixtureToken, Model: "1022", InterfaceType: "kreadoai-agent-art-image"},
	})
	if err == nil || !strings.Contains(err.Error(), "只支持文生图") || blocked.Load() != 0 {
		t.Fatalf("参考图 err=%v calls=%d", err, blocked.Load())
	}
}

func TestMCPImageChannelCatalogDoesNotCallUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("读取模型打到了上游")
	}))
	defer upstream.Close()
	registry := kreadoImageRegistry(t)
	items, err := FetchChannelModelCatalog(context.Background(), &model.User{ID: "user"}, ChannelModelsRequest{
		BaseURL: upstream.URL, APIKey: mcpImageFixtureToken, APIFormat: "openai", Protocol: "kreadoai-agent-art-image",
	}, nil, func(context.Context, string, string, string, []modelcatalog.ChannelHeader) ([]byte, error) {
		t.Fatal("读取模型发起了目录请求")
		return nil, nil
	}, nil, func(id string) (protocol.Adapter, bool) {
		return registry.Resolve(id)
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.ID == "1022" && item.DisplayName == "aihub_gpt-image-2.6-sunburst" && item.ModelType == "image" {
			found = true
		}
	}
	if !found {
		t.Fatalf("目录 = %#v", items)
	}
}

func TestMCPEndpointRejectsSecretInURL(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	_, err := RunImageTask(WithProtocolRegistry(context.Background(), kreadoImageRegistry(t)), Input{
		Mode: "image", Prompt: "一只猫",
		Config: Config{BaseURL: server.URL + "?apiToken=" + mcpImageFixtureToken, APIKey: mcpImageFixtureToken, Model: "1022", InterfaceType: "kreadoai-agent-art-image"},
	})
	if err == nil || calls.Load() != 0 || strings.Contains(err.Error(), mcpImageFixtureToken) {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func kreadoImageRegistry(t *testing.T) *protocol.Registry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "plugin-packages", "kreadoai-agent-art-image", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	adapters, err := protocol.LoadInstalledProviders(data)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := protocol.NewRegistry(adapters...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func newMCPImageServer(t *testing.T, submits, polls *atomic.Int32, imageURL string, fail bool) *httptest.Server {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "submit_ai_image_task"}, func(_ context.Context, _ *mcp.CallToolRequest, in struct {
		UserPrompt   string `json:"userPrompt"`
		Quantity     int    `json:"quantity"`
		ModelsSource int    `json:"modelsSource"`
	}) (*mcp.CallToolResult, map[string]any, error) {
		submits.Add(1)
		if in.UserPrompt == "" || in.Quantity != 1 || in.ModelsSource != 1022 {
			t.Errorf("提交参数 = %#v", in)
		}
		return nil, map[string]any{"taskId": "img-mcp-1", "status": 1}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "get_ai_image_task_detail"}, func(_ context.Context, _ *mcp.CallToolRequest, in struct {
		TaskID string `json:"taskId"`
	}) (*mcp.CallToolResult, map[string]any, error) {
		polls.Add(1)
		if in.TaskID != "img-mcp-1" {
			t.Errorf("查询任务号 = %q", in.TaskID)
		}
		if fail {
			return nil, map[string]any{"taskId": in.TaskID, "status": 4, "message": "上游拒绝了这次文生图"}, nil
		}
		return nil, map[string]any{"taskId": in.TaskID, "status": 3, "images": []any{map[string]any{"url": imageURL}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" || strings.Contains(r.URL.String(), mcpImageFixtureToken) {
			t.Errorf("MCP 地址带上了密钥：%s", r.URL.String())
		}
		if got := r.Header.Values("apiToken"); len(got) != 1 || got[0] != mcpImageFixtureToken {
			t.Errorf("apiToken 头 = %#v 全部请求头 = %#v", got, r.Header)
		}
		handler.ServeHTTP(w, r)
	}))
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
