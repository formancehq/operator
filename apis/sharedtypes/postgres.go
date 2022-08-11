//+kubebuilder:object:generate=true
package sharedtypes

import (
	"fmt"
)

type PostgresConfig struct {
	Database string `json:"database"`
	Port     int    `json:"port"`
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (c PostgresConfig) URI() string {
	return fmt.Sprintf(
		"postgresql://%s:%s@%s:%d/%s",
		c.Username,
		c.Password,
		c.Host,
		c.Port,
		c.Database,
	)
}

func (c PostgresConfig) URIWithoutDatabase() string {
	return fmt.Sprintf(
		"postgresql://%s:%s@%s:%d",
		c.Username,
		c.Password,
		c.Host,
		c.Port,
	)
}
