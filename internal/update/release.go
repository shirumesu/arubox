package update

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// repository is where releases are published.
const repository = "shirumesu/arubox"

// manifestKey is the public half of the P-256 key release CI signs every
// manifest with, as an uncompressed point.
const manifestKey = "0419548d9766f53f8f3635c9da69dcf9b1d295fda4adb2f78140ad91f4e91d6249" +
	"25899b4d39bdebce84cd3d82c3cfc03c8ca28ad6a6b1fd2e8e5e5d8fa13aec55"

// maxFeed and maxManifest bound what is read of the release feed, which
// carries every listed release's notes, and of a manifest or its
// signature.
const (
	maxFeed     = 8 << 20
	maxManifest = 1 << 20
)

// manifest is manifest.json of a release, made by scripts/package.sh.
type manifest struct {
	Version string `json:"version"`
	// Notes is CHANGELOG.md of the release without its heading.
	Notes     string                 `json:"notes"`
	Platforms map[string]packageFile `json:"platforms"`
}

// packageFile is the update package of one platform. URLs are places to
// download the same file from.
type packageFile struct {
	URLs   []string `json:"urls"`
	SHA256 string   `json:"sha256"`
	Size   int64    `json:"size"`
}

// Release is a newer version on offer.
type Release struct {
	Version string `json:"version"`
	// Notes is what changed since the installed version, in the Markdown
	// of CHANGELOG.md.
	Notes string `json:"notes"`
}

// offer is a release and its package for this platform.
type offer struct {
	Release
	file packageFile
}

// feed is the part of GitHub's release feed that is read.
type feed struct {
	Entries []struct {
		Links []struct {
			Rel  string `xml:"rel,attr"`
			Href string `xml:"href,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

// newest finds the newest published release after installed, prereleases
// only when asked for. It reads GitHub's public release feed and release
// files rather than the REST API, which allows 60 unauthenticated requests
// an hour per address and runs out on shared proxy exits. Each release
// carries manifest.json, signed in manifest.json.sig and naming every
// package with its digest; a package is trusted only through that
// signature. It returns nil when there is nothing newer.
func newest(ctx context.Context, client *http.Client, installed version, prereleases bool) (*offer, error) {
	raw, err := get(ctx, client, "https://github.com/"+repository+"/releases.atom", maxFeed)
	if err != nil {
		return nil, err
	}
	var f feed
	if err := xml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("发行版列表无法解析：%w", err)
	}
	// The feed lists the latest published releases; drafts are not public.
	tagPrefix := "/" + repository + "/releases/tag/v"
	var latest *version
	for _, e := range f.Entries {
		for _, l := range e.Links {
			u, err := url.Parse(l.Href)
			if l.Rel != "alternate" || err != nil {
				continue
			}
			text, ok := strings.CutPrefix(u.Path, tagPrefix)
			if !ok {
				continue
			}
			// A tag that is no version, such as one made by hand, is no
			// release of the program.
			v, err := parseVersion(text)
			if err != nil || v.compare(installed) <= 0 || (v.prerelease() && !prereleases) {
				continue
			}
			if latest == nil || v.compare(*latest) > 0 {
				latest = &v
			}
		}
	}
	if latest == nil {
		return nil, nil
	}

	tag := "v" + latest.String()
	base := "https://github.com/" + repository + "/releases/download/" + tag + "/"
	body, err := get(ctx, client, base+"manifest.json", maxManifest)
	if err != nil {
		return nil, err
	}
	signature, err := get(ctx, client, base+"manifest.json.sig", maxManifest)
	if err != nil {
		return nil, err
	}
	m, err := verifyManifest(body, signature)
	if err != nil {
		return nil, fmt.Errorf("发行版 %s：%w", tag, err)
	}
	if m.Version != latest.String() {
		return nil, fmt.Errorf("发行版 %s 的更新清单属于另一个版本 %s", tag, m.Version)
	}
	file, ok := m.Platforms[platformKey()]
	if !ok || len(file.URLs) == 0 || file.Size <= 0 {
		return nil, fmt.Errorf("发行版 %s 没有适用于本系统的更新包", tag)
	}
	if _, err := hex.DecodeString(file.SHA256); err != nil || len(file.SHA256) != sha256.Size*2 {
		return nil, fmt.Errorf("发行版 %s 的更新清单中校验值无效", tag)
	}
	return &offer{
		Release: Release{Version: latest.String(), Notes: unseenNotes(m.Notes, installed, *latest)},
		file:    file,
	}, nil
}

// verifyManifest checks the signature over a manifest and decodes it.
// openssl dgst -sha256 -sign writes the signature as a DER sequence of r
// and s.
func verifyManifest(body, signature []byte) (*manifest, error) {
	point, err := hex.DecodeString(manifestKey)
	if err != nil {
		return nil, err
	}
	key, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(body)
	if !ecdsa.VerifyASN1(key, digest[:], signature) {
		return nil, errors.New("更新清单的签名无效")
	}
	var m manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("更新清单无法解析：%w", err)
	}
	return &m, nil
}

var sectionHeading = regexp.MustCompile(`^## (\S+)\s*$`)

// unseenNotes is the part of latest's notes that installed lacks. A
// prerelease's notes have a "## <version>" section for each prerelease of
// its cycle, newest first; those up to installed are dropped. A stable
// release sums up its prereleases and ships the last one unchanged, so a
// copy running one of them has it all.
func unseenNotes(notes string, installed, latest version) string {
	if installed.prerelease() && !latest.prerelease() && installed.core == latest.core {
		return fmt.Sprintf("%s 是正式版，内容与正在使用的 %s 相同。", latest, installed)
	}
	var sections []string
	for line := range strings.Lines(notes) {
		if len(sections) == 0 || strings.HasPrefix(line, "## ") {
			sections = append(sections, "")
		}
		sections[len(sections)-1] += line
	}
	var kept []string
	for _, section := range sections {
		heading, _, _ := strings.Cut(section, "\n")
		if m := sectionHeading.FindStringSubmatch(heading); m != nil {
			if v, err := parseVersion(m[1]); err == nil && v.compare(installed) <= 0 {
				continue
			}
		}
		kept = append(kept, section)
	}
	return strings.TrimSpace(strings.Join(kept, ""))
}

// get reads a whole response of at most limit bytes.
func get(ctx context.Context, client *http.Client, address string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 %s：%w", req.URL.Host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s 返回 %s", address, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败：%w", address, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s 过大", address)
	}
	return body, nil
}
