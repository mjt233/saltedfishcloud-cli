// Package client 的测试文件，覆盖核心 HTTP 客户端行为。
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestDoJSON_AddsApiTicketHeaderAndUnwrapsData 验证 GetJSON 正确注入鉴权头并从 data 字段解包响应。
func TestDoJSON_AddsApiTicketHeaderAndUnwrapsData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "ApiTicket ticket-1" {
			t.Fatalf("unexpected auth header: %s", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{"name": "demo"},
			"msg":  "OK",
		})
	}))
	defer srv.Close()

	cli := NewAPIClient(srv.URL, "ticket-1")
	var resp struct {
		Name string `json:"name"`
	}
	if err := cli.GetJSON(context.Background(), "/api/openApi/test", nil, &resp); err != nil {
		t.Fatalf("GetJSON returned error: %v", err)
	}
	if resp.Name != "demo" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

// TestGetFeatureVersion_ReadsTopLevelVersion 验证 GetFeatureVersion 直接读取顶层 version 字段而非 data。
func TestGetFeatureVersion_ReadsTopLevelVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"version": "3.1.2"})
	}))
	defer srv.Close()

	cli := NewAPIClient(srv.URL, "")
	version, err := cli.GetFeatureVersion(context.Background())
	if err != nil || version != "3.1.2" {
		t.Fatalf("unexpected result: %q %v", version, err)
	}
}

// TestDoJSON_BusinessErrorPreservesCodeAndMessage 验证业务层错误码和消息被正确映射为 error。
func TestDoJSON_BusinessErrorPreservesCodeAndMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":         200,
			"businessCode": 403,
			"data":         nil,
			"msg":          "no permission",
		})
	}))
	defer srv.Close()

	cli := NewAPIClient(srv.URL, "ticket-x")
	var out any
	err := cli.GetJSON(context.Background(), "/api/openApi/restricted", nil, &out)
	if err == nil {
		t.Fatal("expected error for business error response, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "403") || !strings.Contains(errMsg, "no permission") {
		t.Fatalf("error message should contain code and message, got: %s", errMsg)
	}
}

// TestGetJSON_SendsQueryParams 验证 GetJSON 将 url.Values 拼接到请求 URL。
func TestGetJSON_SendsQueryParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") != "/test/dir" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{"ok": true},
			"msg":  "OK",
		})
	}))
	defer srv.Close()

	cli := NewAPIClient(srv.URL, "t")
	q := url.Values{"path": {"/test/dir"}}
	var out struct {
		Ok bool `json:"ok"`
	}
	if err := cli.GetJSON(context.Background(), "/api/openApi/ls", q, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestPostJSON_SendsJSONBodyAndUnwrapsData 验证 PostJSON 发送 JSON 请求体并从 data 字段解包响应。
func TestPostJSON_SendsJSONBodyAndUnwrapsData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["key"] != "val" {
			t.Fatalf("unexpected body: %v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{"created": true},
			"msg":  "OK",
		})
	}))
	defer srv.Close()

	cli := NewAPIClient(srv.URL, "t")
	var out struct {
		Created bool `json:"created"`
	}
	if err := cli.PostJSON(context.Background(), "/api/openApi/create", map[string]any{"key": "val"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Created {
		t.Fatalf("expected Created=true, got %v", out)
	}
}

// TestDeleteJSON_SendsDeleteRequest 验证 DeleteJSON 使用 DELETE 方法并携带查询参数和请求体。
func TestDeleteJSON_SendsDeleteRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Query().Get("uid") != "1" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{"deleted": true},
			"msg":  "OK",
		})
	}))
	defer srv.Close()

	cli := NewAPIClient(srv.URL, "t")
	q := url.Values{"uid": {"1"}}
	var out struct {
		Deleted bool `json:"deleted"`
	}
	if err := cli.DeleteJSON(context.Background(), "/api/openApi/rm", q, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Deleted {
		t.Fatalf("expected Deleted=true, got %v", out)
	}
}

// TestDownload_ReturnsRawResponse 验证 Download 直接返回原始 HTTP 响应，不进行 JSON 解析。
func TestDownload_ReturnsRawResponse(t *testing.T) {
	content := "binary-content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(content))
	}))
	defer srv.Close()

	cli := NewAPIClient(srv.URL, "t")
	resp, err := cli.Download(context.Background(), "/api/openApi/diskFile/download/v1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != content {
		t.Fatalf("unexpected body: %q", body)
	}
}

// TestDownload_ReturnsErrorOnHTTPStatus4xx 验证 Download 在 HTTP 状态码 >= 400 时直接返回错误。
func TestDownload_ReturnsErrorOnHTTPStatus4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	cli := NewAPIClient(srv.URL, "t")
	resp, err := cli.Download(context.Background(), "/api/openApi/diskFile/download/v1", nil)
	if err == nil {
		if resp != nil {
			resp.Body.Close()
		}
		t.Fatal("expected error for HTTP 403 response, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("error should mention HTTP status 403, got: %s", err.Error())
	}
}

