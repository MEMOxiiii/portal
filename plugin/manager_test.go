package plugin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/paroxity/portal"
	"github.com/paroxity/portal/event"
)

func TestManagerRunsLifecycleInDependencyOrder(t *testing.T) {
	rec := &recorder{}
	m := newTestManager(t, nil,
		testPlugin{manifest: Manifest{Name: "top", Depends: []string{"middle"}}, rec: rec},
		testPlugin{manifest: Manifest{Name: "middle", Depends: []string{"base"}}, rec: rec},
		testPlugin{manifest: Manifest{Name: "base"}, rec: rec},
	)

	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	m.Enable()
	m.Disable()

	want := []string{
		"load:base", "load:middle", "load:top",
		"enable:base", "enable:middle", "enable:top",
		"disable:top", "disable:middle", "disable:base",
	}
	if got := rec.calls(); !equal(got, want) {
		t.Errorf("lifecycle order:\n got %v\nwant %v", got, want)
	}
}

func TestManagerSkipsPluginWithMissingDependency(t *testing.T) {
	rec := &recorder{}
	m := newTestManager(t, nil,
		testPlugin{manifest: Manifest{Name: "dependent", Depends: []string{"absent"}}, rec: rec},
		testPlugin{manifest: Manifest{Name: "standalone"}, rec: rec},
	)

	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	m.Enable()

	if _, ok := m.Plugin("dependent"); ok {
		t.Error("plugin with a missing dependency was loaded")
	}
	if !m.Enabled("standalone") {
		t.Error("standalone plugin should still have been enabled")
	}
	if got := rec.calls(); !equal(got, []string{"load:standalone", "enable:standalone"}) {
		t.Errorf("unexpected lifecycle calls: %v", got)
	}
}

func TestManagerSkipsDependentsOfFailedPlugin(t *testing.T) {
	rec := &recorder{}
	m := newTestManager(t, nil,
		testPlugin{manifest: Manifest{Name: "broken"}, rec: rec, onLoad: func(*Context) error {
			return errors.New("boom")
		}},
		testPlugin{manifest: Manifest{Name: "direct", Depends: []string{"broken"}}, rec: rec},
		testPlugin{manifest: Manifest{Name: "indirect", Depends: []string{"direct"}}, rec: rec},
	)

	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, name := range []string{"broken", "direct", "indirect"} {
		if _, ok := m.Plugin(name); ok {
			t.Errorf("plugin %q should not have loaded", name)
		}
	}
}

func TestManagerSkipsDependencyCycle(t *testing.T) {
	rec := &recorder{}
	m := newTestManager(t, nil,
		testPlugin{manifest: Manifest{Name: "a", Depends: []string{"b"}}, rec: rec},
		testPlugin{manifest: Manifest{Name: "b", Depends: []string{"a"}}, rec: rec},
		testPlugin{manifest: Manifest{Name: "free"}, rec: rec},
	)

	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := rec.calls(); !equal(got, []string{"load:free"}) {
		t.Errorf("expected only the acyclic plugin to load, got calls %v", got)
	}
	for _, name := range []string{"a", "b"} {
		if _, ok := m.Plugin(name); ok {
			t.Errorf("plugin %q in a cycle should not have loaded", name)
		}
	}
}

func TestManagerHonoursDisabledList(t *testing.T) {
	rec := &recorder{}
	m := newTestManager(t, []string{"unwanted"},
		testPlugin{manifest: Manifest{Name: "unwanted"}, rec: rec},
		testPlugin{manifest: Manifest{Name: "wanted"}, rec: rec},
	)

	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := m.Plugin("unwanted"); ok {
		t.Error("a plugin named in the disabled list was loaded")
	}
	if _, ok := m.Plugin("wanted"); !ok {
		t.Error("wanted plugin was not loaded")
	}
}

func TestManagerRecoversPanickingPlugin(t *testing.T) {
	rec := &recorder{}
	m := newTestManager(t, nil,
		testPlugin{manifest: Manifest{Name: "panicky"}, rec: rec, onEnable: func(*Context) error {
			panic("plugin exploded")
		}},
		testPlugin{manifest: Manifest{Name: "sane"}, rec: rec},
	)

	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	m.Enable()

	if m.Enabled("panicky") {
		t.Error("a plugin that panicked during Enable should not be marked enabled")
	}
	if !m.Enabled("sane") {
		t.Error("a panic in one plugin should not prevent others from enabling")
	}
}

