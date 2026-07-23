package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/release"
)

var (
	stableVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	safeAssetName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,254}$`)
)

type assetFlags []string

func (values *assetFlags) String() string { return strings.Join(*values, ";") }
func (values *assetFlags) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "release-manifest: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("release-manifest", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var assets assetFlags
	version := flags.String("version", "", "stable SemVer without v prefix")
	catalogSchema := flags.Int("catalog-schema", 0, "catalog schema version")
	stateSchema := flags.Int("state-schema", 0, "state schema version")
	catalogSource := flags.String("catalog-source", "", "development catalog source to render")
	catalogOutput := flags.String("catalog-output", "", "rendered release catalog output")
	flags.Var(&assets, "asset", "kind,os,arch,path (repeat exactly six times)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if !stableVersion.MatchString(*version) {
		return fmt.Errorf("version %q is not stable SemVer", *version)
	}
	if *catalogSchema <= 0 || *stateSchema <= 0 {
		return errors.New("catalog-schema and state-schema must be positive")
	}
	if *catalogSchema < release.CompiledMinCatalogSchema || *catalogSchema > release.CompiledMaxCatalogSchema {
		return fmt.Errorf("catalog schema %d is outside the compiled range", *catalogSchema)
	}
	if *stateSchema < release.CompiledMinStateSchema || *stateSchema > release.CompiledMaxStateSchema {
		return fmt.Errorf("state schema %d is outside the compiled range", *stateSchema)
	}
	if (*catalogSource == "") != (*catalogOutput == "") {
		return errors.New("catalog-source and catalog-output must be provided together")
	}
	if *catalogSource != "" {
		if !catalogAssetUsesOutput(assets, *catalogOutput) {
			return errors.New("catalog asset must use catalog-output")
		}
		if err := renderCatalog(*catalogSource, *catalogOutput, *version, *catalogSchema); err != nil {
			return err
		}
	}

	manifestAssets := make([]release.Asset, 0, len(assets))
	platforms := make(map[string]bool, 4)
	singletons := make(map[string]bool, 2)
	names := make(map[string]bool, len(assets))
	for _, specification := range assets {
		asset, err := inspectAsset(specification)
		if err != nil {
			return err
		}
		if names[asset.Name] {
			return fmt.Errorf("duplicate asset name %q", asset.Name)
		}
		names[asset.Name] = true
		switch asset.Kind {
		case "binary":
			platform := asset.OS + "/" + asset.Arch
			if platform != "darwin/amd64" && platform != "darwin/arm64" &&
				platform != "linux/amd64" && platform != "linux/arm64" {
				return fmt.Errorf("unsupported binary platform %s", platform)
			}
			if platforms[platform] {
				return fmt.Errorf("duplicate binary platform %s", platform)
			}
			platforms[platform] = true
		case "source", "catalog":
			if asset.OS != "" || asset.Arch != "" {
				return fmt.Errorf("%s asset must not declare a platform", asset.Kind)
			}
			if singletons[asset.Kind] {
				return fmt.Errorf("duplicate %s asset", asset.Kind)
			}
			singletons[asset.Kind] = true
		default:
			return fmt.Errorf("unsupported asset kind %q", asset.Kind)
		}
		manifestAssets = append(manifestAssets, asset)
	}
	for _, platform := range []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"} {
		if !platforms[platform] {
			return fmt.Errorf("missing binary platform %s", platform)
		}
	}
	if !singletons["source"] || !singletons["catalog"] || len(manifestAssets) != 6 {
		return errors.New("release requires four binaries, one source, and one catalog")
	}
	sort.Slice(manifestAssets, func(i, j int) bool {
		return assetOrder(manifestAssets[i]) < assetOrder(manifestAssets[j])
	})

	manifest := release.Manifest{
		Version:       *version,
		CatalogSchema: *catalogSchema,
		StateSchema:   *stateSchema,
		Assets:        manifestAssets,
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(manifest)
}

func renderCatalog(source, output, version string, schemaVersion int) error {
	sourcePath, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("resolve catalog source: %w", err)
	}
	outputPath, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("resolve catalog output: %w", err)
	}
	if sourcePath == outputPath {
		return errors.New("catalog source and output must differ")
	}
	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open catalog source: %w", err)
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var value model.Catalog
	decodeErr := decoder.Decode(&value)
	var trailing any
	trailingErr := decoder.Decode(&trailing)
	closeErr := file.Close()
	if decodeErr != nil {
		return fmt.Errorf("decode catalog source: %w", decodeErr)
	}
	if !errors.Is(trailingErr, io.EOF) {
		if trailingErr == nil {
			trailingErr = errors.New("multiple JSON values")
		}
		return fmt.Errorf("decode catalog source trailing data: %w", trailingErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close catalog source: %w", closeErr)
	}
	if value.Release != "development" {
		return fmt.Errorf("catalog source release %q is not development", value.Release)
	}
	if value.Version != schemaVersion {
		return fmt.Errorf("catalog version %d differs from manifest schema %d", value.Version, schemaVersion)
	}
	value.Release = version

	temp, err := os.CreateTemp(filepath.Dir(outputPath), ".resources.json-")
	if err != nil {
		return fmt.Errorf("create rendered catalog: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set rendered catalog mode: %w", err)
	}
	encoder := json.NewEncoder(temp)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		_ = temp.Close()
		return fmt.Errorf("encode rendered catalog: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close rendered catalog: %w", err)
	}
	if err := os.Rename(tempPath, outputPath); err != nil {
		return fmt.Errorf("publish rendered catalog: %w", err)
	}
	return nil
}

func catalogAssetUsesOutput(assets []string, output string) bool {
	want, err := filepath.Abs(output)
	if err != nil {
		return false
	}
	for _, specification := range assets {
		parts := strings.SplitN(specification, ",", 4)
		if len(parts) != 4 || parts[0] != "catalog" {
			continue
		}
		got, err := filepath.Abs(parts[3])
		return err == nil && got == want
	}
	return false
}

func inspectAsset(specification string) (release.Asset, error) {
	parts := strings.SplitN(specification, ",", 4)
	if len(parts) != 4 || parts[3] == "" {
		return release.Asset{}, fmt.Errorf("invalid asset %q: want kind,os,arch,path", specification)
	}
	info, err := os.Stat(parts[3])
	if err != nil {
		return release.Asset{}, fmt.Errorf("stat asset %q: %w", parts[3], err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return release.Asset{}, fmt.Errorf("asset %q must be a non-empty regular file", parts[3])
	}
	name := filepath.Base(parts[3])
	if !safeAssetName.MatchString(name) {
		return release.Asset{}, fmt.Errorf("unsafe asset name %q", name)
	}
	file, err := os.Open(parts[3])
	if err != nil {
		return release.Asset{}, fmt.Errorf("open asset %q: %w", parts[3], err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return release.Asset{}, fmt.Errorf("hash asset %q: %w", parts[3], err)
	}
	return release.Asset{
		Kind: parts[0], OS: parts[1], Arch: parts[2], Name: name,
		Size: info.Size(), SHA256: hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func assetOrder(asset release.Asset) string {
	switch asset.Kind {
	case "binary":
		return "0/" + asset.OS + "/" + asset.Arch
	case "source":
		return "1"
	default:
		return "2"
	}
}
