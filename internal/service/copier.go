// Package service 负责路径解析、远程与本地操作编排，以及用户资料等业务能力。
// 当前文件提供跨资源域的复制与移动能力。
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"

	"github.com/mjt233/saltedfishcloud-cli/internal/client"
)

// CopierService 提供跨资源域的复制与移动能力。
type CopierService struct {
	// client 是底层 HTTP 客户端，用于调用咸鱼云开放接口。
	client *client.APIClient
	// disk 提供远端文件的列出、上传、下载和删除能力。
	disk *DiskFileService
	// paths 负责将用户输入的原始路径解析为带域和 uid 的结构。
	paths *PathService
}

// NewCopierService 构造一个 CopierService 实例。
// cli、disk 和 paths 均不应为 nil。
func NewCopierService(cli *client.APIClient, disk *DiskFileService, paths *PathService) *CopierService {
	return &CopierService{client: cli, disk: disk, paths: paths}
}

// copyMoveRequest 是远端复制/移动接口的请求体结构。
type copyMoveRequest struct {
	SourceUID  int64    `json:"sourceUid"`
	TargetUID  int64    `json:"targetUid"`
	SourcePath string   `json:"sourcePath"`
	TargetPath string   `json:"targetPath"`
	Files      []string `json:"files"`
}

// Copy 将 source 对应的文件或目录复制到 target。
// 支持 remote->remote（同域或跨域）的复制。
// 远端到远端调用 /api/openApi/diskFile/copy/v1，
// 请求体包含 sourceUid、targetUid、sourcePath、targetPath、files（文件名数组）。
func (s *CopierService) Copy(ctx context.Context, source, target string) error {
	// 解析源路径和目标路径
	srcRP, err := s.paths.Resolve(ctx, source)
	if err != nil {
		return err
	}

	// 源路径不能是 local
	if srcRP.Area == "local" {
		return fmt.Errorf("local 资源域不支持作为复制操作的源，请使用 private 或 public 域")
	}

	tgtRP, err := s.paths.Resolve(ctx, target)
	if err != nil {
		return err
	}

	// 目标路径不能是 local
	if tgtRP.Area == "local" {
		return fmt.Errorf("local 资源域不支持作为复制操作的目标，请使用 private 或 public 域")
	}

	// 获取要复制的文件名列表
	files, err := s.resolveFileNames(ctx, srcRP)
	if err != nil {
		return err
	}

	// 构造远端复制请求体
	req := copyMoveRequest{
		SourceUID:  srcRP.UID,
		TargetUID:  tgtRP.UID,
		SourcePath: path.Dir(srcRP.Path),
		TargetPath: tgtRP.Path,
		Files:      files,
	}

	// 发送 POST 请求到复制接口
	if err := s.client.PostJSON(ctx, "/api/openApi/diskFile/copy/v1", req, nil); err != nil {
		return fmt.Errorf("复制 %q 到 %q 失败: %w", source, target, err)
	}
	return nil
}

// Move 将 source 移动到 target。
// 远端到远端：调用 /api/openApi/diskFile/move/v1，参数结构同 Copy。
// local 到远端：先 Upload 再删除本地源文件。
// 远端到 local：先 Download 再 Remove 远端源。
func (s *CopierService) Move(ctx context.Context, source, target string) error {
	// 解析源路径和目标路径
	srcRP, err := s.paths.Resolve(ctx, source)
	if err != nil {
		return err
	}
	tgtRP, err := s.paths.Resolve(ctx, target)
	if err != nil {
		return err
	}

	// local -> remote：先上传后删除本地源
	// 使用 paths.ParseLocalPath 提取原始本地路径，避免 Resolve 加上的 / 前缀破坏 Windows 路径
	if srcRP.Area == "local" && tgtRP.Area != "local" {
		localPath := s.paths.ParseLocalPath(source)
		return s.moveLocalToRemote(ctx, localPath, target)
	}

	// remote -> local：先下载后删除远端源
	// 同理使用原始本地路径
	if srcRP.Area != "local" && tgtRP.Area == "local" {
		localPath := s.paths.ParseLocalPath(target)
		return s.moveRemoteToLocal(ctx, srcRP, localPath)
	}

	// remote -> remote：直接调用远端移动接口
	if srcRP.Area != "local" && tgtRP.Area != "local" {
		return s.moveRemoteToRemote(ctx, srcRP, tgtRP)
	}

	// local -> local：不支持
	return fmt.Errorf("local 到 local 的移动暂不支持")
}

