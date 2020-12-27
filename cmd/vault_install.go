package cmd

import (
	"fmt"
	"github.com/jsiebens/hashi-up/pkg/config"
	"github.com/jsiebens/hashi-up/pkg/operator"
	"github.com/markbates/pkger"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/thanhpk/randstr"
	"path/filepath"
	"strings"
)

func InstallVaultCommand() *cobra.Command {

	var ignoreConfigFlags bool
	var skipEnable bool
	var skipStart bool
	var binary string
	var version string

	var generatedConfigFile string
	var configFiles []string

	var flags = config.VaultConfig{}

	var command = &cobra.Command{
		Use:          "install",
		SilenceUsage: true,
	}

	command.Flags().BoolVarP(&ignoreConfigFlags, "ignore-config-flags", "i", false, "If set to false will generate a configuration file based on CLI flags, otherwise the flags are ignored")
	command.Flags().BoolVar(&skipEnable, "skip-enable", false, "If set to true will not enable or start Vault service")
	command.Flags().BoolVar(&skipStart, "skip-start", false, "If set to true will not start Vault service")
	command.Flags().StringVarP(&binary, "package", "p", "", "Upload and use this Vault package instead of downloading")
	command.Flags().StringVarP(&version, "version", "v", "", "Version of Vault to install")

	command.Flags().StringVarP(&generatedConfigFile, "generated-config-file", "c", "vault.hcl", "Name of the generated config file")
	command.Flags().StringArrayVarP(&configFiles, "file", "f", []string{}, "Additional configuration file to upload")

	command.Flags().StringVar(&flags.CertFile, "cert-file", "", "Vault: the certificate for TLS. (see Vault documentation for more info)")
	command.Flags().StringVar(&flags.KeyFile, "key-file", "", "Vault: the private key for the certificate. (see Vault documentation for more info)")
	command.Flags().StringArrayVar(&flags.Address, "address", []string{"0.0.0.0:8200"}, "Vault: the address to bind to for listening. (see Vault documentation for more info)")
	command.Flags().StringVar(&flags.ApiAddr, "api-addr", "", "Vault: the address (full URL) to advertise to other Vault servers in the cluster for client redirection. (see Vault documentation for more info)")
	command.Flags().StringVar(&flags.ClusterAddr, "cluster-addr", "", "Vault: the address to advertise to other Vault servers in the cluster for request forwarding. (see Vault documentation for more info)")
	command.Flags().StringVar(&flags.Storage, "storage", "file", "Vault: the type of storage backend. Currently only \"file\" of \"consul\" is supported. (see Vault documentation for more info)")
	command.Flags().StringVar(&flags.ConsulAddr, "consul-addr", "127.0.0.1:8500", "Vault: the address of the Consul agent to communicate with. (see Vault documentation for more info)")
	command.Flags().StringVar(&flags.ConsulPath, "consul-path", "vault/", "Vault: the path in Consul's key-value store where Vault data will be stored. (see Vault documentation for more info)")
	command.Flags().StringVar(&flags.ConsulToken, "consul-token", "", "Vault: the Consul ACL token with permission to read and write from the path in Consul's key-value store. (see Vault documentation for more info)")
	command.Flags().StringVar(&flags.ConsulCaFile, "consul-tls-ca-file", "", "Vault: the path to the CA certificate used for Consul communication. (see Vault documentation for more info)")
	command.Flags().StringVar(&flags.ConsulCertFile, "consul-tls-cert-file", "", "Vault: the path to the certificate for Consul communication. (see Vault documentation for more info)")
	command.Flags().StringVar(&flags.ConsulKeyFile, "consul-tls-key-file", "", "Vault: the path to the private key for Consul communication. (see Vault documentation for more info)")

	command.RunE = func(command *cobra.Command, args []string) error {
		if !runLocal && len(sshTargetAddr) == 0 {
			return fmt.Errorf("required ssh-target-addr flag is missing")
		}

		var generatedConfig string

		if !ignoreConfigFlags {
			generatedConfig = flags.GenerateConfigFile()
			if !strings.HasSuffix(generatedConfigFile, ".hcl") {
				generatedConfigFile = generatedConfigFile + ".hcl"
			}
		}

		if len(binary) == 0 && len(version) == 0 {
			latest, err := config.GetLatestVersion("vault")

			if err != nil {
				return errors.Wrapf(err, "unable to get latest version number, define a version manually with the --version flag")
			}

			version = latest
		}

		callback := func(op operator.CommandOperator) error {
			dir := "/tmp/vault-installation." + randstr.String(6)

			defer op.Execute("rm -rf " + dir)

			_, err := op.Execute("mkdir -p " + dir + "/config")
			if err != nil {
				return fmt.Errorf("error received during installation: %s", err)
			}

			if len(binary) != 0 {
				info("Uploading Vault package...")
				err = op.UploadFile(binary, dir+"/vault.zip", "0644")
				if err != nil {
					return fmt.Errorf("error received during upload Vault package: %s", err)
				}
			}

			if !ignoreConfigFlags {
				info("Uploading generated Vault configuration...")
				err = op.Upload(strings.NewReader(generatedConfig), dir+"/config/"+generatedConfigFile, "0640")
				if err != nil {
					return fmt.Errorf("error received during upload consul configuration: %s", err)
				}

				if flags.EnableTLS() {
					configFiles = append([]string{flags.KeyFile, flags.CertFile}, configFiles...)
				}

				if flags.EnableConsulTLS() {
					configFiles = append([]string{flags.ConsulCaFile, flags.ConsulCertFile, flags.ConsulKeyFile}, configFiles...)
				}
			}

			for _, s := range configFiles {
				info(fmt.Sprintf("Uploading %s...", s))
				_, filename := filepath.Split(expandPath(s))
				err = op.UploadFile(expandPath(s), dir+"/config/"+filename, "0640")
				if err != nil {
					return fmt.Errorf("error received during upload file: %s", err)
				}
			}

			installScript, err := pkger.Open("/scripts/install_vault.sh")

			if err != nil {
				return err
			}

			defer installScript.Close()

			err = op.Upload(installScript, dir+"/install.sh", "0755")
			if err != nil {
				return fmt.Errorf("error received during upload install script: %s", err)
			}

			info("Installing Vault...")
			_, err = op.Execute(fmt.Sprintf("cat %s/install.sh | TMP_DIR='%s' VAULT_VERSION='%s' SKIP_ENABLE='%t' SKIP_START='%t' sh -\n", dir, dir, version, skipEnable, skipStart))
			if err != nil {
				return fmt.Errorf("error received during installation: %s", err)
			}

			return nil
		}

		if runLocal {
			return operator.ExecuteLocal(callback)
		} else {
			return operator.ExecuteRemote(sshTargetAddr, sshTargetUser, sshTargetKey, callback)
		}
	}

	return command
}
