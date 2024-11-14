package config

type AccountPlain struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type Account struct {
	Plain *AccountPlain `yaml:"plain"`
}

type JiraInstance struct {
	BaseURL string  `yaml:"baseURL"`
	Account Account `yaml:"account"`
}

type Atlassian struct {
	Account Account `yaml:"account"`
}

type Playwright struct {
	Endpoint string `yaml:"endpoint"`
	Headful  bool   `yaml:"headful"`
}

type Config struct {
	Instances  []JiraInstance `yaml:"instances"`
	Atlassian  Atlassian      `yaml:"atlassian"`
	Playwright Playwright     `yaml:"playwright"`
}
