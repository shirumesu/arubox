// Package launcher is the launcher module: a search panel that opens apps
// and files, calculates and runs commands.
package launcher

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/shirumesu/arubox/internal/hotkey"
	"github.com/shirumesu/arubox/internal/module"
	"github.com/shirumesu/arubox/internal/platform"
)

const id = "launcher"

// Store keeps the module's settings. config.Store satisfies it.
type Store interface {
	ModuleSettings(id string, v any) error
	SetModuleSettings(id string, v any) error
}

// Settings is what the user can change about the module.
type Settings struct {
	// Files searches files and folders by name as well as apps.
	Files bool `json:"files"`
	// Currency is the home currency amounts are converted into, as an ISO
	// code.
	Currency string `json:"currency"`
	// KeepMinutes is how long the hidden panel is kept ready before its
	// memory is released. 0 keeps it for good.
	KeepMinutes int `json:"keepMinutes"`
	// Keywords search websites: "gg cats" searches Google for cats.
	Keywords []Keyword `json:"keywords"`
	// DefaultSearch is the keyword offered last for any query, or empty
	// for none.
	DefaultSearch string `json:"defaultSearch"`
	// TranslateKeyword translates what follows it, whatever it looks like.
	// Empty turns it off.
	TranslateKeyword string `json:"translateKeyword"`
	// FilesKeyword searches only files and folders, even with Files off.
	// Empty turns it off.
	FilesKeyword string `json:"filesKeyword"`
	// FileExcludes are folders the Windows index leaves out: a full path,
	// or a name that leaves out every folder so named.
	FileExcludes []string `json:"fileExcludes"`
}

// stored is what the module keeps in the settings file: the settings and
// how often each app was opened, which ranks it higher.
type stored struct {
	Settings
	Launches map[string]int `json:"launches,omitempty"`
}

func defaultStored() stored {
	return stored{Settings: Settings{
		Files:            true,
		Currency:         "CNY",
		KeepMinutes:      30,
		Keywords:         slices.Clone(defaultKeywords),
		DefaultSearch:    "gg",
		TranslateKeyword: "fy",
		FilesKeyword:     "f",
		FileExcludes:     slices.Clone(defaultFileExcludes),
	}}
}

const (
	// iconSize is the size icons are drawn at, in points.
	iconSize = 32
	// rescanAfter is how old the app list may get before an open refreshes
	// it in the background.
	rescanAfter = time.Minute
	// fileCandidates is how many files are asked of the index before
	// ranking.
	fileCandidates = 400
	// maxIcons bounds the icon cache; files and folders can add many.
	maxIcons = 1000
	// maxFiles is how many files the files keyword lists.
	maxFiles = 20
)

// Module is the launcher module.
type Module struct {
	store      Store
	log        *slog.Logger
	rates      *rates
	translator Translator

	mu      sync.Mutex
	stored  stored
	panel   *panel // nil while disabled
	app     *application.App
	apps    []platform.App
	scanned time.Time
	icons   map[string]string // data URLs by app ID, extension or folder
	files   *fileIndex        // nil while disabled
}

// New creates the module. It does nothing until enabled. translator may be
// nil, which leaves translation out.
func New(store Store, log *slog.Logger, translator Translator) *Module {
	s := defaultStored()
	if err := store.ModuleSettings(id, &s); err != nil {
		log.Warn("launcher settings unreadable, using defaults", "error", err)
		s = defaultStored()
	}
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{Proxy: platform.SystemProxy, ForceAttemptHTTP2: true},
	}
	return &Module{store: store, log: log, stored: s, rates: &rates{client: client}, translator: translator}
}

// Info describes the module.
func (m *Module) Info() module.Info {
	return module.Info{
		ID:          id,
		Name:        "启动器",
		Description: "搜索并打开应用和文件，用关键词搜索网站，翻译文字，计算算式、换算汇率、查看世界时间，用 ! 开头运行命令。",
		Platforms:   []string{"darwin", "windows"},
	}
}

