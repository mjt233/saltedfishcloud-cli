// Package config 负责从配置文件、环境变量和命令行标志中加载并校验运行时配置。
// 优先级固定为：命令行标志 > 环境变量 > ~/.config/sfc-cli/config.json。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Options 封装由命令行标志层传入的配置覆盖值。非空字段将以最高优先级覆盖环境变量和文件中的同名配置。
type Options struct {
	// ServiceURL 为命令行 --service-url 标志的值；留空则不覆盖低优先级来源。
	ServiceURL string
	// APITicket 为命令行 --api-ticket 标志的值；留空则不覆盖低优先级来源。
	APITicket string
}

// Config 是经过合并与校验后的运行时配置，供各子命令直接使用。
type Config struct {
	// ServiceURL 是咸鱼云服务的基础 URL。
	ServiceURL string
	// APITicket 是用于鉴权的永久有效凭据。
	APITicket string
}

// Load 按照"标志 > 环境变量 > 配置文件"的优先级合并配置，
// 并在必填字段缺失时一次性返回所有缺失字段的错误信息。
// 配置文件不存在时静默忽略；文件存在但解析失败时返回错误。
func Load(opts Options) (Config, error) {
	// 第一阶段：分别初始化文件层和环境变量层的 Viper 实例。
	fileViper, err := newFileViper()
	if err != nil {
		return Config{}, fmt.Errorf("failed to read config file: %w", err)
	}
	envViper := newEnvViper()

	// 第二阶段：按优先级合并。文件值先进入基础配置；
	// 仅当环境变量非空时，才用 Viper 读取到的环境值覆盖文件值。
	cfg := Config{
		ServiceURL: fileViper.GetString("serviceUrl"),
		APITicket:  fileViper.GetString("apiTicket"),
	}
	if val, ok := os.LookupEnv("SFC_SERVICE_URL"); ok && val != "" {
		cfg.ServiceURL = envViper.GetString("serviceUrl")
	}
	if val, ok := os.LookupEnv("SFC_API_TICKET"); ok && val != "" {
		cfg.APITicket = envViper.GetString("apiTicket")
	}
	if opts.ServiceURL != "" {
		cfg.ServiceURL = opts.ServiceURL
	}
	if opts.APITicket != "" {
		cfg.APITicket = opts.APITicket
	}

	// 第三阶段：校验必填字段，收集所有缺失项后一次性报错
	var missing []string
	if cfg.ServiceURL == "" {
		missing = append(missing, "serviceUrl")
	}
	if cfg.APITicket == "" {
		missing = append(missing, "apiTicket")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}

// newFileViper 构造并返回一个仅读取配置文件的 Viper 实例。
// 读取 ~/.config/sfc-cli/config.json；文件不存在时静默忽略，
// 文件存在但无法解析时返回错误。
func newFileViper() (*viper.Viper, error) {
	v := viper.New()

	home, err := os.UserHomeDir()
	if err != nil {
		// 无法获取家目录，跳过文件加载
		return v, nil
	}

	// 构造配置文件绝对路径并尝试加载
	cfgPath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	v.SetConfigFile(cfgPath)
	if err := v.ReadInConfig(); err != nil {
		// 文件不存在属于正常情况，静默忽略
		if errors.Is(err, os.ErrNotExist) {
			return v, nil
		}
		// 其他错误（解析失败、权限不足等）需要向上传递
		return nil, err
	}

	return v, nil
}

// newEnvViper 构造并返回一个用于读取环境变量的 Viper 实例。
// 仅负责将 SFC_* 环境变量映射到内部配置键，不直接处理空值覆盖逻辑。
func newEnvViper() *viper.Viper {
	v := viper.New()
	v.SetEnvPrefix("SFC")
	v.SetEnvKeyReplacer(strings.NewReplacer(
		"SERVICEURL", "SERVICE_URL",
		"APITICKET", "API_TICKET",
	))
	v.AutomaticEnv()
	_ = v.BindEnv("serviceUrl")
	_ = v.BindEnv("apiTicket")
	return v
}
