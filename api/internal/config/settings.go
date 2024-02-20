package config

type Settings struct {
	ClientID                  string `yaml:"CLIENT_ID"`
	Domain                    string `yaml:"DOMAIN"`
	Scope                     string `yaml:"SCOPE"`
	ResponseType              string `yaml:"RESPONSE_TYPE"`
	GrantType                 string `yaml:"GRANT_TYPE"`
	AuthURL                   string `yaml:"AUTH_URL"`
	SubmitChallengeURL        string `yaml:"SUBMIT_CHALLENGE_URL"`
	IdentityAPIURL            string `yaml:"IDENTITY_API_URL"`
	TokenExchangeJWTKeySetURL string `yaml:"TOKEN_EXCHANGE_JWK_KEY_SET_URL"`
	TokenExchangeAPIURL       string `yaml:"TOKEN_EXCHANGE_API_URL"`
	DeviceDataAPIBaseURL      string `yaml:"DEVICE_DATA_API_BASE_URL"`
	Port                      string `yaml:"PORT"`
	LogLevel                  string `yaml:"LOG_LEVEL"`
}
