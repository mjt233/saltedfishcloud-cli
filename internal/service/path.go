// Package service 负责路径解析、远程与本地操作编排，以及用户资料等业务能力。
// 当前文件提供资源路径的解析与规范化能力。
package service

import (
	"context"
	"fmt"
	"strings"
)

// ResolvedPath 是解析后的资源路径结构，包含资源域、用户 ID 和规范化路径。
type ResolvedPath struct {
	// Area 是资源域，取值为 "local"、"private" 或 "public"。
	Area string

	// UID 是资源所属的用户 ID。
	// public 域固定为 0；local 域为 0（不使用）；private 域通过回调获取。
	UID int64

	// Path 是规范化后的路径，始终以 / 开头。
	Path string
}

// supportedAreas 列出所有合法的资源域名称。
var supportedAreas = map[string]bool{
	"local":   true,
	"private": true,
	"public":  true,
}

// PathService 负责将用户输入的原始路径字符串解析为 ResolvedPath。
type PathService struct {
	// privateUID 是用于获取当前用户私有 UID 的回调函数。
	privateUID func(context.Context) (int64, error)
}

// NewPathService 构造一个 PathService 实例。
// privateUID 是一个回调函数，仅在解析 private 域路径时调用。
func NewPathService(privateUID func(context.Context) (int64, error)) *PathService {
	return &PathService{privateUID: privateUID}
}

// Resolve 将原始路径字符串解析为 ResolvedPath。
//
// 支持的输入格式为 [resourceArea:]<path>：
//   - 无前缀时默认资源域为 private；
//   - private 域通过 privateUID 回调获取 UID；
//   - public 域 UID 固定为 0，不调用回调；
//   - local 域不需要 UID，不调用回调；
//   - 路径不以 / 开头时自动补全。
//
// 返回错误的情形：输入为空、资源域不合法、冒号后路径为空。
func (s *PathService) Resolve(ctx context.Context, raw string) (ResolvedPath, error) {
	// 空输入直接报错，提示期望格式
	if raw == "" {
		return ResolvedPath{}, fmt.Errorf("路径不能为空，期望格式为 [resourceArea:]<path>")
	}

	var area, rawPath string

	// 按第一个冒号分割资源域与路径
	if idx := strings.Index(raw, ":"); idx >= 0 {
		area = raw[:idx]
		rawPath = raw[idx+1:]
	} else {
		// 无冒号时整体视为路径，资源域默认 private
		area = "private"
		rawPath = raw
	}

	// 校验资源域合法性
	if !supportedAreas[area] {
		return ResolvedPath{}, fmt.Errorf("不支持的资源域 %q，合法值为 local、private、public（格式：[resourceArea:]<path>）", area)
	}

	// 冒号后路径为空时报错
	if rawPath == "" {
		return ResolvedPath{}, fmt.Errorf("路径不能为空，期望格式为 [resourceArea:]<path>")
	}

	// 规范化路径：补全前缀 /
	if rawPath[0] != '/' {
		rawPath = "/" + rawPath
	}

	// 根据资源域填充 UID
	var uid int64
	switch area {
	case "public", "local":
		// public 与 local 域都不使用远端 UID，固定为 0。
		uid = 0
	case "private":
		// private 域通过回调获取当前用户 UID
		var err error
		uid, err = s.privateUID(ctx)
		if err != nil {
			return ResolvedPath{}, fmt.Errorf("获取私有用户 UID 失败: %w", err)
		}
	}

	return ResolvedPath{
		Area: area,
		UID:  uid,
		Path: rawPath,
	}, nil
}
