// Package service 负责路径解析、远程与本地操作编排，以及用户资料等业务能力。
// 当前文件提供远端磁盘文件列表查询与下载能力。
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"

	progressbar "github.com/schollz/progressbar/v3"

	"github.com/mjt233/saltedfishcloud-cli/internal/client"
	"github.com/mjt233/saltedfishcloud-cli/internal/localfs"
)

// DiskEntry 表示远端磁盘上的一个文件或目录条目的元数据信息。
type DiskEntry struct {
	// Name 是文件或目录名称。
	Name string `json:"name"`
	// Type 为条目类型，通常为 "file" 或 "dir"。
	Type string `json:"type"`
	// Size 是文件字节大小；目录通常为 0。
	Size int64 `json:"size"`
	// Mtime 是最后修改时间，格式由后端决定。
	Mtime string `json:"mtime"`
}

// DiskFileService 提供远端磁盘文件的查询与操作能力。
type DiskFileService struct {
	// client 是底层 HTTP 客户端，用于调用咸鱼云开放接口。
	client *client.APIClient
	// paths 负责将用户输入的原始路径解析为带域和 uid 的结构。
	paths *PathService
}

// NewDiskFileService 构造一个 DiskFileService 实例。
// cli 和 paths 均不应为 nil。
func NewDiskFileService(cli *client.APIClient, paths *PathService) *DiskFileService {
	return &DiskFileService{client: cli, paths: paths}
}

// List 列出指定资源路径下的所有文件与目录条目。
//
// rawPath 支持 [resourceArea:]<path> 格式，域可为 private 或 public。
// local 域在当前切片中不支持，会返回明确错误。
//
// 内部调用 /api/openApi/diskFile/fileList/v1，
// 以 uid 和 path 作为查询参数，并将响应 data 解码为 []DiskEntry。
func (s *DiskFileService) List(ctx context.Context, rawPath string) ([]DiskEntry, error) {
	// 解析资源路径，获取域、uid 和规范化路径
	rp, err := s.paths.Resolve(ctx, rawPath)
	if err != nil {
		return nil, err
	}

	// local 域不走远端接口，当前切片明确拒绝
	if rp.Area == "local" {
		return nil, fmt.Errorf("local 资源域暂不支持远端文件列表操作，请使用 private 或 public 域")
	}

	// 使用已解析的路径调用列表接口
	return s.listByResolved(ctx, rp)
}