// Services binds the panel and the settings page.
func (m *Module) Services() []application.Service {
	return []application.Service{application.NewService(&Service{module: m})}
}

// Hotkeys declares the hotkey. Alt+Space is the window menu on Windows, so
// the default adds Shift there.
func (m *Module) Hotkeys() []hotkey.Def {
	open := "Alt+space"
	if runtime.GOOS == "windows" {
		open = "Alt+Shift+space"
	}
	return []hotkey.Def{{ID: "open", Label: "打开启动器", Default: open, Action: m.toggle}}
}

// Enable loads the panel and the app list in the background, so the first
// open is as fast as the others.
func (m *Module) Enable(ctx module.Context) error {
	files, err := newFileIndex(m.log, m.currentSettings().FileExcludes)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.files = files
	m.app = ctx.App
	m.panel = newPanel(ctx.App, m.keepTime, m.keepLocked())
	m.icons = map[string]string{}
	m.mu.Unlock()
	go m.scan()
	return nil
}

// Disable closes the panel, saves the file index and drops the app list
// and icons.
func (m *Module) Disable() error {
	m.mu.Lock()
	p, files := m.panel, m.files
	m.panel, m.app, m.apps, m.icons, m.scanned, m.files = nil, nil, nil, nil, time.Time{}, nil
	m.mu.Unlock()
	if files != nil {
		files.stop()
	}
	if p != nil {
		p.mu.Lock()
		p.destroy()
		p.mu.Unlock()
	}
	return nil
}

func (m *Module) keepTime() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.keepLocked()
}

// keepLocked is keepTime with m.mu held.
func (m *Module) keepLocked() time.Duration {
	return time.Duration(m.stored.KeepMinutes) * time.Minute
}

func (m *Module) currentPanel() *panel {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.panel
}

func (m *Module) toggle() {
	p := m.currentPanel()
	if p == nil {
		return
	}
	m.mu.Lock()
	stale := time.Since(m.scanned) > rescanAfter
	m.mu.Unlock()
	if stale {
		go m.scan()
	}
	p.toggle()
}

// scan reads the installed apps and draws their icons, so searching never
// waits for either.
func (m *Module) scan() {
	apps := platform.Applications()
	m.mu.Lock()
	if m.panel == nil {
		m.mu.Unlock()
		return
	}
	m.apps, m.scanned = apps, time.Now()
	m.mu.Unlock()
	for _, a := range apps {
		m.icon(a.ID, a.ID)
	}
}

