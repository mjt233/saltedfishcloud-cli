// Package client 封装对咸鱼云开放接口的 HTTP 请求，负责鉴权头注入、
// JSON 响应信封解包、业务错误映射以及文件上传和二进制下载等核心能力。
package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
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
// 默认超时时间为 30 秒，防止请求长时间挂起。
func NewAPIClient(baseURL, apiTicket string) *APIClient {
	// 去除末尾斜杠，防止拼接路径时产生双斜杠
	normalizedURL := strings.TrimRight(baseURL, "/")
	return &APIClient{
		baseURL:   normalizedURL,
		apiTicket: apiTicket,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// BusinessError 表示后端返回的业务层错误，包含业务错误码和可读消息。
// doJSONRequest 在 businessCode 非零时返回此类型，供调用方通过 errors.As 进行类型断言。
type BusinessError struct {
	// BusinessCode 是后端返回的业务错误码。
	BusinessCode int
	// Msg 是后端返回的可读错误消息。
	Msg string
	// URL 是请求的接口地址。
	URL string
}

// Error 实现 error 接口，格式与原有字符串错误格式保持一致以确保向后兼容。
func (e *BusinessError) Error() string {
	return fmt.Sprintf("business error %d: %s [%s]", e.BusinessCode, e.Msg, e.URL)
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
// 当前咸鱼云后端的开放接口通过 OIDC access token 鉴权，
// 使用标准的 `Authorization: Bearer {token}` 方案（`ApiTicket` 方案已不被后端接受）。
func (c *APIClient) addAuthHeader(req *http.Request) {
	if c.apiTicket != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiTicket)
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

// checkHTTPStatus 在响应状态码 >= 400 时返回错误，错误消息中包含请求地址。
func checkHTTPStatus(resp *http.Response) error {
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http error %d: %s [%s]", resp.StatusCode, resp.Status, resp.Request.URL)
	}
	return nil
}

// doJSONRequest 执行 HTTP 请求并将标准信封响应解包到 out。
// 处理顺序：先检查 HTTP 状态码（>= 400 直接报错），再检查 businessCode，
// 最后检查 code 是否为 200。businessCode 非零时返回业务错误；
// code != 200 且 businessCode 为 0 时返回通用错误。
func (c *APIClient) doJSONRequest(req *http.Request, out any) error {
	return c.doRequestWithClient(c.httpClient, req, out)
}

// doRequestWithClient 使用指定底层 http.Client 执行请求并解包标准信封响应到 out。
// 是 doJSONRequest 的核心实现，允许上传等场景替换为无全局超时的客户端。
func (c *APIClient) doRequestWithClient(httpClient *http.Client, req *http.Request, out any) error {
	// 注入鉴权头
	c.addAuthHeader(req)

	// 发起请求
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w [%s]", err, req.URL)
	}
	defer resp.Body.Close()

	if err := checkHTTPStatus(resp); err != nil {
		return err
	}

	// 解析标准信封
	var envelope apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("failed to decode response: %w [%s]", err, req.URL)
	}

	// businessCode 非零且非 200（成功）时返回业务层错误（优先于 code 检查）
	if envelope.BusinessCode != 0 && envelope.BusinessCode != 200 {
		return &BusinessError{BusinessCode: envelope.BusinessCode, Msg: envelope.Msg, URL: req.URL.String()}
	}

	// code != 200 且无 businessCode 时，视为通用错误（如服务端内部错误）
	if envelope.Code != 200 {
		return fmt.Errorf("server error %d: %s [%s]", envelope.Code, envelope.Msg, req.URL)
	}

	// 将 data 字段解包到调用方提供的目标结构
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("failed to unmarshal data: %w [%s]", err, req.URL)
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

// PostQuery 向指定路径发送不含请求体的 POST 请求，将 query 编码为 URL 查询参数，
// 并将响应信封中的 data 字段解包到 out。
// 适用于参数较简单、直接通过 URL 传递的 POST 接口（如 mkdir）。
func (c *APIClient) PostQuery(ctx context.Context, path string, query url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.buildURL(path, query), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	return c.doJSONRequest(req, out)
}

// PostQueryWithBody 向指定路径发送 POST 请求，将 query 编码为 URL 查询参数，
// 将 body 序列化为 JSON 请求体，并将响应信封中的 data 字段解包到 out。
// 适用于同时需要查询参数和请求体的 POST 接口（如 rename）。
func (c *APIClient) PostQueryWithBody(ctx context.Context, path string, query url.Values, body any, out any) error {
	// 序列化请求体
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return fmt.Errorf("failed to encode request body: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.buildURL(path, query), &buf)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doJSONRequest(req, out)
}

// DeleteJSON 向指定路径发送 DELETE 请求，携带可选查询参数和 JSON 请求体，
// 并将响应信封中的 data 字段解包到 out。
func (c *APIClient) DeleteJSON(ctx context.Context, path string, query url.Values, body any, out any) error {
	// 序列化可选请求体；body 为 nil 时使用 nil 而非空缓冲区，保持语义清晰
	var reqBody io.Reader
	if body != nil {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return fmt.Errorf("failed to encode request body: %w", err)
		}
		reqBody = &buf
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.buildURL(path, query), reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.doJSONRequest(req, out)
}

// UploadFile 以 multipart/form-data 流式向指定路径上传文件，
// 并将响应信封中的 data 字段解包到 out。
// fileField 为表单字段名，fileName 为文件名，reader 为文件内容来源，
// fileSize 为内容已知大小（字节），未知时传 -1 并回退为 chunked 传输。
// 内容边读边发，不会整体缓冲在内存；请求使用无全局超时的客户端，
// 大文件上传的总时长不受 NewAPIClient 默认超时限制，超时控制由 ctx 负责。
func (c *APIClient) UploadFile(ctx context.Context, path string, query url.Values, fileField, fileName string, reader io.Reader, fileSize int64, out any) error {
	// 生成随机 boundary，并用同一 boundary 预先测量 multipart 固定部分（part 头 + 结束边界）的长度
	boundary, err := randomBoundary()
	if err != nil {
		return fmt.Errorf("failed to generate multipart boundary: %w", err)
	}
	fixedLen, err := measureMultipartFixedPart(boundary, fileField, fileName)
	if err != nil {
		return fmt.Errorf("failed to prepare multipart body: %w", err)
	}

	// 通过管道边读边发：后台 goroutine 把 reader 内容经 multipart writer 写入管道，请求体从管道读出
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	if err := mw.SetBoundary(boundary); err != nil {
		return fmt.Errorf("failed to prepare multipart body: %w", err)
	}

	// copyDone 用于回收后台写入 goroutine 的结果，避免 goroutine 泄漏
	copyDone := make(chan error, 1)
	go func() {
		copyDone <- writeMultipartFile(mw, pw, fileField, fileName, reader)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.buildURL(path, query), pr)
	if err != nil {
		// 请求构造失败时关闭管道，让后台写入 goroutine 立即退出
		pw.Close()
		<-copyDone
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	// 大小已知时精确设置 Content-Length，避免 chunked 编码在部分服务器/代理上的兼容性问题
	if fileSize >= 0 {
		req.ContentLength = fixedLen + fileSize
	}

	// 上传使用无全局超时的克隆客户端：与 Download 同理，整个请求时长不可预估，超时交给 ctx
	streamClient := *c.httpClient
	streamClient.Timeout = 0
	err = c.doRequestWithClient(&streamClient, req, out)

	// 等待后台写入 goroutine 结束；请求层错误优先返回，其次才是本地读写错误
	copyErr := <-copyDone
	if err != nil {
		return err
	}
	if copyErr != nil {
		return copyErr
	}
	return nil
}

// writeMultipartFile 把 reader 的内容以指定字段写入 mw，并在结束后关闭 writer 与管道。
// 任一环节失败时通过 pw.CloseWithError 让请求侧立即感知并终止传输。
func writeMultipartFile(mw *multipart.Writer, pw *io.PipeWriter, fileField, fileName string, reader io.Reader) error {
	// 创建文件字段
	fw, err := mw.CreateFormFile(fileField, fileName)
	if err != nil {
		pw.CloseWithError(fmt.Errorf("failed to create form file: %w", err))
		return err
	}
	// 把内容拷贝进 multipart part
	if _, err := io.Copy(fw, reader); err != nil {
		pw.CloseWithError(fmt.Errorf("failed to write file content: %w", err))
		return err
	}
	// 写入结束边界
	if err := mw.Close(); err != nil {
		pw.CloseWithError(fmt.Errorf("failed to close multipart writer: %w", err))
		return err
	}
	return pw.Close()
}

// measureMultipartFixedPart 用给定 boundary 构造与实际上传一致的 multipart part 头和结束边界，
// 返回其字节长度，供流式上传时精确设置 Content-Length。
// CreateFormFile 的输出是确定性的，测量结果与实际请求中的固定部分完全一致。
func measureMultipartFixedPart(boundary, fileField, fileName string) (int64, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.SetBoundary(boundary); err != nil {
		return 0, err
	}
	if _, err := mw.CreateFormFile(fileField, fileName); err != nil {
		return 0, err
	}
	if err := mw.Close(); err != nil {
		return 0, err
	}
	return int64(buf.Len()), nil
}

// randomBoundary 生成 60 个十六进制字符的随机 boundary，避免与文件内容冲突。
func randomBoundary() (string, error) {
	var buf [30]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
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

	// 克隆 HTTP 客户端并将超时设为 0，避免全局默认超时中断大文件流式下载；
	// 超时控制改由调用方通过 context 实现。
	streamClient := *c.httpClient
	streamClient.Timeout = 0

	// 直接返回响应，不进行 JSON 解析
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w [%s]", err, req.URL)
	}
	if err := checkHTTPStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return resp, nil
}

// featureVersionResponse 是 /api/hello/feature 接口的响应结构，
// 该接口直接在顶层返回 version 字段，不遵循标准信封格式。
type featureVersionResponse struct {
	Version string `json:"version"`
}

// RemoteVersion 调用 /api/hello/feature 接口获取服务端版本号。
// 内部委托 GetFeatureVersion 实现，对外提供更简洁的方法名。
func (c *APIClient) RemoteVersion(ctx context.Context) (string, error) {
	return c.GetFeatureVersion(ctx)
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
		return "", fmt.Errorf("http request failed: %w [%s]", err, req.URL)
	}
	defer resp.Body.Close()

	if err := checkHTTPStatus(resp); err != nil {
		return "", err
	}

	// 直接解析顶层 version 字段
	var result featureVersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode feature version response: %w [%s]", err, req.URL)
	}
	return result.Version, nil
}
