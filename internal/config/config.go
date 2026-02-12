package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Storage   StorageConfig   `mapstructure:"storage"`
	Logging   LoggingConfig   `mapstructure:"logging"`
	Pricing   PricingConfig   `mapstructure:"pricing"`
	Providers ProvidersConfig `mapstructure:"providers"`
	S3        S3Config        `mapstructure:"s3"`
	Metadata  MetadataConfig  `mapstructure:"metadata"`
	Secrets   SecretsConfig   `mapstructure:"secrets"`
}

type SecretsConfig struct {
	EncryptionKey string `mapstructure:"encryption_key"` // AES-256 key, hex-encoded (64 chars)
}

type MetadataConfig struct {
	HLLFlushInterval   time.Duration `mapstructure:"hll_flush_interval"`
	ActiveKeysCacheTTL time.Duration `mapstructure:"active_keys_cache_ttl"`
}

type ServerConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type StorageConfig struct {
	Driver   string         `mapstructure:"driver"`
	Postgres PostgresConfig `mapstructure:"postgres"`
}

type PostgresConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database"`
	SSLMode  string `mapstructure:"sslmode"`
	MaxConns int    `mapstructure:"max_conns"`
}

func (p *PostgresConfig) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		p.User, p.Password, p.Host, p.Port, p.Database, p.SSLMode)
}

type LoggingConfig struct {
	StoreRequestBody  bool   `mapstructure:"store_request_body"`
	StoreResponseBody bool   `mapstructure:"store_response_body"`
	MaxBodySize       int    `mapstructure:"max_body_size"`
	BodyStorage       string `mapstructure:"body_storage"` // "none", "postgres", "s3"
}

type S3Config struct {
	Enabled         bool   `mapstructure:"enabled"`
	Bucket          string `mapstructure:"bucket"`
	Region          string `mapstructure:"region"`
	Endpoint        string `mapstructure:"endpoint"` // For MinIO/LocalStack
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`
}

type PricingConfig struct {
	RemoteURL       string        `mapstructure:"remote_url"`
	RefreshInterval time.Duration `mapstructure:"refresh_interval"`
	FallbackFile    string        `mapstructure:"fallback_file"`
	AliasesFile     string        `mapstructure:"aliases_file"`
}

type ProvidersConfig struct {
	OpenAI    ProviderConfig `mapstructure:"openai"`
	Anthropic ProviderConfig `mapstructure:"anthropic"`
	Gemini    ProviderConfig `mapstructure:"gemini"`
	Azure     ProviderConfig `mapstructure:"azure"`
	Bedrock   BedrockConfig  `mapstructure:"bedrock"`
}

type ProviderConfig struct {
	BaseURL string `mapstructure:"base_url"`
}

type BedrockConfig struct {
	Region string `mapstructure:"region"`
}

func Load(configPath string) (*Config, error) {
	v := viper.New()

	setDefaults(v)

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("majordomo")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/majordomo")
	}

	v.SetEnvPrefix("MAJORDOMO")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.read_timeout", 30*time.Second)
	v.SetDefault("server.write_timeout", 120*time.Second)

	v.SetDefault("storage.driver", "postgres")
	v.SetDefault("storage.postgres.host", "localhost")
	v.SetDefault("storage.postgres.port", 5432)
	v.SetDefault("storage.postgres.user", "")
	v.SetDefault("storage.postgres.password", "")
	v.SetDefault("storage.postgres.database", "majordomo")
	v.SetDefault("storage.postgres.sslmode", "disable")
	v.SetDefault("storage.postgres.max_conns", 20)

	v.SetDefault("logging.store_request_body", false)
	v.SetDefault("logging.store_response_body", false)
	v.SetDefault("logging.max_body_size", 65536)
	v.SetDefault("logging.body_storage", "none") // "none", "postgres", "s3"

	v.SetDefault("s3.enabled", false)
	v.SetDefault("s3.bucket", "")
	v.SetDefault("s3.region", "us-east-1")
	v.SetDefault("s3.endpoint", "")
	v.SetDefault("s3.access_key_id", "")
	v.SetDefault("s3.secret_access_key", "")

	v.SetDefault("pricing.remote_url", "https://www.llm-prices.com/current-v1.json")
	v.SetDefault("pricing.refresh_interval", time.Hour)
	v.SetDefault("pricing.fallback_file", "./pricing.json")
	v.SetDefault("pricing.aliases_file", "./model_aliases.json")

	v.SetDefault("providers.openai.base_url", "https://api.openai.com")
	v.SetDefault("providers.anthropic.base_url", "https://api.anthropic.com")
	v.SetDefault("providers.gemini.base_url", "https://generativelanguage.googleapis.com")
	v.SetDefault("providers.bedrock.region", "us-east-1")

	v.SetDefault("metadata.hll_flush_interval", 60*time.Second)
	v.SetDefault("metadata.active_keys_cache_ttl", 5*time.Minute)

	v.SetDefault("secrets.encryption_key", "")
}