// icon returns the icon for target as a data URL, drawing it once per key.
func (m *Module) icon(key, target string) string {
	m.mu.Lock()
	url, ok := m.icons[key]
	m.mu.Unlock()
	if ok {
		return url
	}
	if png := platform.Icon(target, iconSize); len(png) > 0 {
		url = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.icons != nil {
		if len(m.icons) >= maxIcons {
			m.icons = map[string]string{}
		}
		m.icons[key] = url
	}
	return url
}

// search returns the instant results for a query: a command, a calculation,
// the time in a city and the matching apps.
func (m *Module) search(query string) []Result {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	if command, ok := strings.CutPrefix(q, "!"); ok {
		if command = strings.TrimSpace(command); command == "" {
			return nil
		}
		return []Result{{Kind: kindCommand, Key: command, Title: command, Subtitle: "在终端中运行"}}
	}
	var results []Result
	if v, ok := calculate(q); ok {
		value := formatNumber(v)
		results = append(results, Result{Kind: kindCalc, Key: value, Title: value, Subtitle: "计算结果"})
	}
	if r, ok := worldTime(q, time.Now()); ok {
		results = append(results, r)
	}
	m.mu.Lock()
	apps, launches := m.apps, m.stored.Launches
	m.mu.Unlock()
	var matches []scored
	for _, a := range apps {
		if s := matchScore(q, a.Name, a.Alias); s > 0 {
			subtitle := "应用"
			if a.Alias != "" {
				subtitle = a.Alias
			}
			r := Result{Kind: kindApp, Key: a.ID, Title: a.Name, Subtitle: subtitle}
			matches = append(matches, scored{result: r, score: s + launchBoost(launches[a.ID])})
		}
	}
	for _, r := range best(matches, maxResults) {
		r.Icon = m.icon(r.Key, r.Key)
		results = append(results, r)
	}
	return results
}

// searchMore returns the results that take a while: a currency conversion,
// which may need the day's rates, and the matching files.
func (m *Module) searchMore(ctx context.Context, query string) []Result {
	q := strings.TrimSpace(query)
	m.mu.Lock()
	s := m.stored.Settings
	m.mu.Unlock()
	if strings.HasPrefix(q, "!") {
		return nil
	}
	var results []Result
	if c, ok := parseConversion(q); ok {
		perEuro, date, err := m.rates.get(ctx)
		if err != nil {
			results = append(results, Result{Kind: kindCurrency, Title: "无法获取汇率", Subtitle: err.Error()})
		} else {
			results = append(results, convert(c, perEuro, date, s.Currency)...)
		}
	}
	if s.Files && len([]rune(q)) >= 2 {
		results = append(results, m.searchFiles(ctx, q, maxResults)...)
	}
	return results
}

// searchFiles returns up to n files and folders whose name matches the
// query, recently opened ones first.
func (m *Module) searchFiles(ctx context.Context, q string, n int) []Result {
	q = strings.TrimSpace(q)
	m.mu.Lock()
	files := m.files
	m.mu.Unlock()
	if q == "" || files == nil {
		return nil
	}
	now := time.Now()
	found := files.search(ctx, q, fileCandidates)
	matches := make([]scored, 0, len(found))
	for i, f := range found {
		name := filepath.Base(f.Path)
		// The index has matched the query, which may use a search syntax
		// the name does not contain as text; its own order breaks ties.
		s := max(matchScore(q, name), 1) + recencyBoost(f.LastUsed, now)
		r := Result{Kind: kindFile, Key: f.Path, Title: name, Subtitle: displayDir(f.Path)}
		matches = append(matches, scored{result: r, score: s*fileCandidates + len(found) - i})
	}
	if ctx.Err() != nil {
		return nil
	}
	results := best(matches, n)
	for i := range results {
		r := &results[i]
		key := strings.ToLower(filepath.Ext(r.Key))
		if info, err := os.Stat(r.Key); err == nil && info.IsDir() {
			// Folders such as Downloads have icons of their own.
			r.Kind, key = kindFolder, r.Key
		}
		if key == "" {
			key = r.Key
		}
		r.Icon = m.icon(key, r.Key)
	}
	return results
}

// displayDir shows the folder a path is in, with the home folder as ~.
func displayDir(path string) string {
	dir := filepath.Dir(path)
	if home, err := os.UserHomeDir(); err == nil {
		if rest, ok := strings.CutPrefix(dir, home); ok {
			return "~" + rest
		}
	}
	return dir
}

// activate opens a result: launches the app, opens the file or folder (or
// shows it in Finder or File Explorer when reveal is set), copies the value
// or the translation (or opens the translation popup when reveal is set) or
// runs the command, or searches the web. The panel hides first.
func (m *Module) activate(r Result, reveal bool) error {
	if p := m.currentPanel(); p != nil {
		p.close()
	}
	switch r.Kind {
	case kindApp:
		m.countLaunch(r.Key)
		return platform.OpenApp(r.Key)
	case kindFile, kindFolder:
		if reveal {
			platform.RevealFile(r.Key)
			return nil
		}
		return platform.OpenFile(r.Key)
	case kindCalc, kindCurrency, kindTime:
		if r.Key == "" {
			return nil
		}
		return m.copy(r.Key)
	case kindTranslation:
		if r.Key == "" {
			return nil
		}
		if reveal {
			m.translator.Open(r.Key)
			return nil
		}
		return m.copy(r.Title)
	case kindCommand:
		return platform.RunInTerminal(r.Key)
	case kindWeb:
		return m.searchWeb(r.Key, r.Title)
	}
	return errors.New("未知的结果")
}

// searchWeb searches the website of a keyword for query in the default
// browser.
func (m *Module) searchWeb(keyword, query string) error {
	m.mu.Lock()
	app, keywords := m.app, m.stored.Keywords
	m.mu.Unlock()
	i := slices.IndexFunc(keywords, func(k Keyword) bool { return strings.EqualFold(k.Keyword, keyword) })
	if i < 0 || app == nil {
		return errors.New("关键词“" + keyword + "”已不存在")
	}
	return app.Browser.OpenURL(keywords[i].searchURL(query))
}

func (m *Module) copy(text string) error {
	m.mu.Lock()
	app := m.app
	m.mu.Unlock()
	if app == nil || !app.Clipboard.SetText(text) {
		return errors.New("复制失败")
	}
	return nil
}

// countLaunch remembers that an app was opened.
func (m *Module) countLaunch(appID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// A copy, so a failed save leaves the counts as they were.
	s := m.stored
	s.Launches = maps.Clone(m.stored.Launches)
	if s.Launches == nil {
		s.Launches = map[string]int{}
	}
	s.Launches[appID]++
	if err := m.store.SetModuleSettings(id, s); err != nil {
		m.log.Warn("save launch count", "error", err)
		return
	}
	m.stored = s
}

func (m *Module) currentSettings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stored.Settings
}

