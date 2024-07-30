package coordinator

import "time"

type Config struct {
	DBURL  string `config:"DB_URL"`
	DBType string `config:"DB_TYPE"`

	ClientID    string `config:"GH_CLIENT_ID"`
	PrivateFile string `config:"GH_PRIVATE_KEY_PATH"`

	JobExecTimeLimit time.Duration `config:"JOB_EXEC_TIME_LIMIT"`
}
