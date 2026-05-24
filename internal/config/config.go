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
	// 第一阶段：通过 Viper 读取配置文件中的基础值
	fileServiceURL, fileAPITicket := loadFromFile()

	// 第二阶段：读取环境变量（前缀 SFC_），空值不覆盖低优先级来源
	envServiceURL := os.Getenv("SFC_SERVICE_URL")
	envAPITicket := os.Getenv("SFC_API_TICKET")

	// 第三阶段：按优先级合并，空值跳过以保证低优先级来源的有效值不被清空
	cfg := Config{
		ServiceURL: fileServiceURL,
		APITicket:  fileAPITicket,
	}
	if envServiceURL != "" {
		cfg.ServiceURL = envServiceURL
	}
	if envAPITicket != "" {
		cfg.APITicket = envAPITicket
	}
	if opts.ServiceURL != "" {
		cfg.ServiceURL = opts.ServiceURL
	}
	if opts.APITicket != "" {
		cfg.APITicket = opts.APITicket
	}

	// 第四阶段：校验必填字段，收集所有缺失项后一次性报错
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

// loadFromFile 使用 Viper 从 ~/.config/sfc-cli/config.json 中读取配置，
// 文件不存在或读取失败时静默返回空字符串。
func loadFromFile() (serviceURL, apiTicket string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}

	// 构造配置文件的绝对路径
	cfgPath := filepath.Join(home, ".config", "sfc-cli", "config.json")

	v := viper.New()
	v.SetConfigFile(cfgPath)

	// 忽略文件不存在等非致命错误
	if err := v.ReadInConfig(); err != nil {
		return "", ""
	}

	return v.GetString("serviceUrl"), v.GetString("apiTicket")
}