func (m *Module) setSettings(s Settings) error {
	if s.KeepMinutes < 0 {
		return errors.New("保留时间不能为负数")
	}
	if !slices.Contains(currencies, s.Currency) {
		return errors.New("不支持这种货币")
	}
	s.TranslateKeyword = strings.TrimSpace(s.TranslateKeyword)
	s.FilesKeyword = strings.TrimSpace(s.FilesKeyword)
	excludes := make([]string, 0, len(s.FileExcludes))
	for _, e := range s.FileExcludes {
		if e = strings.TrimSpace(e); e != "" && !slices.Contains(excludes, e) {
			excludes = append(excludes, e)
		}
	}
	s.FileExcludes = excludes
	s.Keywords = slices.Clone(s.Keywords)
	for i, k := range s.Keywords {
		s.Keywords[i] = Keyword{Keyword: strings.TrimSpace(k.Keyword), Name: strings.TrimSpace(k.Name), URL: strings.TrimSpace(k.URL)}
	}
	if err := checkKeywords(s); err != nil {
		return err
	}
	m.mu.Lock()
	next := m.stored
	next.Settings = s
	if err := m.store.SetModuleSettings(id, next); err != nil {
		m.mu.Unlock()
		return errors.New("保存设置失败：" + err.Error())
	}
	m.stored = next
	files := m.files
	m.mu.Unlock()
	if files != nil {
		files.setExcluded(s.FileExcludes)
	}
	return nil
}

// FileIndexStatus describes the file index for the settings page.
type FileIndexStatus struct {
	// State is "off" while the launcher is disabled and "spotlight" on
	// macOS. On Windows it is "missing", "stopped" or "outdated" when the
	// index service needs installing, then "connecting", "indexing" or
	// "ready".
	State string `json:"state"`
	// Files is how many files and folders are indexed.
	Files int `json:"files"`
	// Volumes are the indexed drives.
	Volumes []string `json:"volumes"`
	// Message explains a problem, such as a drive that cannot be read.
	Message string `json:"message,omitempty"`
}

func (m *Module) fileIndexStatus() FileIndexStatus {
	m.mu.Lock()
	files := m.files
	m.mu.Unlock()
	if files == nil {
		return FileIndexStatus{State: "off"}
	}
	return files.status()
}

// changeFileIndex installs the index service, or removes it.
func (m *Module) changeFileIndex(install bool) error {
	m.mu.Lock()
	files := m.files
	m.mu.Unlock()
	if files == nil {
		return errors.New("先启用启动器")
	}
	if install {
		return files.install()
	}
	return files.remove()
}
