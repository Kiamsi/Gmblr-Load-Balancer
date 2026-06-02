//this file's purpose is to parse the lb.yaml's plain text into executable Go code

package config

type Config struct {
	Listen     string     `yaml:"listen"`
	Backends   []Backend  `yaml:"backends"`
	Health     Health     `yaml:"health"`
	Stickiness Stickiness `yaml:"stickiness"`
	Admin      Admin      `yaml:"admin"`
}

type Backend struct {
	Addr string `yaml:"addr"`
}

type Health struct {
	Path          string `yaml:"path"`
	IntervalS     int    `yaml:"interval_s"`
	FailThreshold int    `yaml:"fail_threshold"`
	PassThreshold int    `yaml:"pass_threshold"`
}

type Stickiness struct {
	RoomIDRegex string `yaml:"room_id_regex"`
}

type Admin struct {
	Listen       string `yaml:"listen"`
	AuthTokenEnv string `yaml:"auth_token_env"`
}
