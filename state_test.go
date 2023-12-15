package main

import (
	"fmt"
	"io/fs"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
)

func initialState() StateDir {
	sd := NewStateDir("machine-name", "")
	sd.getEnv = func(string) string { return "" }
	sd.userConfigDir = func() (string, error) { return "", nil }
	sd.dirExists = func(string) (bool, error) { return false, nil }
	sd.readFileString = func(string) (string, error) { return "", nil }
	sd.writeFileString = func(string, string) error { return nil }

	return sd
}

// Ensure that the -stateDir flag is used for selecting the state directory.
func TestStateDirFlag_IsUsedIfSet(t *testing.T) {
	t.Parallel()

	const stateDirFlag = "some path"

	sd := initialState()
	sd.stateDirFlag = stateDirFlag

	stateDir, err := sd.Compute()

	require.NoError(t, err)
	require.Equal(t, stateDirFlag, stateDir)
}

// Ensure that the TS_STATE_DIR environment variable is used for selecting the state directory.
func TestTSSTATEDIREnvVarIsUsedIfSet(t *testing.T) {
	t.Parallel()

	const stateDirEnv = "some path"

	sd := initialState()
	sd.getEnv = func(string) string { return stateDirEnv }

	stateDir, err := sd.Compute()

	require.NoError(t, err)
	require.Equal(t, stateDirEnv, stateDir)
}

// Ensure that the tsnet-tsnsrv is used if it exists and the machine_name file contents match the -name argument.
func TestTsnetTsnsrvDirIsUsedIfExistsAndMachineNameMatches(t *testing.T) {
	t.Parallel()

	const userConfigDir = "/home/somedir/.config/"
	const legacyTsnetConfigDir = "/home/somedir/.config/tsnet-tsnsrv"

	sd := initialState()
	sd.userConfigDir = func() (string, error) { return userConfigDir, nil }
	sd.dirExists = func(dir string) (bool, error) { return true, nil }
	sd.readFileString = func(file string) (string, error) { return sd.machineName, nil }

	stateDir, err := sd.Compute()

	require.NoError(t, err)
	require.Equal(t, legacyTsnetConfigDir, stateDir)
}

// Ensure that the machine_name file is created in tsnet-tsnsrv if it doesn't exist.
func TestMachineNameFileIsCreatedIfNeeded(t *testing.T) {
	t.Parallel()

	const userConfigDir = "/home/somedir/.config/"
	const legacyTsnetConfigDir = "/home/somedir/.config/tsnet-tsnsrv"
	machineNameFile := path.Join(legacyTsnetConfigDir, "machine-name")
	writeFileStringCalled := false

	sd := initialState()
	sd.userConfigDir = func() (string, error) { return userConfigDir, nil }
	sd.dirExists = func(dir string) (bool, error) { return true, nil }
	sd.readFileString = func(file string) (string, error) { return "", fs.ErrNotExist }
	sd.writeFileString = func(file, contents string) error {
		require.Equal(t, machineNameFile, file)
		require.Equal(t, sd.machineName, contents)
		writeFileStringCalled = true
		return nil
	}

	stateDir, err := sd.Compute()

	require.True(t, writeFileStringCalled)
	require.NoError(t, err)
	require.Equal(t, legacyTsnetConfigDir, stateDir)
}

// Ensure that tsnet-tsnsrv-<name> is used if a tsnet-tsnsrv directory doesn't exist
func TestTsnetTsnsrvNameIsUsedIfLegacyDirDoesntExist(t *testing.T) {
	t.Parallel()

	sd := initialState()
	const userConfigDir = "/home/somedir/.config/"
	newTsnetConfigDir := fmt.Sprintf("/home/somedir/.config/tsnet-tsnsrv-%s", sd.machineName)

	sd.userConfigDir = func() (string, error) { return userConfigDir, nil }
	sd.dirExists = func(dir string) (bool, error) { return false, nil }

	stateDir, err := sd.Compute()

	require.NoError(t, err)
	require.Equal(t, newTsnetConfigDir, stateDir)
}

// Ensure that tsnet-tsnsrv-<name> is used if the machine_name doesn't match.
func TestTsnetTsnsrvNameIsUsedIfMachineNameDoesntMatch(t *testing.T) {
	t.Parallel()

	sd := initialState()
	const userConfigDir = "/home/somedir/.config/"
	newTsnetConfigDir := fmt.Sprintf("/home/somedir/.config/tsnet-tsnsrv-%s", sd.machineName)

	sd.userConfigDir = func() (string, error) { return userConfigDir, nil }
	sd.dirExists = func(dir string) (bool, error) { return true, nil }
	sd.readFileString = func(file string) (string, error) { return "not-a-match", nil }

	stateDir, err := sd.Compute()

	require.NoError(t, err)
	require.Equal(t, newTsnetConfigDir, stateDir)
}
