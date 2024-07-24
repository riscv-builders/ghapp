package coordinator

type Config struct {
	ListenAddr string `config:"LISTEN_ADDR"`

	DBURL  string `config:"DB_URL"`
	DBType string `config:"DB_TYPE"`
}
