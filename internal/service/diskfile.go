// Package service 负责路径解析、远程与本地操作编排，以及用户资料等业务能力。
// 当前文件提供远端磁盘文件列表查询与下载能力。
package service

import (
	"context"
	"encoding/json"
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

// removeLocalFile 允许在测试中替换不完整下载文件的清理行为。
var removeLocalFile = os.Remove

// DiskEntry 表示远端磁盘上的一个文件或目录条目的元数据信息。
type DiskEntry struct {
	// Name 是文件或目录名称。
	Name string `json:"name"`
	// Type 为条目类型，"dir" 或 "file"；由 UnmarshalJSON 根据 dir 字段推导。
	Type string
	// Size 是文件字节大小；目录通常为 -1，由 UnmarshalJSON 从字符串解析。
	Size int64
	// Mtime 是最后修改时间戳（毫秒），由后端以字符串形式返回。
	Mtime string `json:"mtime"`
}

// diskEntryRaw 是 DiskEntry 的原始 JSON 映射，用于处理接口返回的非标字段类型。
type diskEntryRaw struct {
	Name  string      `json:"name"`
	Dir   bool        `json:"dir"`
	Size  interface{} `json:"size"` // 接口以字符串形式返回，如 "-1"
	Mtime string      `json:"mtime"`
}

// UnmarshalJSON 实现自定义反序列化，兼容接口返回的 size 字符串和 dir 布尔字段。
func (e *DiskEntry) UnmarshalJSON(data []byte) error {
	var raw diskEntryRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	e.Name = raw.Name
	e.Mtime = raw.Mtime
	// 根据 dir 布尔字段推导类型字符串
	if raw.Dir {
		e.Type = "dir"
	} else {
		e.Type = "file"
	}
	// size 可能是字符串或数字，统一转换为 int64
	switch v := raw.Size.(type) {
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse size %q: %w", v, err)
		}
		e.Size = n
	case float64:
		e.Size = int64(v)
	default:
		e.Size = 0
	}
	return nil
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
		return nil, fmt.Errorf("local resource area does not support remote file listing, please use private or public area")
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
// out 用于输出进度信息等用户可见内容，为 nil 时进度信息将被丢弃。
//
// 文件与目录的检测策略：列出 remotePath 的父目录，从列表中查找目标条目，
// 根据条目的 dir 字段判断路径类型为"文件"还是"目录"。
func (s *DiskFileService) Download(ctx context.Context, remotePath, localPath string, out io.Writer) error {
	// 解析资源路径，提前检查域合法性
	rp, err := s.paths.Resolve(ctx, remotePath)
	if err != nil {
		return err
	}

	// local 域不支持远端下载
	if rp.Area == "local" {
		return fmt.Errorf("local resource area does not support remote download, please use private or public area")
	}

	// 确保 out 不为 nil，避免进度条写入 panic
	if out == nil {
		out = io.Discard
	}

	// 列出父目录内容，从列表中查找目标条目以判断路径类型
	entry, _, err := s.findEntryInParent(ctx, rp)
	if err != nil {
		return err
	}

	if entry != nil && entry.Type == "dir" {
		// 目录下载：列出目录自身内容后递归处理
		dirEntries, listErr := s.listByResolved(ctx, rp)
		if listErr != nil {
			return fmt.Errorf("failed to list directory %q: %w", rp.Path, listErr)
		}
		return s.downloadDir(ctx, rp, localPath, dirEntries, out)
	}

	// 文件下载：调用 download 接口并写入本地文件
	return s.downloadFile(ctx, rp, localPath, out)
}

// findEntryInParent 列出目标路径的父目录，从条目列表中查找与目标名称匹配的条目。
// 返回值：entry 为匹配到的条目（未找到时为 nil）；entries 为父目录的完整条目列表；
// 当父目录列出失败或目标路径为根路径时返回错误。
func (s *DiskFileService) findEntryInParent(ctx context.Context, rp ResolvedPath) (*DiskEntry, []DiskEntry, error) {
	// 计算父目录路径和目标条目名称
	parentPath := path.Dir(rp.Path)
	baseName := path.Base(rp.Path)

	// 根路径无法作为文件处理
	if baseName == "/" || baseName == "." {
		return nil, nil, fmt.Errorf("cannot download root path as a file")
	}

	// 构造父目录的 ResolvedPath
	parentRp := ResolvedPath{Area: rp.Area, UID: rp.UID, Path: parentPath}

	// 列出父目录内容
	entries, err := s.listByResolved(ctx, parentRp)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list parent directory %q: %w", parentPath, err)
	}

	// 在条目列表中查找目标名称
	for i := range entries {
		if entries[i].Name == baseName {
			return &entries[i], entries, nil
		}
	}

	// 未找到匹配条目，返回 nil 表示条目不存在（仍返回列表供调用方参考）
	return nil, entries, nil
}

