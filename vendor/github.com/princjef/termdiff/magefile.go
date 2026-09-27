//go:build mage
// +build mage

package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/princjef/mageutil/bintool"
	"github.com/princjef/mageutil/shellcmd"
)

var linter = bintool.Must(bintool.New(
	"golangci-lint{{.BinExt}}",
	"1.51.1",
	"https://github.com/golangci/golangci-lint/releases/download/v{{.Version}}/golangci-lint-{{.Version}}-{{.GOOS}}-{{.GOARCH}}{{.ArchiveExt}}",
))

var documenter = bintool.Must(bintool.New(
	"gomarkdoc{{.BinExt}}",
	"0.4.1",
	"https://github.com/princjef/gomarkdoc/releases/download/v{{.Version}}/gomarkdoc_{{.Version}}_{{.GOOS}}_{{.GOARCH}}{{.ArchiveExt}}",
))

var changelog = bintool.Must(bintool.New(
	"git-chglog{{.BinExt}}",
	"0.15.4",
	"https://github.com/git-chglog/git-chglog/releases/download/v{{.Version}}/git-chglog_{{.Version}}_{{.GOOS}}_{{.GOARCH}}.tar.gz",
))

func Lint() error {
	if err := linter.Ensure(); err != nil {
		return err
	}

	return linter.Command(`run`).Run()
}

func Generate() error {
	return shellcmd.Command(`go generate .`).Run()
}

func Doc() error {
	if err := documenter.Ensure(); err != nil {
		return err
	}

	return documenter.Command(`.`).Run()
}

func DocVerify() error {
	if err := documenter.Ensure(); err != nil {
		return err
	}

	return documenter.Command(`-c .`).Run()
}

func Release(version string) error {
	if err := changelog.Ensure(); err != nil {
		return err
	}

	out, err := shellcmd.Command(`git status --porcelain`).Output()
	if err != nil {
		return err
	}

	if strings.TrimSpace(string(out)) != "" {
		return errors.New("detected uncommitted files. please commit all files before releasing")
	}

	out, err = shellcmd.Command(`git rev-parse --abbrev-ref HEAD`).Output()
	if err != nil {
		return err
	}

	outStr := strings.TrimSpace(string(out))

	if outStr != "main" {
		return fmt.Errorf("releases should only be made from the main branch. current branch is %s", outStr)
	}

	err = shellcmd.RunAll(
		shellcmd.Command(fmt.Sprintf(`git tag v%s`, version)),
		changelog.Command("-o CHANGELOG.md"),
		shellcmd.Command(fmt.Sprintf(`git commit -am 'chore: release v%s [skip ci]'`, version)),
		shellcmd.Command(fmt.Sprintf(`git push origin v%s`, version)),
		`git push origin main`,
	)
	if err != nil {
		return err
	}

	fmt.Printf("Published release v%s", version)
	return nil
}

func Test() error {
	return shellcmd.Command(`go test -count 1 -coverprofile=coverage.txt ./...`).Run()
}

func Coverage() error {
	return shellcmd.Command(`go tool cover -html=coverage.txt`).Run()
}
