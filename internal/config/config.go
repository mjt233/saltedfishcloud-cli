// Package config 负责从配置文件、环境变量和命令行标志中加载并校验运行时配置。
// 优先级固定为：命令行标志 > 环境变量 > ~/.config/sfc-cli/config.json。
package config

import (
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
func Load(opts Options) (Config, error) {
	// 第一阶段：初始化 Viper 实例，绑定配置文件与环境变量
	v := newViper()

	// 第二阶段：按优先级合并。空值不覆盖低优先级来源，
	// 通过 os.LookupEnv 检测环境变量是否被显式设置且非空。
	cfg := Config{
		ServiceURL: v.GetString("serviceUrl"),
		APITicket:  v.GetString("apiTicket"),
	}
	if val, ok := os.LookupEnv("SFC_SERVICE_URL"); ok && val != "" {
		cfg.ServiceURL = val
	}
	if val, ok := os.LookupEnv("SFC_API_TICKET"); ok && val != "" {
		cfg.APITicket = val
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

// newViper 构造并返回一个已配置好文件路径与环境变量绑定的 Viper 实例。
// 读取 ~/.config/sfc-cli/config.json；文件不存在时静默忽略。
func newViper() *viper.Viper {
	v := viper.New()
	v.SetEnvPrefix("SFC")
	v.AutomaticEnv()
	// 将 Viper 键名中的驼峰分隔映射到下划线分隔的环境变量
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// 绑定具体环境变量，确保大小写不敏感的键名能正确解析
	_ = v.BindEnv("serviceUrl", "SFC_SERVICE_URL")
	_ = v.BindEnv("apiTicket", "SFC_API_TICKET")

	home, err := os.UserHomeDir()
	if err != nil {
		return v
	}

	// 构造配置文件绝对路径并尝试加载，忽略文件不存在等非致命错误
	cfgPath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	v.SetConfigFile(cfgPath)
	_ = v.ReadInConfig()

	return v
}