// downloadFile 从远端下载单个文件到本地路径。
// 若 Content-Length 可用，会通过进度条展示下载进度。
// 若 localPath 已是目录，文件将下载到该目录下以远端文件基础名命名的路径。
// 若写入过程中出现错误，已创建的不完整文件会被自动删除。
func (s *DiskFileService) downloadFile(ctx context.Context, rp ResolvedPath, localPath string, out io.Writer) (err error) {
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
		return fmt.Errorf("failed to download file %q: %w", rp.Path, err)
	}
	defer resp.Body.Close()

	// 确保本地父目录存在
	if err := localfs.EnsureParentDir(localPath); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// 创建本地目标文件
	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file %q: %w", localPath, err)
	}

	// copyOK 标记写入是否成功；defer 负责关闭文件并在失败时删除不完整文件。
	copyOK := false
	defer func() {
		f.Close()
		if !copyOK {
			// 下载失败时删除已创建的不完整文件，避免遗留损坏数据
			if removeErr := removeLocalFile(localPath); removeErr != nil {
				err = errors.Join(err, fmt.Errorf("failed to clean up incomplete file %q: %w", localPath, removeErr))
			}
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
		return fmt.Errorf("failed to write file %q: %w", localPath, err)
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
		return fmt.Errorf("failed to create local directory %q: %w", localPath, err)
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
				return fmt.Errorf("failed to list subdirectory %q: %w", entryRp.Path, err)
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

// Remove 删除远端路径对应的文件或目录。
// rawPath 支持 [resourceArea:]<path> 格式，域可为 private 或 public。
// 内部将路径拆分为父目录和文件名，调用 /api/openApi/diskFile/delete/v1。
func (s *DiskFileService) Remove(ctx context.Context, rawPath string) error {
	// 解析资源路径，获取域、uid 和规范化路径
	rp, err := s.paths.Resolve(ctx, rawPath)
	if err != nil {
		return err
	}

	// local 域不走远端接口
	if rp.Area == "local" {
		return fmt.Errorf("local resource area does not support remote deletion, please use private or public area")
	}

	// 拆分路径为父目录和文件名
	dirPath := path.Dir(rp.Path)
	fileName := path.Base(rp.Path)

	// 构造查询参数
	q := url.Values{
		"uid":  {strconv.FormatInt(rp.UID, 10)},
		"path": {dirPath},
	}

	// 发送 DELETE 请求，body 包含文件名数组
	names := []string{fileName}
	if err := s.client.DeleteJSON(ctx, "/api/openApi/diskFile/delete/v1", q, names, nil); err != nil {
		return fmt.Errorf("failed to delete %q: %w", rp.Path, err)
	}
	return nil
}

// Rename 重命名远端路径对应的文件或目录。
// rawPath 是源路径，newName 是新名称（不含路径）。
// 内部将路径拆分为父目录和旧名称，调用 /api/openApi/diskFile/rename/v1，
// 请求体包含 path、oldName、newName 字段。
func (s *DiskFileService) Rename(ctx context.Context, rawPath, newName string) error {
	// 解析资源路径，获取域、uid 和规范化路径
	rp, err := s.paths.Resolve(ctx, rawPath)
	if err != nil {
		return err
	}

	// local 域不走远端接口
	if rp.Area == "local" {
		return fmt.Errorf("local resource area does not support remote rename, please use private or public area")
	}

	// 拆分路径为父目录和旧名称
	dirPath := path.Dir(rp.Path)
	oldName := path.Base(rp.Path)

	// 构造请求体（uid 通过查询参数传递）
	body := map[string]string{
		"path":    dirPath,
		"oldName": oldName,
		"newName": newName,
	}

	// 构造查询参数（uid 通过查询参数传递）
	q := url.Values{
		"uid": {strconv.FormatInt(rp.UID, 10)},
	}

	// 发送 POST 请求到重命名接口，同时携带查询参数和 JSON body
	if err := s.client.PostQueryWithBody(ctx, "/api/openApi/diskFile/rename/v1", q, body, nil); err != nil {
		return fmt.Errorf("failed to rename %q to %q: %w", rp.Path, newName, err)
	}
	return nil
}

// Upload 将本地路径对应的文件或目录上传到远端资源路径。
//
// localPath 是本地文件或目录路径；remotePath 支持 [resourceArea:]<path> 格式，
// 域可为 private 或 public；local 域不支持，返回明确错误。
// 若 localPath 不存在，返回清晰的错误信息。
// 单文件上传：remotePath 指定远端目标文件路径（父目录 + 文件名）。
// 目录上传：remotePath 指定远端目标目录，递归创建子目录并上传所有文件。
// out 用于输出进度信息等用户可见内容，为 nil 时进度信息将被丢弃。
func (s *DiskFileService) Upload(ctx context.Context, localPath, remotePath string, out io.Writer) error {
	// 解析远端路径，获取域、uid 和规范化路径
	rp, err := s.paths.Resolve(ctx, remotePath)
	if err != nil {
		return err
	}

	// local 域不支持远端上传操作
	if rp.Area == "local" {
		return fmt.Errorf("local resource area does not support remote upload, please use private or public area")
	}

	// 确保 out 不为 nil，避免进度条写入 panic
	if out == nil {
		out = io.Discard
	}

	// 检查本地路径是否存在
	localInfo, err := os.Stat(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("local path %q does not exist", localPath)
		}
		return fmt.Errorf("failed to read local path %q: %w", localPath, err)
	}

	// 根据本地路径类型分别处理
	if localInfo.IsDir() {
		// 目录上传：遍历本地目录并递归编排 mkdir + upload
		return s.uploadDir(ctx, localPath, rp, out)
	}
	// 单文件上传
	return s.uploadSingleFile(ctx, localPath, rp, out)
}

// uploadSingleFile 将单个本地文件上传到远端路径。
// rp.Path 的基础名用作上传文件名，父目录作为 path 查询参数。
// 若文件大小可知，通过进度条展示上传进度。
func (s *DiskFileService) uploadSingleFile(ctx context.Context, localPath string, rp ResolvedPath, out io.Writer) error {
	// 打开本地文件
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open file %q: %w", localPath, err)
	}
	defer f.Close()

	// 从远端路径中提取文件名和父目录
	fileName := path.Base(rp.Path)
	dirPath := path.Dir(rp.Path)

	// 获取文件大小以初始化进度条（失败时以 -1 表示未知大小）
	var fileSize int64 = -1
	if fi, statErr := f.Stat(); statErr == nil {
		fileSize = fi.Size()
	}

	// 构造进度条，展示上传进度
	bar := progressbar.NewOptions64(
		fileSize,
		progressbar.OptionSetWriter(out),
		progressbar.OptionSetDescription(fileName),
		progressbar.OptionShowBytes(true),
	)

	// 构造上传接口查询参数
	q := url.Values{
		"uid":  {strconv.FormatInt(rp.UID, 10)},
		"path": {dirPath},
	}

	// 上传文件，同步更新进度条
	reader := io.TeeReader(f, bar)
	if err := s.client.UploadFile(ctx, "/api/openApi/diskFile/upload/v1", q, "file", fileName, reader, nil); err != nil {
		return fmt.Errorf("failed to upload file %q: %w", localPath, err)
	}
	_ = bar.Finish()
	return nil
}

