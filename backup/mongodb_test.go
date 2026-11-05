package backup

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"monodb-backup/config"
)

func TestMongoDumpIgnoresBackupAsTables(t *testing.T) {
	oldParams, oldDateNow := params, dateNow
	t.Cleanup(func() {
		params, dateNow = oldParams, oldDateNow
	})
	params = &config.Params{Database: "mongodb", BackupAsTables: true}
	dateNow = rightNow{day: "Thursday"}

	got := dumpName("app", config.Rotation{Enabled: true, Suffix: "day"}, "")
	if got != "app-Thursday" {
		t.Fatalf("dumpName() = %q, want %q", got, "app-Thursday")
	}
}

func TestMongoCommandsKeepPasswordOutOfArgs(t *testing.T) {
	oldParams := params
	t.Cleanup(func() { params = oldParams })
	params = &config.Params{Remote: config.Remote{
		IsRemote: true,
		Host:     "mongo.internal",
		Port:     "27017",
		User:     "smokeadmin",
		Password: "smoke-only-password",
	}}

	tests := []struct {
		name    string
		prefix  string
		command func() (*exec.Cmd, func(), error)
	}{
		{name: "mongosh", prefix: "--file=", command: mongoShellCommand},
		{name: "mongodump", prefix: "--config=", command: func() (*exec.Cmd, func(), error) {
			return mongoDumpCommand([]string{"--db=app"})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd, cleanup, err := test.command()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cleanup)
			if strings.Contains(strings.Join(cmd.Args, " "), params.Remote.Password) {
				t.Fatalf("password exposed in process arguments: %v", cmd.Args)
			}

			secretPath := ""
			for _, arg := range cmd.Args {
				if value, ok := strings.CutPrefix(arg, test.prefix); ok {
					secretPath = value
				}
			}
			if secretPath == "" {
				t.Fatalf("protected credential file argument missing: %v", cmd.Args)
			}
			info, err := os.Stat(secretPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("credential file mode = %o, want 600", got)
			}
			contents, err := os.ReadFile(secretPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(contents), params.Remote.Password) {
				t.Fatal("credential file does not contain the configured password")
			}
			cleanup()
			if _, err := os.Stat(secretPath); !os.IsNotExist(err) {
				t.Fatalf("credential file remains after cleanup: %v", err)
			}
		})
	}
}

func TestMongoHelperProcess(t *testing.T) {
	switch os.Getenv("MONODB_TEST_HELPER") {
	case "dump":
		for {
			time.Sleep(time.Hour)
		}
	case "zip":
		os.Exit(1)
	}
}

func TestMongoEncryptedPipelineReapsDumpWhenSevenZipFails(t *testing.T) {
	newHelper := func(kind string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMongoHelperProcess$")
		cmd.Env = append(os.Environ(), "MONODB_TEST_HELPER="+kind)
		return cmd
	}
	dumpCmd := newHelper("dump")
	t.Cleanup(func() {
		if dumpCmd.Process != nil && dumpCmd.ProcessState == nil {
			_ = dumpCmd.Process.Kill()
			_, _ = dumpCmd.Process.Wait()
		}
	})

	if err := runMongoEncryptedPipeline(dumpCmd, newHelper("zip")); err == nil {
		t.Fatal("pipeline succeeded despite the compression helper failing")
	}
	if dumpCmd.ProcessState == nil {
		t.Fatal("mongodump was not reaped after compression failed")
	}
}