// listByResolved 使用已解析的 ResolvedPath 直接调用后端文件列表接口。
// 此方法供内部复用，避免重复解析路径。
func (s *DiskFileService) listByResolved(ctx context.Context, rp ResolvedPath) ([]DiskEntry, error) {
	// 构造查询参数：uid 转字符串，path 保持规范化路径
	q := url.Values{
		"uid":  {strconv.FormatInt(rp.UID, 10)},
		"path": {rp.Path},
	}

	// 调用后端文件列表接口，将 data 解码为条目切片
	var entries []DiskEntry
	if err := s.client.GetJSON(ctx, "/api/openApi/diskFile/fileList/v1", q, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// Download 将远端路径对应的文件或目录递归下载到本地。
//
// remotePath 支持 [resourceArea:]<path> 格式，域可为 private 或 public；local 域不支持，返回明确错误。
// localPath 是本地目标路径：若远端是文件，localPath 为目标文件路径；若远端是目录，localPath 为目标目录路径。
// out 用于输出进度信息等用户可见内容。
//
// 文件与目录的检测策略：先尝试列出 remotePath；若列表成功，视为目录递归下载；若列表失败，视为文件直接下载。
func (s *DiskFileService) Download(ctx context.Context, remotePath, localPath string, out io.Writer) error {
	// 解析资源路径，提前检查域合法性
	rp, err := s.paths.Resolve(ctx, remotePath)
	if err != nil {
		return err
	}

	// local 域不支持远端下载
	if rp.Area == "local" {
		return fmt.Errorf("local 资源域不支持远端下载操作，请使用 private 或 public 域")
	}

	// 尝试列出路径内容；若成功则视为目录，否则检查错误类型
	entries, listErr := s.listByResolved(ctx, rp)
	if listErr == nil {
		// 目录下载：递归处理所有条目
		return s.downloadDir(ctx, rp, localPath, entries, out)
	}

	// 仅当业务错误码明确表示"路径非目录"时回退到文件下载；
	// 其他错误（HTTP 错误、网络错误、鉴权失败、上下文取消等）直接返回，不进行回退。
	var bizErr *client.BusinessError
	if !errors.As(listErr, &bizErr) || bizErr.BusinessCode != client.BusinessCodeNotADirectory {
		return listErr
	}

	// 文件下载：调用 download 接口并写入本地文件
	return s.downloadFile(ctx, rp, localPath, out)
}

// downloadFile 从远端下载单个文件到本地路径。
// 若 Content-Length 可用，会通过进度条展示下载进度。
// 若 localPath 已是目录，文件将下载到该目录下以远端文件基础名命名的路径。
// 若写入过程中出现错误，已创建的不完整文件会被自动删除。
func (s *DiskFileService) downloadFile(ctx context.Context, rp ResolvedPath, localPath string, out io.Writer) error {
	// 若 localPath 已是目录，将文件下载到该目录下以远端文件基础名命名的文件
	if fi, statErr := os.Stat(localPath); statErr == nil && fi.IsDir() {
		localPath = filepath.Join(localPath, path.Base(rp.Path))
	}

	// 构造下载接口查询参数
	q := url.Values{
		"uid":  {strconv.FormatInt(rp.UID, 10)},
		"path": {rp.Path},
	}

	// 发起下载请求，获取二进制流响应
	resp, err := s.client.Download(ctx, "/api/openApi/diskFile/download/v1", q)
	if err != nil {
		return fmt.Errorf("下载文件 %q 失败: %w", rp.Path, err)
	}
	defer resp.Body.Close()

	// 确保本地父目录存在
	if err := localfs.EnsureParentDir(localPath); err != nil {
		return fmt.Errorf("创建目标目录失败: %w", err)
	}

	// 创建本地目标文件
	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("创建本地文件 %q 失败: %w", localPath, err)
	}

	// copyOK 标记写入是否成功；defer 负责关闭文件并在失败时删除不完整文件。
	copyOK := false
	defer func() {
		f.Close()
		if !copyOK {
			// 下载失败时删除已创建的不完整文件，避免遗留损坏数据
			_ = os.Remove(localPath)
		}
	}()

	// 构造进度条，内容长度未知时（-1）以无限制模式运行
	bar := progressbar.NewOptions64(
		resp.ContentLength,
		progressbar.OptionSetWriter(out),
		progressbar.OptionSetDescription(filepath.Base(localPath)),
		progressbar.OptionShowBytes(true),
	)

	// 同时写入文件和进度条
	if _, err := io.Copy(io.MultiWriter(f, bar), resp.Body); err != nil {
		return fmt.Errorf("写入文件 %q 失败: %w", localPath, err)
	}
	// 确保进度条显示完成状态
	_ = bar.Finish()
	copyOK = true
	return nil
}

// downloadDir 将远端目录中的所有条目递归下载到本地目录。
// 若条目为文件，直接下载；若条目为子目录，递归处理。
func (s *DiskFileService) downloadDir(ctx context.Context, rp ResolvedPath, localPath string, entries []DiskEntry, out io.Writer) error {
	// 创建本地目录（含所有中间目录）
	if err := os.MkdirAll(localPath, 0755); err != nil {
		return fmt.Errorf("创建本地目录 %q 失败: %w", localPath, err)
	}

	// 遍历所有条目，按类型分别处理
	for _, entry := range entries {
		// 构造条目的本地路径和远端 ResolvedPath
		entryLocalPath := filepath.Join(localPath, entry.Name)
		entryRp := ResolvedPath{
			Area: rp.Area,
			UID:  rp.UID,
			// 使用 path.Join 确保远端路径始终使用正斜杠
			Path: path.Join(rp.Path, entry.Name),
		}

		if entry.Type == "dir" {
			// 子目录：先列出其内容，再递归下载
			subEntries, err := s.listByResolved(ctx, entryRp)
			if err != nil {
				return fmt.Errorf("列出子目录 %q 失败: %w", entryRp.Path, err)
			}
			if err := s.downloadDir(ctx, entryRp, entryLocalPath, subEntries, out); err != nil {
				return err
			}
		} else {
			// 文件：直接下载到对应本地路径
			if err := s.downloadFile(ctx, entryRp, entryLocalPath, out); err != nil {
				return err
			}
		}
	}
	return nil
}