// mkdir 调用后端接口在远端创建目录。
// rp.Path 的基础名为要创建的目录名，父目录作为 path 查询参数。
func (s *DiskFileService) mkdir(ctx context.Context, rp ResolvedPath) error {
	q := url.Values{
		"uid":  {strconv.FormatInt(rp.UID, 10)},
		"path": {path.Dir(rp.Path)},
		"name": {path.Base(rp.Path)},
	}
	if err := s.client.PostQuery(ctx, "/api/openApi/diskFile/mkdir/v1", q, nil); err != nil {
		return fmt.Errorf("failed to create remote directory %q: %w", rp.Path, err)
	}
	return nil
}

// uploadDir 递归将本地目录上传到远端目录。
// 使用 localfs.Walk 遍历本地目录，按条目顺序（目录先于内容）先创建远端子目录，
// 再上传各个文件，确保父目录在上传子文件前已存在。
func (s *DiskFileService) uploadDir(ctx context.Context, localPath string, rp ResolvedPath, out io.Writer) error {
	// 获取本地目录的所有条目（目录先于其内容出现）
	entries, err := localfs.Walk(localPath)
	if err != nil {
		return fmt.Errorf("failed to walk local directory %q: %w", localPath, err)
	}

	// 遍历条目，按顺序执行 mkdir（目录）或 upload（文件）
	for _, entry := range entries {
		// 将 OS 原生路径分隔符转为正斜杠，用于构造远端路径
		relRemote := filepath.ToSlash(entry.RelativePath)
		entryRp := ResolvedPath{
			Area: rp.Area,
			UID:  rp.UID,
			Path: path.Join(rp.Path, relRemote),
		}

		if entry.IsDir {
			// 先创建远端子目录
			if err := s.mkdir(ctx, entryRp); err != nil {
				return err
			}
		} else {
			// 上传文件到对应远端路径
			if err := s.uploadSingleFile(ctx, entry.AbsolutePath, entryRp, out); err != nil {
				return err
			}
		}
	}
	return nil
}