// moveLocalToRemote 将本地文件上传到远端，成功后删除本地源文件。
func (s *CopierService) moveLocalToRemote(ctx context.Context, localPath, remoteTarget string) error {
	// 上传本地文件到远端
	if err := s.disk.Upload(ctx, localPath, remoteTarget, nil); err != nil {
		return fmt.Errorf("上传 %q 到 %q 失败: %w", localPath, remoteTarget, err)
	}

	// 上传成功后删除本地源（目录使用 RemoveAll，文件使用 Remove）
	if err := os.RemoveAll(localPath); err != nil {
		return fmt.Errorf("删除本地源 %q 失败: %w", localPath, err)
	}
	return nil
}

// moveRemoteToLocal 将远端文件下载到本地，成功后删除远端源文件。
func (s *CopierService) moveRemoteToLocal(ctx context.Context, srcRP ResolvedPath, localPath string) error {
	// 下载远端文件到本地
	if err := s.disk.Download(ctx, srcRP.Area+":"+srcRP.Path, localPath, nil); err != nil {
		return fmt.Errorf("下载 %q 到 %q 失败: %w", srcRP.Path, localPath, err)
	}

	// 下载成功后删除远端源文件
	if err := s.disk.Remove(ctx, srcRP.Area+":"+srcRP.Path); err != nil {
		return fmt.Errorf("删除远端源文件 %q 失败: %w", srcRP.Path, err)
	}
	return nil
}

// moveRemoteToRemote 将远端文件从源路径移动到目标路径。
// 调用 /api/openApi/diskFile/move/v1，参数结构同复制。
func (s *CopierService) moveRemoteToRemote(ctx context.Context, srcRP, tgtRP ResolvedPath) error {
	// 获取要移动的文件名列表
	files, err := s.resolveFileNames(ctx, srcRP)
	if err != nil {
		return err
	}

	// 构造远端移动请求体
	req := copyMoveRequest{
		SourceUID:  srcRP.UID,
		TargetUID:  tgtRP.UID,
		SourcePath: path.Dir(srcRP.Path),
		TargetPath: tgtRP.Path,
		Files:      files,
	}

	// 发送 POST 请求到移动接口
	if err := s.client.PostJSON(ctx, "/api/openApi/diskFile/move/v1", req, nil); err != nil {
		return fmt.Errorf("移动 %q 到 %q 失败: %w", srcRP.Path, tgtRP.Path, err)
	}
	return nil
}

// resolveFileNames 根据源路径获取要操作的文件名列表。
// 若源路径是目录，列出其所有条目并返回文件名；若是文件，返回单元素数组。
func (s *CopierService) resolveFileNames(ctx context.Context, rp ResolvedPath) ([]string, error) {
	// 尝试列出路径内容；若成功则视为目录
	entries, listErr := s.disk.listByResolved(ctx, rp)
	if listErr == nil {
		// 目录：收集所有条目名称
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name)
		}
		return names, nil
	}

	// 检查是否为"路径非目录"业务错误
	var bizErr *client.BusinessError
	if errors.As(listErr, &bizErr) && bizErr.BusinessCode == client.BusinessCodeNotADirectory {
		// 单文件：返回基础名称
		return []string{path.Base(rp.Path)}, nil
	}

	// 其他错误直接返回
	return nil, fmt.Errorf("获取源路径 %q 的文件列表失败: %w", rp.Path, listErr)
}
