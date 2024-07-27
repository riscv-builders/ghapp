package coordinator

type Config struct {
	DBURL  string `config:"DB_URL"`
	DBType string `config:"DB_TYPE"`

	ClientID    string `config:"GH_CLIENT_ID"`
	PrivateFile string `config:"GH_PRIVATE_KEY_PATH"`
}
