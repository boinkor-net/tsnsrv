package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

type StateDir struct {
	machineName     string
	stateDirFlag    string
	getEnv          func(string) string
	userConfigDir   func() (string, error)
	dirExists       func(string) (bool, error)
	readFileString  func(string) (string, error)
	writeFileString func(string, string) error
}

func NewStateDir(machineName, stateDirFlag string) StateDir {
	return StateDir{
		machineName:     machineName,
		stateDirFlag:    stateDirFlag,
		getEnv:          os.Getenv,
		userConfigDir:   os.UserConfigDir,
		dirExists:       dirExists,
		readFileString:  readFileString,
		writeFileString: writeFileString,
	}
}

func (sd StateDir) Compute() (string, error) {
	// Set command line flag
	if sd.stateDirFlag != "" {
		return sd.stateDirFlag, nil
	}

	// Set TS_STATE_DIR env var
	tsStateDirEnv := sd.getEnv("TS_STATE_DIR")
	if tsStateDirEnv != "" {
		return tsStateDirEnv, nil
	}

	// Looking for legacy tsnet-tsnsrv configuration directory
	userConfigDir, err := sd.userConfigDir()
	if err != nil {
		return "", fmt.Errorf("unable to find user config directory. %w", err)
	}
	legacyTsnetConfigDir := path.Join(userConfigDir, "tsnet-tsnsrv")
	legacyTsnetDirExists, err := sd.dirExists(legacyTsnetConfigDir)
	if err != nil {
		return "", fmt.Errorf("unable to determine existence of legacy tsnet config directory. %w", err)
	}

	// The tsnet-tsnet directory doesn't exist so we can just create a unique configuration directory for the given
	// machine name.
	if !legacyTsnetDirExists {
		return path.Join(userConfigDir, fmt.Sprintf("tsnet-tsnsrv-%s", sd.machineName)), nil
	}

	// The tsnet-tsnet directory does exist reach the machine name file and see if they match
	machineNamePath := path.Join(legacyTsnetConfigDir, "machine-name")
	readName, err := sd.readFileString(machineNamePath)
	if errors.Is(err, fs.ErrNotExist) {
		err = sd.writeFileString(machineNamePath, sd.machineName)
		if err != nil {
			return "", fmt.Errorf("unable to write machine name to legacy config dir. %w", err)
		}

		return legacyTsnetConfigDir, nil
	}
	if err != nil {
		return "", fmt.Errorf("unable to read legacy machine-name file. %w", err)
	}

	if strings.TrimSpace(readName) == sd.machineName {
		return legacyTsnetConfigDir, nil
	}

	return path.Join(userConfigDir, fmt.Sprintf("tsnet-tsnsrv-%s", sd.machineName)), nil
}

func lockFilePath() string {
	return path.Join(os.TempDir(), "tsnsrv.lock")
}

var tryLockTimeoutErr = errors.New("timeout trying to get the file lock")

func lockContext(ctx context.Context) context.Context {
	ctx, _ = context.WithTimeoutCause(ctx, time.Second*5, tryLockTimeoutErr)
	return ctx
}

func tryLock(ctx context.Context, readLock bool) (func() error, error) {
	lockFile := lockFilePath()
	lock := flock.New(lockFile)
	ctx = lockContext(ctx)
	lockFn := lock.TryLockContext
	if readLock {
		lockFn = lock.TryRLockContext
	}

	locked, err := lockFn(ctx, time.Millisecond*100)
	if errors.Is(err, tryLockTimeoutErr) {
		return nil, fmt.Errorf("timeout trying to get lock %s another process is using it", lockFile)
	}
	if err != nil {
		return nil, fmt.Errorf("trying to lock %s. %w", lockFile, err)
	}
	if !locked {
		return nil, fmt.Errorf("unable to get lock %s", lockFile)
	}

	return lock.Unlock, nil
}

func readFileString(file string) (string, error) {
	unlocker, err := tryLock(context.Background(), true)
	if err != nil {
		return "", err
	}
	defer unlocker()

	bytes, err := os.ReadFile(file)
	return string(bytes), err
}

func writeFileString(file, contents string) error {
	unlocker, err := tryLock(context.Background(), false)
	if err != nil {
		return err
	}
	defer unlocker()

	return os.WriteFile(file, []byte(contents), 0644)
}

func dirExists(dir string) (bool, error) {
	_, err := os.Stat(dir)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}

	return false, err
}