// TestNewAPIClient_NormalizesTrailingSlash 验证 NewAPIClient 去除 baseURL 末尾的斜杠。
func TestNewAPIClient_NormalizesTrailingSlash(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// 路径应为 /api/openApi/test，不能出现双斜杠
		if strings.Contains(r.URL.Path, "//") {
			t.Fatalf("double slash in path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{},
			"msg":  "OK",
		})
	}))
	defer srv.Close()

	// baseURL 含末尾斜杠
	cli := NewAPIClient(srv.URL+"/", "t")
	var out any
	_ = cli.GetJSON(context.Background(), "/api/openApi/test", nil, &out)
	if !called {
		t.Fatal("server was not called")
	}
}

// TestNewAPIClient_HasDefaultTimeout 验证 NewAPIClient 创建的 http.Client 携带默认超时时间。
func TestNewAPIClient_HasDefaultTimeout(t *testing.T) {
	cli := NewAPIClient("http://localhost", "t")
	if cli.httpClient.Timeout == 0 {
		t.Fatal("expected non-zero default timeout on http.Client, got 0")
	}
}

// TestDoJSON_CodeNon200AndNoBusinessCodeReturnsError 验证 code != 200 且无 businessCode 时返回错误。
func TestDoJSON_CodeNon200AndNoBusinessCodeReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 500,
			"msg":  "internal server error",
		})
	}))
	defer srv.Close()

	cli := NewAPIClient(srv.URL, "t")
	var out any
	err := cli.GetJSON(context.Background(), "/api/openApi/test", nil, &out)
	if err == nil {
		t.Fatal("expected error when code=500 and no businessCode, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error should contain code 500, got: %s", err.Error())
	}
}

// TestDoJSON_HTTP4xxReturnsErrorBeforeDecoding 验证 HTTP 状态码 >= 400 时直接返回错误，不尝试解析 JSON 信封。
func TestDoJSON_HTTP4xxReturnsErrorBeforeDecoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 返回 HTTP 401，响应体为非 JSON（模拟网关或代理返回）
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("Unauthorized"))
	}))
	defer srv.Close()

	cli := NewAPIClient(srv.URL, "t")
	var out any
	err := cli.GetJSON(context.Background(), "/api/openApi/test", nil, &out)
	if err == nil {
		t.Fatal("expected error for HTTP 401 response, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error should mention HTTP status 401, got: %s", err.Error())
	}
}

// TestGetFeatureVersion_ReturnsErrorOnHTTPStatus4xx 验证 GetFeatureVersion 在 HTTP 状态码 >= 400 时先返回错误。
func TestGetFeatureVersion_ReturnsErrorOnHTTPStatus4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	cli := NewAPIClient(srv.URL, "")
	version, err := cli.GetFeatureVersion(context.Background())
	if err == nil {
		t.Fatalf("expected error for HTTP 500 response, got version=%q", version)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error should mention HTTP status 500, got: %s", err.Error())
	}
}

// roundTripFunc 允许将函数直接用作 http.RoundTripper，方便在测试中拦截请求。
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// TestDeleteJSON_NilBodySendsNoBody 验证 body == nil 时 DELETE 请求的 req.Body 为 nil 或 http.NoBody，
// 而非包装了空缓冲区的非 nil reader。
func TestDeleteJSON_NilBodySendsNoBody(t *testing.T) {
	okResp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"code":200,"data":{},"msg":"OK"}`)),
	}

	var capturedBody io.ReadCloser
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		capturedBody = req.Body
		return okResp, nil
	})

	cli := &APIClient{
		baseURL:    "http://example.com",
		apiTicket:  "t",
		httpClient: &http.Client{Transport: transport},
	}
	var out any
	if err := cli.DeleteJSON(context.Background(), "/api/openApi/rm", nil, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedBody != nil && capturedBody != http.NoBody {
		t.Fatalf("expected nil or http.NoBody for nil-body DELETE, got non-nil body: %T", capturedBody)
	}
}

// TestUploadFile_SendsMultipartFormData 验证 UploadFile 以 multipart/form-data 格式发送文件。
func TestUploadFile_SendsMultipartFormData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" {
			t.Fatalf("expected multipart/form-data, got: %s", r.Header.Get("Content-Type"))
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		part, err := mr.NextPart()
		if err != nil {
			t.Fatalf("failed to read multipart part: %v", err)
		}
		if part.FormName() != "file" {
			t.Fatalf("unexpected form field: %s", part.FormName())
		}
		if part.FileName() != "hello.txt" {
			t.Fatalf("unexpected file name: %s", part.FileName())
		}
		data, _ := io.ReadAll(part)
		if string(data) != "hello" {
			t.Fatalf("unexpected file content: %s", data)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{"uploaded": true},
			"msg":  "OK",
		})
	}))
	defer srv.Close()

	cli := NewAPIClient(srv.URL, "t")
	var out struct {
		Uploaded bool `json:"uploaded"`
	}
	if err := cli.UploadFile(context.Background(), "/api/openApi/upload", nil, "file", "hello.txt", bytes.NewBufferString("hello"), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Uploaded {
		t.Fatalf("expected Uploaded=true, got %v", out)
	}
}
