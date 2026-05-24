// main_test.go 验证 main 包入口的错误处理行为。
package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// TestRun_PrintsErrorToStderrOnFailure 验证 run 在执行函数返回错误时将错误信息写入 stderr。
func TestRun_PrintsErrorToStderrOnFailure(t *testing.T) {
	var buf bytes.Buffer
	testErr := errors.New("something went wrong")

	// 传入始终返回错误的执行函数，触发错误输出路径
	code := run(&buf, func() error { return testErr })

	if code != 1 {
		t.Errorf("退出码 = %d，期望 1", code)
	}
	if !strings.Contains(buf.String(), "something went wrong") {
		t.Errorf("stderr = %q，期望包含 %q", buf.String(), "something went wrong")
	}
}

// TestRun_ReturnsZeroOnSuccess 验证 run 在执行函数成功时返回 0 且不向 stderr 写入任何内容。
func TestRun_ReturnsZeroOnSuccess(t *testing.T) {
	var buf bytes.Buffer

	// 传入始终成功的执行函数
	code := run(&buf, func() error { return nil })

	if code != 0 {
		t.Errorf("退出码 = %d，期望 0", code)
	}
	if buf.Len() != 0 {
		t.Errorf("stderr = %q，期望为空", buf.String())
	}
}
