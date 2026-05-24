// Package service 负责路径解析、远程与本地操作编排，以及用户资料等业务能力。
// 当前文件提供远端磁盘文件列表查询能力。
package service

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/mjt233/saltedfishcloud-cli/internal/client"
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
