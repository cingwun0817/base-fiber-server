package common

import (
	"log"
	"os"

	"github.com/spf13/viper"
)

// loadConfig loads the config file.
func LoadConfig() {
	viper.SetConfigName(".env")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(os.Getenv("CONFIG_PATH"))

	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("[config] viper.ReadInConfig, error %v\n", err)
		os.Exit(1)
	}
}
