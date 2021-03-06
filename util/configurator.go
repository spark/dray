package util

import (
	"reflect"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/kelseyhightower/envconfig"
)

type ConfigSpecification struct {
	DockerHost string `envconfig:"DOCKER_HOST" default:"unix:///var/run/docker.sock"`
	LogLevel   string `envconfig:"LOG_LEVEL" default:"info"`
	RedisPort  string `envconfig:"REDIS_PORT"`
	JobsKey    string `envconfig:"DRAY_JOBS_KEY" default:"jobs"`
	KeyTTL     int    `envconfig:"DRAY_KEY_TTL"`
	RemoveDone bool   `envconfig:"DRAY_REMOVE_DONE"`
}

type configurator struct {
	config ConfigSpecification
}

var instance *configurator
var once sync.Once

func GetConfig() *ConfigSpecification {
	once.Do(func() {
		var c ConfigSpecification
		err := envconfig.Process("dray", &c)
		if err != nil {
			log.Fatal(err.Error())
		}

		instance = &configurator{config: c}
	})
	return &instance.config
}

func GetDefaultValue(name string) interface{} {
	c := GetConfig()
	t := reflect.TypeOf(c)
	field, found := t.FieldByName(name)
	if !found {
		return nil
	}
	return field.Tag.Get("default")
}
