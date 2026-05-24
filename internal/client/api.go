// Package client 封装对咸鱼云开放接口的 HTTP 请求，负责鉴权头注入、
// JSON 响应信封解包、业务错误映射以及文件上传和二进制下载等核心能力。
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

// APIClient 持有与咸鱼云服务通信所需的基础配置和可复用 HTTP 客户端。
type APIClient struct {
	// baseURL 是服务根地址，已去除末尾斜杠。
	baseURL string
	// apiTicket 是用于 Authorization 头的永久有效凭据。
	apiTicket string
	// httpClient 是底层 HTTP 客户端，支持外部注入以便测试替换。
	httpClient *http.Client
}

// NewAPIClient 构造一个 APIClient 实例。
// baseURL 会被规范化（去除末尾斜杠）；apiTicket 为空时请求不携带鉴权头。
func NewAPIClient(baseURL, apiTicket string) *APIClient {
	// 去除末尾斜杠，防止拼接路径时产生双斜杠
	normalizedURL := strings.TrimRight(baseURL, "/")
	return &APIClient{
		baseURL:    normalizedURL,
		apiTicket:  apiTicket,
		httpClient: &http.Client{},
	}
}

// apiEnvelope 是咸鱼云标准 JSON 响应的信封结构。
// /api/hello/feature 等特殊接口不使用此结构，单独处理。
type apiEnvelope struct {
	Code         int             `json:"code"`
	BusinessCode int             `json:"businessCode"`
	Data         json.RawMessage `json:"data"`
	Msg          string          `json:"msg"`
}

// addAuthHeader 在请求上注入鉴权头（若 apiTicket 非空）。
func (c *APIClient) addAuthHeader(req *http.Request) {
	if c.apiTicket != "" {
		req.Header.Set("Authorization", "ApiTicket "+c.apiTicket)
	}
}

// buildURL 将路径与查询参数拼接为完整请求 URL。
func (c *APIClient) buildURL(path string, query url.Values) string {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// doJSONRequest 执行 HTTP 请求并将标准信封响应解包到 out。
// 若业务码非零（businessCode != 0），返回包含业务码和消息的错误。
func (c *APIClient) doJSONRequest(req *http.Request, out any) error {
	// 注入鉴权头
	c.addAuthHeader(req)

	// 发起请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	// 解析标准信封
	var envelope apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	// 检查业务层错误码（businessCode 非零视为错误）
	if envelope.BusinessCode != 0 {
		return fmt.Errorf("business error %d: %s", envelope.BusinessCode, envelope.Msg)
	}

	// 将 data 字段解包到调用方提供的目标结构
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("failed to unmarshal data: %w", err)
		}
	}
	return nil
}

// GetJSON 向指定路径发送 GET 请求，将响应信封中的 data 字段解包到 out。
// query 为可选查询参数；out 为 nil 时忽略响应体。
func (c *APIClient) GetJSON(ctx context.Context, path string, query url.Values, out any) error {
	// 构造完整请求 URL
	reqURL := c.buildURL(path, query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	return c.doJSONRequest(req, out)
}

// PostJSON 向指定路径发送 POST 请求，将 body 序列化为 JSON 请求体，
// 并将响应信封中的 data 字段解包到 out。
func (c *APIClient) PostJSON(ctx context.Context, path string, body any, out any) error {
	// 序列化请求体
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return fmt.Errorf("failed to encode request body: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.buildURL(path, nil), &buf)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doJSONRequest(req, out)
}

// DeleteJSON 向指定路径发送 DELETE 请求，携带可选查询参数和 JSON 请求体，
// 并将响应信封中的 data 字段解包到 out。
func (c *APIClient) DeleteJSON(ctx context.Context, path string, query url.Values, body any, out any) error {
	// 序列化可选请求体
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return fmt.Errorf("failed to encode request body: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.buildURL(path, query), &buf)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.doJSONRequest(req, out)
}

// UploadFile 以 multipart/form-data 格式向指定路径上传文件，
// 并将响应信封中的 data 字段解包到 out。
// fileField 为表单字段名，fileName 为文件名，reader 为文件内容来源。
func (c *APIClient) UploadFile(ctx context.Context, path string, query url.Values, fileField, fileName string, reader io.Reader, out any) error {
	// 构造 multipart 请求体
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// 创建文件字段并写入文件内容
	fw, err := mw.CreateFormFile(fileField, fileName)
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(fw, reader); err != nil {
		return fmt.Errorf("failed to write file content: %w", err)
	}

	// 关闭 writer 以写入边界结束标记
	if err := mw.Close(); err != nil {
		return fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.buildURL(path, query), &buf)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return c.doJSONRequest(req, out)
}

// Download 向指定路径发送 GET 请求并直接返回原始 HTTP 响应，供调用方处理二进制流。
// 调用方负责关闭返回响应的 Body。
func (c *APIClient) Download(ctx context.Context, path string, query url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL(path, query), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	// 注入鉴权头
	c.addAuthHeader(req)

	// 直接返回响应，不进行 JSON 解析
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	return resp, nil
}

// featureVersionResponse 是 /api/hello/feature 接口的响应结构，
// 该接口直接在顶层返回 version 字段，不遵循标准信封格式。
type featureVersionResponse struct {
	Version string `json:"version"`
}

// GetFeatureVersion 调用 /api/hello/feature 接口，读取顶层 version 字段并返回。
// 此接口不需要鉴权，且不使用标准信封格式。
func (c *APIClient) GetFeatureVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL("/api/hello/feature", nil), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// 发起请求（不注入鉴权头，该接口为公开接口）
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	// 直接解析顶层 version 字段
	var result featureVersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode feature version response: %w", err)
	}
	return result.Version, nil
}
