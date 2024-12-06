package broker

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"github.com/ubuntu/authd-oidc-brokers/internal/testutils/golden"
)

var configTypes = map[string]string{
	"valid": `
[oidc]
issuer = https://issuer.url.com
client_id = client_id
`,

	"valid+optional": `
[oidc]
issuer = https://issuer.url.com
client_id = client_id

[users]
home_base_dir = /home
allowed_ssh_suffixes = @issuer.url.com
`,

	"singles": `
[oidc]
issuer = https://ISSUER_URL>
client_id = <CLIENT_ID
`,

	"template": `
[oidc]
issuer = https://<ISSUER_URL>
client_id = <CLIENT_ID>
`,

	"overwrite_lower_precedence": `
[oidc]
issuer = https://lower-precedence-issuer.url.com
client_id = lower_precedence_client_id
`,

	"overwrite_higher_precedence": `
[oidc]
issuer = https://higher-precedence-issuer.url.com
`,
}

func TestParseConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		configType string
		dropInType string

		wantErr bool
	}{
		"Successfully_parse_config_file":                      {},
		"Successfully_parse_config_file_with_optional_values": {configType: "valid+optional"},
		"Successfully_parse_config_with_drop_in_files":        {dropInType: "valid"},

		"Do_not_fail_if_values_contain_a_single_template_delimiter": {configType: "singles"},

		"Error_if_file_does_not_exist":             {configType: "inexistent", wantErr: true},
		"Error_if_file_is_unreadable":              {configType: "unreadable", wantErr: true},
		"Error_if_file_is_not_updated":             {configType: "template", wantErr: true},
		"Error_if_drop_in_directory_is_unreadable": {dropInType: "unreadable-dir", wantErr: true},
		"Error_if_drop_in_file_is_unreadable":      {dropInType: "unreadable-file", wantErr: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			confPath := filepath.Join(t.TempDir(), "broker.conf")

			if tc.configType == "" {
				tc.configType = "valid"
			}
			err := os.WriteFile(confPath, []byte(configTypes[tc.configType]), 0600)
			require.NoError(t, err, "Setup: Failed to write config file")

			switch tc.configType {
			case "inexistent":
				err = os.Remove(confPath)
				require.NoError(t, err, "Setup: Failed to remove config file")
			case "unreadable":
				err = os.Chmod(confPath, 0000)
				require.NoError(t, err, "Setup: Failed to make config file unreadable")
			}

			dropInDir := confPath + ".d"
			if tc.dropInType != "" {
				err = os.Mkdir(dropInDir, 0700)
				require.NoError(t, err, "Setup: Failed to create drop-in directory")
			}

			switch tc.dropInType {
			case "valid":
				// Create multiple drop-in files to test that they are loaded in the correct order.
				err = os.WriteFile(filepath.Join(dropInDir, "00-drop-in.conf"), []byte(configTypes["overwrite_lower_precedence"]), 0600)
				require.NoError(t, err, "Setup: Failed to write drop-in file")
				err = os.WriteFile(filepath.Join(dropInDir, "01-drop-in.conf"), []byte(configTypes["overwrite_higher_precedence"]), 0600)
				require.NoError(t, err, "Setup: Failed to write drop-in file")
				// Create the main config file, to test that the options which are not overwritten by the drop-in files
				// are still present.
				err = os.WriteFile(confPath, []byte(configTypes["valid+optional"]), 0600)
				require.NoError(t, err, "Setup: Failed to write config file")
			case "unreadable-dir":
				err = os.Chmod(dropInDir, 0000)
				require.NoError(t, err, "Setup: Failed to make drop-in directory unreadable")
			case "unreadable-file":
				err = os.WriteFile(filepath.Join(dropInDir, "00-drop-in.conf"), []byte(configTypes["valid"]), 0000)
				require.NoError(t, err, "Setup: Failed to make drop-in file unreadable")
			}

			cfg, err := parseConfigFile(confPath)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			outDir := t.TempDir()
			// Write the names and values of all fields in the config to a file. We can't use the json or yaml
			// packages because they can't access unexported fields.
			var fields []string
			val := reflect.ValueOf(&cfg).Elem()
			typ := reflect.TypeOf(&cfg).Elem()
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				fieldValue := val.Field(i)
				if field.PkgPath != "" {
					//nolint: gosec // We are using unsafe to access unexported fields for testing purposes
					fieldValue = reflect.NewAt(fieldValue.Type(), unsafe.Pointer(fieldValue.UnsafeAddr())).Elem()
				}
				fields = append(fields, fmt.Sprintf("%s=%v", field.Name, fieldValue))
			}
			err = os.WriteFile(filepath.Join(outDir, "config.txt"), []byte(strings.Join(fields, "\n")), 0600)
			require.NoError(t, err)

			golden.CheckOrUpdateFileTree(t, outDir)
		})
	}
}
