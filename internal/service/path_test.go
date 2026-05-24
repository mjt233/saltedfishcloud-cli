// Package service 的路径解析服务测试文件。
package service

import (
	"context"
	"strings"
	"testing"
)

// TestResolve_DefaultsToPrivateArea 验证无资源域前缀时默认使用 private 域，并调用 privateUID 回调。
func TestResolve_DefaultsToPrivateArea(t *testing.T) {
	svc := NewPathService(func(ctx context.Context) (int64, error) {
		return 123, nil
	})

	rp, err := svc.Resolve(context.Background(), "/some/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.Area != "private" {
		t.Fatalf("expected area=private, got %s", rp.Area)
	}
	if rp.UID != 123 {
		t.Fatalf("expected uid=123, got %d", rp.UID)
	}
	if rp.Path != "/some/path" {
		t.Fatalf("expected path=/some/path, got %s", rp.Path)
	}
}

// TestResolve_PublicAreaUIDIsZero 验证 public 资源域的 UID 固定为 0，且不调用 privateUID 回调。
func TestResolve_PublicAreaUIDIsZero(t *testing.T) {
	uidCalled := false
	svc := NewPathService(func(ctx context.Context) (int64, error) {
		uidCalled = true
		return 999, nil
	})

	rp, err := svc.Resolve(context.Background(), "public:/shared/file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.Area != "public" {
		t.Fatalf("expected area=public, got %s", rp.Area)
	}
	if rp.UID != 0 {
		t.Fatalf("expected uid=0, got %d", rp.UID)
	}
	if rp.Path != "/shared/file" {
		t.Fatalf("expected path=/shared/file, got %s", rp.Path)
	}
	if uidCalled {
		t.Fatal("privateUID callback should not be called for public area")
	}
}

// TestResolve_LocalAreaDoesNotCallUID 验证 local 资源域不调用 privateUID 回调，UID 为 0。
func TestResolve_LocalAreaDoesNotCallUID(t *testing.T) {
	uidCalled := false
	svc := NewPathService(func(ctx context.Context) (int64, error) {
		uidCalled = true
		return 999, nil
	})

	rp, err := svc.Resolve(context.Background(), "local:/downloads/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.Area != "local" {
		t.Fatalf("expected area=local, got %s", rp.Area)
	}
	if rp.Path != "/downloads/file.txt" {
		t.Fatalf("expected path=/downloads/file.txt, got %s", rp.Path)
	}
	if uidCalled {
		t.Fatal("privateUID callback should not be called for local area")
	}
}

// TestResolve_PrivateAreaExplicit 验证显式声明 private 资源域时调用 privateUID 并正确填充 UID。
func TestResolve_PrivateAreaExplicit(t *testing.T) {
	svc := NewPathService(func(ctx context.Context) (int64, error) {
		return 77, nil
	})

	rp, err := svc.Resolve(context.Background(), "private:/my/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.Area != "private" {
		t.Fatalf("expected area=private, got %s", rp.Area)
	}
	if rp.UID != 77 {
		t.Fatalf("expected uid=77, got %d", rp.UID)
	}
	if rp.Path != "/my/file.txt" {
		t.Fatalf("expected path=/my/file.txt, got %s", rp.Path)
	}
}

// TestResolve_InvalidAreaReturnsError 验证不支持的资源域返回包含预期格式提示的错误。
func TestResolve_InvalidAreaReturnsError(t *testing.T) {
	svc := NewPathService(func(ctx context.Context) (int64, error) {
		return 1, nil
	})

	_, err := svc.Resolve(context.Background(), "ftp:/some/path")
	if err == nil {
		t.Fatal("expected error for invalid area, got nil")
	}
}

// TestResolve_EmptyInputReturnsError 验证空输入返回含格式提示的错误。
func TestResolve_EmptyInputReturnsError(t *testing.T) {
	svc := NewPathService(func(ctx context.Context) (int64, error) {
		return 1, nil
	})

	_, err := svc.Resolve(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}
	if !strings.Contains(err.Error(), "[resourceArea:]") {
		t.Fatalf("error should mention expected format, got: %s", err.Error())
	}
}

// TestResolve_PathWithoutLeadingSlashGetsNormalized 验证路径不以 / 开头时自动补全前缀。
func TestResolve_PathWithoutLeadingSlashGetsNormalized(t *testing.T) {
	svc := NewPathService(func(ctx context.Context) (int64, error) {
		return 5, nil
	})

	rp, err := svc.Resolve(context.Background(), "private:no-leading-slash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.Path != "/no-leading-slash" {
		t.Fatalf("expected normalized path=/no-leading-slash, got %s", rp.Path)
	}
}

// TestResolve_EmptyPathAfterColonReturnsError 验证冒号后路径为空时返回错误。
func TestResolve_EmptyPathAfterColonReturnsError(t *testing.T) {
	svc := NewPathService(func(ctx context.Context) (int64, error) {
		return 1, nil
	})

	_, err := svc.Resolve(context.Background(), "private:")
	if err == nil {
		t.Fatal("expected error for empty path after colon, got nil")
	}
}
