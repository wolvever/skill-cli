package capability

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func searchIDs(caps []Capability) []string {
	ids := []string{}
	for _, c := range caps {
		ids = append(ids, c.ID)
	}
	return ids
}
func TestRankedDiscovery(t *testing.T) {
	caps := []Capability{
		{ID: "pets.getPetById", Description: "Find a pet by identifier"},
		{ID: "docs.searchGuide", Description: "Search documentation", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"frameworkVersion": map[string]any{"type": "string", "description": "framework release"}}}},
		{ID: "other.lookup", Description: "General reference"},
	}
	providers := map[string]ProviderConfig{"other": {Description: "Astronomy", Keywords: []string{"stargazing"}, SkillDescriptions: []string{"observations: Study distant galaxies"}}}
	index := NewSearchIndex(caps, providers)
	for query, want := range map[string]string{"getPetById": "pets.getPetById", "PETS.GETPETBYID": "pets.getPetById", "pet identifier": "pets.getPetById", "framework version": "docs.searchGuide", "astronomy": "other.lookup", "galaxies": "other.lookup", "stargazing": "other.lookup", "documantation": "docs.searchGuide"} {
		got := index.Search(query)
		if len(got) == 0 || got[0].ID != want {
			t.Fatalf("%s: %v", query, searchIDs(got))
		}
	}
	if !reflect.DeepEqual(searchIDs(index.Search("pet identifier")), searchIDs(index.Search("identifier pet"))) {
		t.Fatal("query order changes ranking")
	}
	for _, query := range []string{"the and this", "zzzzzzzq"} {
		if len(index.Search(query)) != 0 {
			t.Fatalf("unrelated query matched: %s", query)
		}
	}
	if got := searchTokens("HTTPServer search_issues getPetById"); !reflect.DeepEqual(got, []string{"http", "server", "search", "issue", "get", "pet", "id"}) {
		t.Fatal(got)
	}
	if got := searchTokens("文档 检索"); len(got) != 2 {
		t.Fatal(got)
	}
	empty := NewSearchIndex(nil, nil)
	if len(empty.Search("docs")) != 0 {
		t.Fatal("empty index matched")
	}
}

type rankedFixture struct{}

func (*rankedFixture) Discover(context.Context) ([]Capability, error) {
	return []Capability{{ID: "allowed", Description: strings.Repeat("document ", 100), InputSchema: map[string]any{"type": "object"}, Annotations: map[string]any{"readOnlyHint": true}}, {ID: "hidden", Description: "confidential astronomy"}}, nil
}
func (*rankedFixture) Invoke(context.Context, string, map[string]any) (any, error) { return nil, nil }
func (*rankedFixture) Close() error                                                { return nil }
func TestSearchPolicyAndCompactListing(t *testing.T) {
	r := New(Config{Providers: map[string]ProviderConfig{"test": {Type: "http", Allow: []string{"allowed"}, Description: "Document reference"}}}, 1<<20)
	r.providers["test"] = &rankedFixture{}
	defer r.Close()
	for _, full := range []bool{false, true} {
		data, err := r.Execute(context.Background(), Request{Action: "list", Target: "test", Full: full})
		if err != nil {
			t.Fatal(err)
		}
		caps := data.([]Capability)
		if len(caps) != 1 || caps[0].InputSchema != nil {
			t.Fatal(caps)
		}
		if full && (len(caps[0].Description) < 500 || caps[0].Annotations == nil) {
			t.Fatal("full metadata missing")
		}
		if !full && (len([]rune(caps[0].Description)) > 180 || caps[0].Annotations != nil) {
			t.Fatal("compact metadata unbounded")
		}
	}
	data, err := r.Execute(context.Background(), Request{Action: "search", Query: "astronomy"})
	if err != nil || len(data.([]Capability)) != 0 {
		t.Fatal(data, err)
	}
	if _, err := r.Execute(context.Background(), Request{Action: "search", Match: "semantic"}); err == nil {
		t.Fatal("invalid mode accepted")
	}
	data, err = r.Execute(context.Background(), Request{Action: "describe", Target: "test.allowed"})
	if err != nil || data.(Capability).InputSchema == nil {
		t.Fatal(data, err)
	}
	data, err = r.Execute(context.Background(), Request{Action: "list"})
	if err != nil || data.([]map[string]string)[0]["description"] != "Document reference" {
		t.Fatal(data, err)
	}
}
func TestSkillFrontmatterMetadata(t *testing.T) {
	dir := t.TempDir()
	skill := filepath.Join(dir, "SKILL.md")
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(skill, []byte("---\nname: astronomy\ndescription: >-\n  Study galaxies and\n  distant stars.\nmetadata:\n  short-description: extra accepted\n---\nSECRET BODY NEVER INDEXED"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(`{"providers":{"test":{"type":"http","url":"https://example.com","skill_files":["SKILL.md"]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Providers["test"].SkillDescriptions
	if len(got) != 1 || strings.Contains(got[0], "SECRET") || !strings.Contains(got[0], "distant stars") {
		t.Fatal(got)
	}
	for _, text := range []string{"No frontmatter", "---\nname: missing\n---", "---\nname: oversized\ndescription: " + strings.Repeat("x", 65536) + "\n---"} {
		if err := os.WriteFile(skill, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(config); err == nil {
			t.Fatal("bad skill accepted")
		}
	}
}
func BenchmarkRankedIndex(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			caps := make([]Capability, n)
			for i := range caps {
				caps[i] = Capability{ID: fmt.Sprintf("docs.searchArticle%d", i), Description: "Search official framework documentation and API reference"}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				NewSearchIndex(caps, nil).Search("framework documentation")
			}
		})
	}
}