func TestManagerReleasesSubscriptionsOnDisable(t *testing.T) {
	var got int
	rec := &recorder{}
	m := newTestManager(t, nil, testPlugin{
		manifest: Manifest{Name: "subscriber"},
		rec:      rec,
		onEnable: func(ctx *Context) error {
			ctx.Subscribe(event.TopicPlayerJoin, func(any) { got++ })
			return nil
		},
	})

	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	m.Enable()

	bus := m.portal.Events()
	bus.Publish(event.TopicPlayerJoin, event.PlayerPayload{Name: "before"})
	if got != 1 {
		t.Fatalf("handler called %d times while enabled, want 1", got)
	}

	m.Disable()
	bus.Publish(event.TopicPlayerJoin, event.PlayerPayload{Name: "after"})
	if got != 1 {
		t.Errorf("handler called %d times after Disable, want it to stay at 1", got)
	}
}

func TestManagerReleasesSubscriptionsOfPluginThatFailsToEnable(t *testing.T) {
	var got int
	rec := &recorder{}
	m := newTestManager(t, nil, testPlugin{
		manifest: Manifest{Name: "halfway"},
		rec:      rec,
		onEnable: func(ctx *Context) error {
			ctx.Subscribe(event.TopicPlayerJoin, func(any) { got++ })
			return errors.New("gave up after subscribing")
		},
	})

	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	m.Enable()

	m.portal.Events().Publish(event.TopicPlayerJoin, event.PlayerPayload{Name: "x"})
	if got != 0 {
		t.Errorf("handler of a plugin that failed to enable was called %d times, want 0", got)
	}
}

func TestManagerSurvivesPanickingEventHandler(t *testing.T) {
	rec := &recorder{}
	m := newTestManager(t, nil, testPlugin{
		manifest: Manifest{Name: "panicky"},
		rec:      rec,
		onEnable: func(ctx *Context) error {
			ctx.Subscribe(event.TopicPlayerJoin, func(any) { panic("handler exploded") })
			return nil
		},
	})

	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	m.Enable()
	m.portal.Events().Publish(event.TopicPlayerJoin, event.PlayerPayload{Name: "x"})
}

func TestManagerLoadIsNotRepeatable(t *testing.T) {
	m := newTestManager(t, nil, testPlugin{manifest: Manifest{Name: "only"}, rec: &recorder{}})

	if err := m.Load(); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if err := m.Load(); err == nil {
		t.Error("second Load should have returned an error")
	}
}

func TestManagerPluginReachableFromDependent(t *testing.T) {
	// A plugin reaching its dependency through the Context during Load is the case that deadlocks if the
	// Manager holds its state lock across plugin callbacks.
	var found bool
	rec := &recorder{}
	m := newTestManager(t, nil,
		testPlugin{manifest: Manifest{Name: "api"}, rec: rec},
		testPlugin{manifest: Manifest{Name: "consumer", Depends: []string{"api"}}, rec: rec, onLoad: func(ctx *Context) error {
			_, found = ctx.Plugin("api")
			return nil
		}},
	)

	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Error("a plugin could not reach its dependency through the Context during Load")
	}
}

func TestContextConfigWritesDefaultsThenReadsThem(t *testing.T) {
	type pluginConfig struct {
		Message string `json:"message"`
		Limit   int    `json:"limit"`
	}

	dir := t.TempDir()
	m := NewManager(portal.New(portal.Options{Logger: &testLogger{t: t}}), dir, &testLogger{t: t}, nil)
	ctx := newContext(m, Manifest{Name: "configured"})

	first := pluginConfig{Message: "hello", Limit: 5}
	if err := ctx.Config(&first); err != nil {
		t.Fatalf("first Config: %v", err)
	}

	path := filepath.Join(dir, "configured", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written config: %v", err)
	}
	var written pluginConfig
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("decoding written config: %v", err)
	}
	if written != first {
		t.Errorf("written defaults = %+v, want %+v", written, first)
	}

	if err := os.WriteFile(path, []byte(`{"message":"edited","limit":9}`), 0o644); err != nil {
		t.Fatalf("editing config: %v", err)
	}
	second := pluginConfig{Message: "hello", Limit: 5}
	if err := ctx.Config(&second); err != nil {
		t.Fatalf("second Config: %v", err)
	}
	if want := (pluginConfig{Message: "edited", Limit: 9}); second != want {
		t.Errorf("read config = %+v, want %+v", second, want)
	}
}

