/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	. "github.com/numary/operator/apis/sharedtypes"
	. "github.com/numary/operator/internal/collectionutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type MongoDBConfig struct {
	// +optional
	Host string `json:"host,omitempty"`
	// +optional
	HostFrom *ConfigSource `json:"hostFrom,omitempty"`
	// +optional
	Port uint16 `json:"port,omitempty"`
	// +optional
	PortFrom *ConfigSource `json:"portFrom,omitempty"`
	// +optional
	Username string `json:"username,omitempty"`
	// +optional
	UsernameFrom *ConfigSource `json:"usernameFrom,omitempty"`
	// +optional
	Password string `json:"password,omitempty"`
	// +optional
	PasswordFrom *ConfigSource `json:"passwordFrom,omitempty"`
	// +optional
	UseSrv bool `json:"useSrv,omitempty"`
	// +required
	Database string `json:"database"`
}

func (cfg *MongoDBConfig) Env(prefix string) []corev1.EnvVar {

	env := make([]corev1.EnvVar, 0)
	env = append(env, SelectRequiredConfigValueOrReference("MONGODB_HOST", prefix,
		cfg.Host, cfg.HostFrom))

	if cfg.Username != "" || cfg.UsernameFrom != nil {
		env = append(env,
			SelectRequiredConfigValueOrReference("MONGODB_USERNAME", prefix,
				cfg.Username, cfg.UsernameFrom),
			SelectRequiredConfigValueOrReference("MONGODB_PASSWORD", prefix,
				cfg.Password, cfg.PasswordFrom),
			Env("MONGODB_CREDENTIALS_PART", ComputeEnvVar(prefix, "%s:%s@",
				"MONGODB_USERNAME",
				"MONGODB_PASSWORD")),
		)
	}

	if cfg.UseSrv {
		env = append(env,
			Env("MONGODB_URI", ComputeEnvVar(prefix, "mongodb+srv://%s%s",
				"MONGODB_CREDENTIALS_PART",
				"MONGODB_HOST",
			)),
		)
	} else {
		env = append(env,
			SelectRequiredConfigValueOrReference("MONGODB_PORT", prefix,
				cfg.Port, cfg.PortFrom),
			Env("MONGODB_URI", ComputeEnvVar(prefix, "mongodb://%s%s:%s",
				"MONGODB_CREDENTIALS_PART",
				"MONGODB_HOST",
				"MONGODB_PORT",
			)),
		)
	}
	env = append(env,
		Env("MONGODB_DATABASE", cfg.Database),
	)

	return env
}

func (cfg *MongoDBConfig) Validate() field.ErrorList {
	return MergeAll(
		ValidateRequiredConfigValueOrReference("host", cfg.Host, cfg.HostFrom),
		ValidateRequiredConfigValueOrReference("port", cfg.Port, cfg.PortFrom),
		ValidateRequiredConfigValueOrReferenceOnly("username", cfg.Username, cfg.UsernameFrom),
		ValidateRequiredConfigValueOrReferenceOnly("password", cfg.Password, cfg.PasswordFrom),
	)
}
