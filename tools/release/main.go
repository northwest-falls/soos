package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type target struct {
	goos, goarch string
	out          string

	needsHost string
	why       string
}

var targets = []target{
	{goos: "windows", goarch: "amd64", out: "soos-windows-amd64.exe"},
	{goos: "windows", goarch: "arm64", out: "soos-windows-arm64.exe"},
	{goos: "linux", goarch: "amd64", out: "soos-linux-amd64"},
	{goos: "linux", goarch: "arm64", out: "soos-linux-arm64"},
	{
		goos: "darwin", goarch: "arm64", out: "soos-macos-arm64",
		needsHost: "darwin",
		why:       "systray binds to Cocoa through Objective-C, so this needs cgo and clang",
	},
	{
		goos: "darwin", goarch: "amd64", out: "soos-macos-amd64",
		needsHost: "darwin",
		why:       "systray binds to Cocoa through Objective-C, so this needs cgo and clang",
	},
}

func main() {
	version := flag.String("version", "dev", "version to stamp into the binaries")
	outDir := flag.String("out", "dist", "where to put the builds")
	flag.Parse()

	if err := run(*version, *outDir); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func run(version, outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	if err := checkResource(); err != nil {
		return err
	}

	var built []string
	var skipped []target

	for _, t := range targets {
		if t.needsHost != "" && runtime.GOOS != t.needsHost {
			skipped = append(skipped, t)
			continue
		}

		path := filepath.Join(outDir, t.out)
		fmt.Printf("  building %s/%s\n", t.goos, t.goarch)

		if err := build(t, version, path); err != nil {
			return fmt.Errorf("%s/%s: %w", t.goos, t.goarch, err)
		}

		built = append(built, path)
	}

	// Inno resolves its output directory against the script, not the working
	// directory, so it has to be told in absolute terms.
	outAbs, err := filepath.Abs(outDir)
	if err != nil {
		return err
	}

	if setup, err := buildInstaller(version, outAbs); err != nil {
		return err
	} else if setup != "" {
		built = append(built, setup)
	}

	sums, err := checksums(built)
	if err != nil {
		return err
	}

	sumPath := filepath.Join(outDir, "SHA256SUMS")
	if err := os.WriteFile(sumPath, []byte(sums), 0o644); err != nil {
		return err
	}

	fmt.Printf("\n%s\n", sums)

	if len(skipped) > 0 {
		fmt.Println("Not built here:")
		for _, t := range skipped {
			fmt.Printf("  %s/%s, run this on a %s machine. %s\n", t.goos, t.goarch, t.needsHost, t.why)
		}
		fmt.Println()
	}

	fmt.Printf("Checksums written to %s.\n", sumPath)
	fmt.Println("Publish them with the release. They are the whole point of publishing the source.")

	return nil
}

// Setup is what people should be downloading, so it is built and checksummed
// with everything else rather than assembled by hand afterwards.
func buildInstaller(version, outDir string) (string, error) {
	if runtime.GOOS != "windows" {
		fmt.Println("  installer skipped, it needs Windows")
		return "", nil
	}

	iscc := findISCC()
	if iscc == "" {
		fmt.Println("  installer skipped, Inno Setup is not installed")
		fmt.Println("    winget install JRSoftware.InnoSetup")
		return "", nil
	}

	fmt.Println("  building installer")

	cmd := exec.Command(iscc,
		"/DAppVersion="+version,
		"/O"+outDir,
		filepath.Join("installer", "soos.iss"),
	)
	cmd.Stderr = os.Stderr

	if out, err := cmd.Output(); err != nil {
		os.Stderr.Write(out)
		return "", fmt.Errorf("installer: %w", err)
	}

	return filepath.Join(outDir, "SoosSetup.exe"), nil
}

func findISCC() string {
	if p, err := exec.LookPath("ISCC.exe"); err == nil {
		return p
	}

	// Installs per user or machine wide depending on how it was fetched, and
	// winget does the former.
	for _, base := range []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs"),
		os.Getenv("ProgramFiles(x86)"),
		os.Getenv("ProgramFiles"),
	} {
		if base == "" {
			continue
		}
		for _, v := range []string{"Inno Setup 6", "Inno Setup 7"} {
			p := filepath.Join(base, v, "ISCC.exe")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}

	return ""
}

func build(t target, version, out string) error {
	cmd := exec.Command("go", "build",
		"-trimpath",
		// No -s or -w until the binaries are signed. Stripping matches the
		// heuristics antivirus products use for packed malware.
		"-ldflags", "-X main.version="+version,
		"-o", out,
		"./cmd/soos",
	)

	cmd.Env = append(os.Environ(),
		"GOOS="+t.goos,
		"GOARCH="+t.goarch,

		"GOFLAGS=-mod=readonly",
	)

	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func checkResource() error {
	const syso = "cmd/soos/resource_windows_amd64.syso"

	if _, err := os.Stat(syso); err == nil {
		return nil
	}

	fmt.Println("  WARNING: no Windows resource object, so the build will have")
	fmt.Println("  no icon and blank publisher fields. A binary with a blank")
	fmt.Println("  company and no description is the exact shape of malware.")
	fmt.Println()
	fmt.Println("    go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest")
	fmt.Println("    goversioninfo -o " + syso + " versioninfo.json")
	fmt.Println()

	return nil
}

func checksums(paths []string) (string, error) {
	sort.Strings(paths)

	var b strings.Builder

	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return "", err
		}

		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return "", err
		}
		f.Close()

		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(h.Sum(nil)), filepath.Base(p))
	}

	return b.String(), nil
}