func TestManifestValidate(t *testing.T) {
	for _, test := range []struct {
		name     string
		manifest Manifest
		wantErr  bool
	}{
		{name: "simple", manifest: Manifest{Name: "hello"}},
		{name: "punctuation", manifest: Manifest{Name: "my_plugin-2"}},
		{name: "empty", manifest: Manifest{}, wantErr: true},
		{name: "path separator", manifest: Manifest{Name: "a/b"}, wantErr: true},
		{name: "traversal", manifest: Manifest{Name: ".."}, wantErr: true},
		{name: "format verb", manifest: Manifest{Name: "%s"}, wantErr: true},
		{name: "self dependency", manifest: Manifest{Name: "loop", Depends: []string{"loop"}}, wantErr: true},
		{name: "too long", manifest: Manifest{Name: strings.Repeat("a", 65)}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.manifest.Validate(); (err != nil) != test.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestResolveOrderPlacesSoftDependenciesFirst(t *testing.T) {
	available := map[string]Plugin{
		"late":      testPlugin{manifest: Manifest{Name: "late", SoftDepends: []string{"early", "absent"}}},
		"early":     testPlugin{manifest: Manifest{Name: "early"}},
		"unrelated": testPlugin{manifest: Manifest{Name: "unrelated"}},
	}

	order, skipped := resolveOrder(available)
	if len(skipped) != 0 {
		t.Fatalf("a missing soft dependency should not skip a plugin, got %v", skipped)
	}
	if indexOf(order, "early") > indexOf(order, "late") {
		t.Errorf("soft dependency was not ordered first: %v", order)
	}
	if len(order) != 3 {
		t.Errorf("order = %v, want all three plugins", order)
	}
}

func TestResolveOrderIsDeterministic(t *testing.T) {
	available := map[string]Plugin{
		"c": testPlugin{manifest: Manifest{Name: "c"}},
		"a": testPlugin{manifest: Manifest{Name: "a"}},
		"b": testPlugin{manifest: Manifest{Name: "b"}},
	}

	first, _ := resolveOrder(available)
	for i := 0; i < 20; i++ {
		next, _ := resolveOrder(available)
		if !equal(first, next) {
			t.Fatalf("resolveOrder is not deterministic: %v then %v", first, next)
		}
	}
	if !equal(first, []string{"a", "b", "c"}) {
		t.Errorf("unrelated plugins should load in name order, got %v", first)
	}
}

// newTestManager creates a Manager over the given plugins, bypassing the process-wide registry.
func newTestManager(t *testing.T, disabled []string, plugins ...Plugin) *Manager {
	t.Helper()

	log := &testLogger{t: t}
	m := NewManager(portal.New(portal.Options{Logger: log}), t.TempDir(), log, disabled)
	m.source = func() []Plugin { return plugins }
	return m
}

// testPlugin is a Plugin that records the lifecycle methods called on it and optionally runs a hook for
// each of them.
type testPlugin struct {
	manifest  Manifest
	rec       *recorder
	onLoad    func(*Context) error
	onEnable  func(*Context) error
	onDisable func() error
}

func (p testPlugin) Manifest() Manifest { return p.manifest }

func (p testPlugin) Load(ctx *Context) error {
	p.rec.add("load:" + p.manifest.Name)
	if p.onLoad != nil {
		return p.onLoad(ctx)
	}
	return nil
}

func (p testPlugin) Enable(ctx *Context) error {
	p.rec.add("enable:" + p.manifest.Name)
	if p.onEnable != nil {
		return p.onEnable(ctx)
	}
	return nil
}

func (p testPlugin) Disable() error {
	p.rec.add("disable:" + p.manifest.Name)
	if p.onDisable != nil {
		return p.onDisable()
	}
	return nil
}

// recorder collects the lifecycle calls made across a set of test plugins.
type recorder struct {
	mu  sync.Mutex
	seq []string
}

func (r *recorder) add(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq = append(r.seq, call)
}

func (r *recorder) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seq...)
}

// testLogger routes the proxy's log output into the test's own output.
type testLogger struct{ t *testing.T }

func (l *testLogger) Debugf(format string, v ...any) { l.t.Logf("DEBUG "+format, v...) }
func (l *testLogger) Infof(format string, v ...any)  { l.t.Logf("INFO  "+format, v...) }
func (l *testLogger) Errorf(format string, v ...any) { l.t.Logf("ERROR "+format, v...) }
func (l *testLogger) Fatalf(format string, v ...any) { l.t.Fatalf(format, v...) }

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func indexOf(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
}
