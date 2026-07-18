package ai

import "gopkg.in/yaml.v3"

// yamlUnmarshal 是 yaml.Unmarshal 的薄封装，方便插件内部统一调用并避免在主文件中重复 import 路径。
func yamlUnmarshal(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}
