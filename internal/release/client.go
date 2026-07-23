package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const DefaultLatestReleaseEndpoint = "https://api.github.com/repos/juty9026/terrapod/releases/latest"

type Client struct {
	HTTP                 *http.Client
	Endpoint             string
	CacheDir             string
	Verifier             Verifier
	AllowedRedirectHosts []string
}

type VerifiedRelease struct {
	Manifest Manifest
	Files    map[string]string
	assets   map[string]githubAsset
	client   *Client
	seal     [sha256.Size]byte
}

type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Assets     []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"browser_download_url"`
}

func (c Client) LatestStable(ctx context.Context) (VerifiedRelease, error) {
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = DefaultLatestReleaseEndpoint
	}
	client, err := c.safeHTTP(endpoint)
	if err != nil {
		return VerifiedRelease{}, err
	}
	metadata, err := getBounded(ctx, client, endpoint, MaxManifestSize)
	if err != nil {
		return VerifiedRelease{}, fmt.Errorf("fetch latest release metadata: %w", err)
	}
	var latest githubRelease
	if err := decodeSingleJSON(metadata, &latest); err != nil {
		return VerifiedRelease{}, fmt.Errorf("decode latest release metadata: %w", err)
	}
	if latest.Draft || latest.Prerelease {
		return VerifiedRelease{}, errors.New("latest GitHub release is not stable")
	}
	assets, err := indexGitHubAssets(latest.Assets)
	if err != nil {
		return VerifiedRelease{}, err
	}
	manifestMeta, ok := assets["release.json"]
	if !ok {
		return VerifiedRelease{}, errors.New("latest release has no release.json")
	}
	signatureMeta, ok := assets["release.json.sig"]
	if !ok {
		return VerifiedRelease{}, errors.New("latest release has no release.json.sig")
	}
	if err := c.requireAllowedHost(endpoint, manifestMeta.URL); err != nil {
		return VerifiedRelease{}, err
	}
	if err := c.requireAllowedHost(endpoint, signatureMeta.URL); err != nil {
		return VerifiedRelease{}, err
	}
	manifestData, err := getBounded(ctx, client, manifestMeta.URL, MaxManifestSize)
	if err != nil {
		return VerifiedRelease{}, fmt.Errorf("fetch release manifest: %w", err)
	}
	if manifestMeta.Size > 0 && int64(len(manifestData)) != manifestMeta.Size {
		return VerifiedRelease{}, errors.New("release manifest size differs from GitHub metadata")
	}
	signature, err := getBounded(ctx, client, signatureMeta.URL, MaxManifestSize)
	if err != nil {
		return VerifiedRelease{}, fmt.Errorf("fetch release signature: %w", err)
	}
	if signatureMeta.Size > 0 && int64(len(signature)) != signatureMeta.Size {
		return VerifiedRelease{}, errors.New("release signature size differs from GitHub metadata")
	}
	manifest, err := c.Verifier.VerifyManifest(manifestData, signature)
	if err != nil {
		return VerifiedRelease{}, err
	}
	if latest.TagName != "v"+manifest.Version {
		return VerifiedRelease{}, fmt.Errorf("GitHub tag %q does not match manifest version %q", latest.TagName, manifest.Version)
	}
	for _, asset := range manifest.Assets {
		remote, ok := assets[asset.Name]
		if !ok {
			return VerifiedRelease{}, fmt.Errorf("GitHub release has no declared asset %q", asset.Name)
		}
		if remote.Size != asset.Size {
			return VerifiedRelease{}, fmt.Errorf("asset %q size differs from GitHub metadata", asset.Name)
		}
		if err := c.requireAllowedHost(endpoint, remote.URL); err != nil {
			return VerifiedRelease{}, fmt.Errorf("asset %q: %w", asset.Name, err)
		}
	}
	bound := c.withHTTP(client)
	release := VerifiedRelease{Manifest: manifest, assets: assets, client: &bound}
	if err := release.sealManifest(); err != nil {
		return VerifiedRelease{}, err
	}
	return release, nil
}

func (c Client) withHTTP(client *http.Client) Client { c.HTTP = client; return c }

func (r VerifiedRelease) localAsset(ctx context.Context, asset Asset) (string, error) {
	if err := r.verifySeal(); err != nil {
		return "", err
	}
	if path := r.Files[asset.Name]; path != "" {
		ok, err := matchingRegularFile(path, asset)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("asset %q checksum or size mismatch", asset.Name)
		}
		return path, nil
	}
	remote, ok := r.assets[asset.Name]
	if !ok || r.client == nil {
		return "", fmt.Errorf("verified release has no source for asset %q", asset.Name)
	}
	return r.client.downloadAsset(ctx, remote.URL, asset)
}

func (r *VerifiedRelease) sealManifest() error {
	if !r.Manifest.verified {
		return errors.New("release manifest was not verified")
	}
	data, err := json.Marshal(r.Manifest)
	if err != nil {
		return err
	}
	r.seal = sha256.Sum256(data)
	return nil
}

func (r VerifiedRelease) verifySeal() error {
	if !r.Manifest.verified {
		return errors.New("release manifest was not verified")
	}
	data, err := json.Marshal(r.Manifest)
	if err != nil {
		return err
	}
	if sha256.Sum256(data) != r.seal {
		return errors.New("verified release manifest was modified")
	}
	return nil
}

func (c Client) downloadAsset(ctx context.Context, rawURL string, asset Asset) (string, error) {
	if asset.Size <= 0 || asset.Size > MaxAssetSize || !digestPattern.MatchString(asset.SHA256) || !assetNamePattern.MatchString(asset.Name) {
		return "", fmt.Errorf("invalid declared asset %q", asset.Name)
	}
	if _, err := requireHTTPS(rawURL); err != nil {
		return "", err
	}
	if err := ensureRealDirectory(c.CacheDir, 0o700); err != nil {
		return "", fmt.Errorf("prepare release cache: %w", err)
	}
	client := c.HTTP
	if client == nil {
		return "", errors.New("release HTTP client is required")
	}
	final := filepath.Join(c.CacheDir, asset.SHA256+"-"+asset.Name)
	if ok, err := matchingRegularFile(final, asset); err != nil {
		return "", err
	} else if ok {
		return final, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("build asset request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download asset %q: %w", asset.Name, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("download asset %q: HTTP status %s", asset.Name, response.Status)
	}
	temporary, err := os.CreateTemp(c.CacheDir, ".tmp-")
	if err != nil {
		return "", fmt.Errorf("create asset temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(response.Body, asset.Size+1))
	closeErr := errors.Join(temporary.Sync(), temporary.Close())
	if copyErr != nil {
		return "", fmt.Errorf("download asset %q: %w", asset.Name, copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("persist asset %q: %w", asset.Name, closeErr)
	}
	if written != asset.Size {
		return "", fmt.Errorf("asset %q size %d, want %d", asset.Name, written, asset.Size)
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != asset.SHA256 {
		return "", fmt.Errorf("asset %q checksum mismatch", asset.Name)
	}
	if err := os.Chmod(temporaryPath, 0o400); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, final); err != nil {
		if ok, checkErr := matchingRegularFile(final, asset); checkErr == nil && ok {
			return final, nil
		}
		return "", fmt.Errorf("commit asset %q to cache: %w", asset.Name, err)
	}
	if err := syncDirectory(c.CacheDir); err != nil {
		return "", err
	}
	return final, nil
}

func (c Client) safeHTTP(endpoint string) (*http.Client, error) {
	parsed, err := requireHTTPS(endpoint)
	if err != nil {
		return nil, err
	}
	if c.HTTP == nil {
		return nil, errors.New("release HTTP client is required")
	}
	copy := *c.HTTP
	allowed := map[string]bool{strings.ToLower(parsed.Hostname()): true}
	for _, host := range c.AllowedRedirectHosts {
		allowed[strings.ToLower(host)] = true
	}
	if parsed.Hostname() == "api.github.com" {
		for _, host := range []string{"api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"} {
			allowed[host] = true
		}
	}
	prior := copy.CheckRedirect
	copy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return errors.New("release redirect must use HTTPS")
		}
		if !allowed[strings.ToLower(req.URL.Hostname())] {
			return fmt.Errorf("release redirect host %q is not allowed", req.URL.Hostname())
		}
		if len(via) > 10 {
			return errors.New("too many release redirects")
		}
		if prior != nil {
			return prior(req, via)
		}
		return nil
	}
	return &copy, nil
}

func (c Client) requireAllowedHost(endpoint, raw string) error {
	base, err := requireHTTPS(endpoint)
	if err != nil {
		return err
	}
	target, err := requireHTTPS(raw)
	if err != nil {
		return err
	}
	allowed := map[string]bool{strings.ToLower(base.Hostname()): true}
	for _, host := range c.AllowedRedirectHosts {
		allowed[strings.ToLower(host)] = true
	}
	if base.Hostname() == "api.github.com" {
		for _, host := range []string{"api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"} {
			allowed[host] = true
		}
	}
	if !allowed[strings.ToLower(target.Hostname())] {
		return fmt.Errorf("release asset host %q is not allowed", target.Hostname())
	}
	return nil
}

func requireHTTPS(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("release URL must use HTTPS without credentials")
	}
	return parsed, nil
}

func getBounded(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
	if _, err := requireHTTPS(rawURL); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP status %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response limit %d exceeded", limit)
	}
	return data, nil
}

func indexGitHubAssets(input []githubAsset) (map[string]githubAsset, error) {
	result := make(map[string]githubAsset, len(input))
	for _, asset := range input {
		if _, duplicate := result[asset.Name]; duplicate {
			return nil, fmt.Errorf("duplicate GitHub asset %q", asset.Name)
		}
		if _, err := requireHTTPS(asset.URL); err != nil {
			return nil, fmt.Errorf("asset %q: %w", asset.Name, err)
		}
		result[asset.Name] = asset
	}
	return result, nil
}

func decodeSingleJSON(data []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("trailing release metadata")
	}
	return nil
}

func matchingRegularFile(path string, asset Asset) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() != asset.Size {
		return false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, err
	}
	return hex.EncodeToString(hash.Sum(nil)) == asset.SHA256, nil
}
