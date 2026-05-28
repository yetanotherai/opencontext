package registry

import "testing"

func TestAllCollectorsExposeLanguageNeutralKindsAndSchemas(t *testing.T) {
	collectors := AllCollectors()
	byName := map[string]CollectorManifest{}
	for _, c := range collectors {
		byName[c.Name] = c
		if c.Kind == "" {
			t.Fatalf("collector %s has empty kind", c.Name)
		}
		if len(c.Install) == 0 {
			t.Fatalf("collector %s has no install command", c.Name)
		}
		if len(c.Sources) == 0 {
			t.Fatalf("collector %s has no sources", c.Name)
		}
	}

	for _, name := range []string{"claude", "codex", "cursor", "opencode"} {
		c, ok := byName[name]
		if !ok {
			t.Fatalf("missing bundled hook collector %s", name)
		}
		if c.Kind != KindBundledHook {
			t.Fatalf("expected %s to be %s, got %s", name, KindBundledHook, c.Kind)
		}
		if len(c.Schemas) == 0 {
			t.Fatalf("expected %s to expose schemas", name)
		}
	}

	for _, name := range []string{"macos", "windows"} {
		c, ok := byName[name]
		if !ok {
			t.Fatalf("missing external collector %s", name)
		}
		if c.Kind != KindExternal {
			t.Fatalf("expected %s to be external, got %s", name, c.Kind)
		}
	}
}
