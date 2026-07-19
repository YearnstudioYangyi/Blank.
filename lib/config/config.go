package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type Plugin struct {
	Id     string   `json:"id"`
	Prefix string   `json:"prefix"`
	Group  []string `json:"group"`
}

// SearXNG 自部署搜索实例配置。
type SearXNG struct {
	BaseURL  string `json:"base_url"`  // 如 http://127.0.0.1:54911
	Limit    int    `json:"limit"`     // 最多返回条数，0 用库默认
	Language string `json:"language"`  // 可选，如 zh-CN
}

type AppConfig struct {
	Port      uint16   `json:"port"`
	AppId     string   `json:"appid"`
	AppSecret string   `json:"secret"`
	Plugins   []Plugin `json:"plugins"`
	ProxyAPI  string   `json:"proxy"`
	Uin       uint64   `json:"uin"`
	Uid       string   `json:"uid"`
	SearXNG   SearXNG  `json:"searxng"`
}

var (
	global AppConfig
	once   sync.Once
)

func InitConfig() AppConfig {
	once.Do(func() {
		file, err := os.ReadFile("./config.json")
		if err != nil {
			fmt.Println("请正确配置config.json")
			os.Exit(1)
		}

		var appConfig AppConfig
		err = json.Unmarshal(file, &appConfig)
		if err != nil {
			fmt.Println("请正确配置config.json")
			os.Exit(1)
		}
		if appConfig.SearXNG.Limit <= 0 {
			appConfig.SearXNG.Limit = 10
		}
		global = appConfig
	})
	return global
}

// Get 返回已加载的配置（须先调用 InitConfig）。
func Get() AppConfig {
	return global
}
