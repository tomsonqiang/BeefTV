package generation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"infinite-canvas/backend/internal/channelurl"
	"infinite-canvas/backend/internal/outbound"
	"infinite-canvas/backend/internal/protocol"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// executeMCPTool 在声明式协议的 HTTP 出站旁路调用 MCP。
// 渠道地址就是 MCP 端点；密钥只放在插件声明的请求头里，不进入 URL、日志请求或错误文本。
func executeMCPTool(ctx context.Context, config Config, spec protocol.RequestSpec) ([]byte, string, error) {
	if err := spec.Validate(); err != nil {
		return nil, "", RequestPreparationError{Cause: err}
	}
	started := time.Now()
	endpoint, err := mcpEndpoint(config.BaseURL)
	if err != nil {
		return nil, "", err
	}
	headerName, token, err := mcpCredential(config, spec.Auth)
	if err != nil {
		return nil, "", err
	}
	body, callErr := callMCPTool(ctx, endpoint, headerName, token, spec.Tool, spec.Body)
	observeMCPCall(ctx, endpoint, started, body, callErr)
	if callErr != nil {
		return nil, "", callErr
	}
	return body, "application/json", nil
}

func mcpEndpoint(baseURL string) (string, error) {
	cleaned := channelurl.CleanBase(baseURL)
	parsed, err := url.Parse(cleaned)
	if err != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return "", PreparationUserError{Category: CategoryInvalidParams, Reason: "MCP 端点不能在地址里带查询参数、片段或账号", Action: "请只填写服务地址，把密钥填在密钥栏"}
	}
	if _, err := outbound.ValidateOutboundURL(cleaned); err != nil {
		return "", redactSecret(err, "")
	}
	return parsed.String(), nil
}

func mcpCredential(config Config, auth protocol.ManifestAuth) (string, string, error) {
	if !strings.EqualFold(strings.TrimSpace(auth.Type), "header") {
		return "", "", PreparationUserError{Category: CategoryAuth, Reason: "MCP 渠道需要按请求头传递密钥", Action: "请使用声明了请求头鉴权的接口"}
	}
	headerName := strings.TrimSpace(auth.Header)
	if headerName == "" {
		return "", "", PreparationUserError{Category: CategoryAuth, Reason: "插件没有声明密钥请求头名称", Action: "请更换已声明请求头名称的接口"}
	}
	token := ProtocolCredentialField(config, auth.Field)
	if token == "" {
		return "", "", PreparationUserError{Category: CategoryAuth, Reason: "渠道密钥未填写", Action: "请在模型服务的密钥栏填写 " + headerName}
	}
	return headerName, token, nil
}

func callMCPTool(ctx context.Context, endpoint, headerName, token, tool string, arguments any) ([]byte, error) {
	if strings.TrimSpace(tool) == "" {
		return nil, RequestPreparationError{Cause: fmt.Errorf("MCP 工具名缺失")}
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	httpClient := outbound.OutboundHTTPClient(HTTPTimeout)
	base := httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	httpClient.Transport = mcpHeaderRoundTripper{base: base, name: headerName, value: token}
	client := mcp.NewClient(&mcp.Implementation{Name: "BeefTV", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: endpoint, HTTPClient: httpClient, MaxRetries: -1, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("连接 MCP 服务失败：%w", redactSecret(err, token))
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: arguments})
	if err != nil {
		return nil, fmt.Errorf("调用 MCP 工具失败：%w", redactSecret(err, token))
	}
	payload, err := mcpResultPayload(result)
	if err != nil {
		return nil, redactSecret(err, token)
	}
	return payload, nil
}

type mcpHeaderRoundTripper struct {
	base        http.RoundTripper
	name, value string
}

func (t mcpHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if clone.Header == nil {
		clone.Header = make(http.Header)
	}
	// 保留插件声明的头名大小写。Header.Set 会把 apiToken 收成 Apitoken。
	clone.Header[t.name] = []string{t.value}
	return t.base.RoundTrip(clone)
}

func mcpResultPayload(result *mcp.CallToolResult) ([]byte, error) {
	if result == nil {
		return nil, fmt.Errorf("MCP 工具没有返回结果")
	}
	if result.IsError {
		text := strings.TrimSpace(mcpResultText(result))
		if text == "" {
			text = "MCP 工具返回失败"
		}
		return nil, fmt.Errorf("%s", text)
	}
	if result.StructuredContent != nil {
		data, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return nil, fmt.Errorf("MCP 工具结果无法编码：%w", err)
		}
		if len(data) > 0 && string(data) != "null" {
			return data, nil
		}
	}
	text := strings.TrimSpace(mcpResultText(result))
	if json.Valid([]byte(text)) {
		return []byte(text), nil
	}
	if text == "" {
		return nil, fmt.Errorf("MCP 工具没有返回可解析的结果")
	}
	return nil, fmt.Errorf("MCP 工具返回的不是 JSON")
}

func mcpResultText(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	parts := make([]string, 0, len(result.Content))
	for _, item := range result.Content {
		text, ok := item.(*mcp.TextContent)
		if ok && strings.TrimSpace(text.Text) != "" {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func observeMCPCall(ctx context.Context, endpoint string, started time.Time, body []byte, callErr error) {
	runtime, ok := RuntimeFromContext(ctx)
	if !ok || runtime.Receipts == nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return
	}
	status := http.StatusOK
	if callErr != nil {
		status = http.StatusBadGateway
	}
	runtime.Receipts.Observe(TransportObservation{
		Request: req, StartedAt: started, StatusCode: status, Body: body, Err: callErr,
		Dispatched: callErr == nil || len(body) > 0, HTTPStatus: status, ReceivedBytes: int64(len(body)),
	})
}

func redactSecret(err error, secret string) error {
	if err == nil || secret == "" || !strings.Contains(err.Error(), secret) {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), secret, "[redacted]"))
}
