// Package service 负责路径解析、远程与本地操作编排，以及用户资料等业务能力。
// 当前文件提供用户 UID 的延迟获取与缓存能力。
package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/mjt233/saltedfishcloud-cli/internal/client"
)

// UserService 封装用户资料的获取逻辑，内部缓存私有 UID 以避免重复请求。
type UserService struct {
	// client 是底层 HTTP 客户端，用于调用咸鱼云开放接口。
	client *client.APIClient

	// once 保证 PrivateUID 只发起一次网络请求。
	// 由于 sync.Once 只会执行第一次回调，首次返回的成功结果或错误都会被缓存到该服务实例生命周期结束；
	// 后续调用不会重试，因此首次错误也会一直沿用。
	once sync.Once

	// cachedUID 是首次成功获取后缓存的私有用户 ID。
	cachedUID int64

	// cacheErr 是首次请求失败时缓存的错误。
	cacheErr error
}

// profileData 是 /api/openApi/user/profile/v1 响应中 data 字段的结构。
type profileData struct {
	// ID 是用户的唯一标识符，接口以字符串形式返回。
	ID string `json:"id"`
}

// NewUserService 构造一个 UserService 实例。
// cli 是已初始化的 APIClient，不应为 nil。
func NewUserService(cli *client.APIClient) *UserService {
	return &UserService{client: cli}
}

// PrivateUID 返回当前认证用户的私有 UID。
// 首次调用时向 /api/openApi/user/profile/v1 发起请求并缓存结果；
// 后续调用直接返回缓存值，不再产生网络请求。
func (s *UserService) PrivateUID(ctx context.Context) (int64, error) {
	// 利用 sync.Once 保证只请求一次；首次失败后也不会再重试。
	s.once.Do(func() {
		var profile profileData
		// 调用用户资料接口获取 id 字段
		err := s.client.GetJSON(ctx, "/api/openApi/user/profile/v1", nil, &profile)
		if err != nil {
			s.cacheErr = fmt.Errorf("failed to get user profile: %w", err)
			return
		}
		// 接口返回的 id 为字符串，需转换为 int64
		uid, err := strconv.ParseInt(profile.ID, 10, 64)
		if err != nil {
			s.cacheErr = fmt.Errorf("failed to parse user id %q: %w", profile.ID, err)
			return
		}
		s.cachedUID = uid
	})
	return s.cachedUID, s.cacheErr
}
