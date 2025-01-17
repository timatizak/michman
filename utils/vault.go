package utils

import (
	vaultapi "github.com/hashicorp/vault/api"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

type OsCredentials struct {
	OsAuthUrl            string
	OsPassword           string
	OsProjectID          string
	OsProjectName        string
	OsProjectDomainID    string
	OsInterface          string
	OsRegionName         string
	OsTenantId           string
	OsTenantName         string
	OsUserName           string
	OsSwiftUserName      string
	OsSwiftPassword      string
	OsComputeApiVersion  string
	OsNovaVersion        string
	OsAuthType           string
	OsCloudname          string
	OsIdentityApiVersion string
	OsImageApiVersion    string
	OsNoCache            string
	OsProjectDomainName  string
	OsUserDomainName     string
	OsVolumeApiVersion   string
	OsPythonwarnings     string
	OsNoProxy            string
}

type DockerCredentials struct {
	Url      string
	User     string
	Password string
}

type CbCredentials struct {
	Address  string `yaml:"cb_address"`
	Username string `yaml:"cb_username"`
	Password string `yaml:"cb_password"`
}

type HydraCredentials struct {
	RedirectUri  string
	ClientId     string
	ClientSecret string
}

type SecretStorage interface {
	ConnectVault() (*vaultapi.Client, *Config)
}

type VaultCommunicator struct {
	config Config
}

func (vc *VaultCommunicator) Init() error {
	path, err := os.Getwd()
	if err != nil {
		log.Fatalln(err)
	}

	var vaultConfigPath string
	if UseBasePath {
		vaultConfigPath = filepath.Join(path, ConfigPath)
	} else {
		vaultConfigPath = ConfigPath
	}

	vaultBs, err := ioutil.ReadFile(vaultConfigPath)
	if err := yaml.Unmarshal(vaultBs, &vc.config); err != nil {
		log.Fatalln(err)
	}
	return nil
}

func (vc *VaultCommunicator) ConnectVault() (*vaultapi.Client, *Config) {
	client, err := vaultapi.NewClient(&vaultapi.Config{
		Address: vc.config.VaultAddr,
	})
	if err != nil {
		log.Fatalln(err)
	}

	client.SetToken(vc.config.Token)
	return client, &vc.config
}
