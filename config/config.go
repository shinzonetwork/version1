package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

const CollectionName = "shinzo"

type Config struct {
	DefraDB struct {
		Host          string `yaml:"host"`
		Port          int    `yaml:"port"`
		KeyringSecret string `yaml:"keyring_secret"`
		P2P           struct {
			Enabled        bool     `yaml:"enabled"`
			BootstrapPeers []string `yaml:"bootstrap_peers"`
			ListenAddr     string   `yaml:"listen_addr"`
		} `yaml:"p2p"`
		Store struct {
			Path string `yaml:"path"`
		} `yaml:"store"`
	} `yaml:"defradb"`

	Alchemy struct {
		APIKey  string `yaml:"api_key"`
		Network string `yaml:"network"`
	} `yaml:"alchemy"`

	Indexer struct {
		BlockPollingInterval float64 `yaml:"block_polling_interval"`
		BatchSize            int     `yaml:"batch_size"`
		StartHeight          int     `yaml:"start_height"`
		Pipeline             struct {
			FetchBlocks struct {
				Workers    int `yaml:"workers"`
				BufferSize int `yaml:"buffer_size"`
			} `yaml:"fetch_blocks"`
			ProcessTransactions struct {
				Workers    int `yaml:"workers"`
				BufferSize int `yaml:"buffer_size"`
			} `yaml:"process_transactions"`
			StoreData struct {
				Workers    int `yaml:"workers"`
				BufferSize int `yaml:"buffer_size"`
			} `yaml:"store_data"`
		} `yaml:"pipeline"`
	} `yaml:"indexer"`

	Source struct {
		NodeURL   string `yaml:"node_url"`
		ChainID   string `yaml:"chain_id"`
		Consensus struct {
			Enabled    bool     `yaml:"enabled"`
			Validators []string `yaml:"validators"`
			P2PPort    int      `yaml:"p2p_port"`
			RPCPort    int      `yaml:"rpc_port"`
		} `yaml:"consensus"`
	} `yaml:"source"`
	Logger struct {
		Development bool `yaml:"development"`
	} `yaml:"logger"`
}

// LoadConfig loads configuration from a YAML file and environment variables
func LoadConfig(path string) (*Config, error) {
	// Load .env file if it exists
	_ = godotenv.Load()

	// Load YAML config
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Override with environment variables
	if keyringSecret := os.Getenv("DEFRA_KEYRING_SECRET"); keyringSecret != "" {
		cfg.DefraDB.KeyringSecret = keyringSecret
	}

	if apiKey := os.Getenv("ALCHEMY_API_KEY"); apiKey != "" {
		cfg.Alchemy.APIKey = apiKey
	}

	if network := os.Getenv("ALCHEMY_NETWORK"); network != "" {
		cfg.Alchemy.Network = network
	}

	if port := os.Getenv("DEFRA_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.DefraDB.Port = p
		}
	}

	if logger := os.Getenv("DEFRA_LOGGING_DEVELOPMENT"); logger != "" {
		if l, err := strconv.ParseBool(logger); err == nil {
			cfg.Logger.Development = l
		}
	}

	if host := os.Getenv("DEFRA_HOST"); host != "" {
		cfg.DefraDB.Host = host
	}

	if startHeight := os.Getenv("INDEXER_START_HEIGHT"); startHeight != "" {
		if h, err := strconv.Atoi(startHeight); err == nil {
			cfg.Indexer.StartHeight = h
		}
	}

	return &cfg, nil
}
