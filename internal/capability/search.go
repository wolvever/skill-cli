package capability

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
	"unicode"
)

// Ranked retrieval is deliberately lexical: no model, API, database, download,
// training corpus, or persistent home-directory index is required.
type searchDoc struct {
	cap    Capability
	terms  map[string]float64
	length float64
	words  []string
}
type SearchIndex struct {
	docs      []searchDoc
	df        map[string]int
	avgLength float64
}

var searchStop = wordSet("a an the and or of to in for from with by on at as is are be it this that these those i we you your me my our can could would should will do does how what which who when where select selected selection tool tools operation operations capability capabilities please need want return empty if none unavailable available only both one not")

func wordSet(text string) map[string]bool {
	out := map[string]bool{}
	for _, word := range strings.Fields(text) {
		out[word] = true
	}
	return out
}

func searchTokens(text string) []string {
	runes := []rune(text)
	var separated strings.Builder
	for i, c := range runes {
		if i > 0 && unicode.IsUpper(c) && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]) || (unicode.IsUpper(runes[i-1]) && i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
			separated.WriteByte(' ')
		}
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			separated.WriteRune(unicode.ToLower(c))
		} else {
			separated.WriteByte(' ')
		}
	}
	out := []string{}
	for _, word := range strings.Fields(separated.String()) {
		if searchStop[word] {
			continue
		}
		// A conservative inflection fold. Keep identifiers intact for exact matches.
		if len(word) > 4 && strings.HasSuffix(word, "ies") {
			word = strings.TrimSuffix(word, "ies") + "y"
		} else if len(word) > 3 && strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss") {
			word = strings.TrimSuffix(word, "s")
		}
		out = append(out, word)
	}
	return out
}

func schemaSearchText(schema any) string {
	b, err := json.Marshal(schema)
	if err != nil || len(b) > 1<<20 {
		return ""
	}
	var value any
	if json.Unmarshal(b, &value) != nil {
		return ""
	}
	var text strings.Builder
	remaining := 128
	var visit func(any, int)
	visit = func(raw any, depth int) {
		if depth > 4 || remaining <= 0 {
			return
		}
		m, ok := raw.(map[string]any)
		if !ok {
			return
		}
		if desc, ok := m["description"].(string); ok {
			r := []rune(desc)
			if len(r) > 256 {
				r = r[:256]
			}
			text.WriteString(string(r))
			text.WriteByte(' ')
		}
		props, _ := m["properties"].(map[string]any)
		keys := []string{}
		for key := range props {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if remaining <= 0 {
				break
			}
			remaining--
			text.WriteString(key)
			text.WriteByte(' ')
			visit(props[key], depth+1)
		}
	}
	visit(value, 0)
	return text.String()
}

func NewSearchIndex(caps []Capability, providers map[string]ProviderConfig) *SearchIndex {
	index := &SearchIndex{df: map[string]int{}}
	for _, cap := range caps {
		provider, _, _ := strings.Cut(cap.ID, ".")
		cfg := providers[provider]
		doc := searchDoc{cap: cap, terms: map[string]float64{}}
		fields := []struct {
			text   string
			weight float64
		}{{cap.ID, 4}, {cap.Description, 1}, {cfg.Description, .6}, {strings.Join(cfg.Keywords, " "), .8}, {strings.Join(cfg.SkillDescriptions, " "), .8}, {schemaSearchText(cap.InputSchema), .35}}
		for _, field := range fields {
			for _, word := range searchTokens(field.text) {
				doc.terms[word] += field.weight
				doc.length += field.weight
			}
		}
		for word := range doc.terms {
			index.df[word]++
			doc.words = append(doc.words, word)
		}
		sort.Strings(doc.words)
		index.avgLength += doc.length
		index.docs = append(index.docs, doc)
	}
	if len(index.docs) > 0 {
		index.avgLength /= float64(len(index.docs))
	}
	return index
}

// Search preserves exact IDs as the strongest evidence. Scores are ranking
// values, not probabilities; fuzzy fallback is used only with no lexical hits.
func (index *SearchIndex) Search(query string) []Capability {
	q := strings.TrimSpace(strings.ToLower(query))
	out := []Capability{}
	if q == "" {
		for _, doc := range index.docs {
			out = append(out, doc.cap)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out
	}
	for _, doc := range index.docs {
		_, op, _ := strings.Cut(doc.cap.ID, ".")
		if strings.EqualFold(doc.cap.ID, q) || strings.EqualFold(op, q) {
			out = append(out, doc.cap)
		}
	}
	if len(out) > 0 {
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out
	}
	terms := wordSet(strings.Join(searchTokens(query), " "))
	if len(terms) == 0 {
		return out
	}
	queryTerms := []string{}
	for word := range terms {
		queryTerms = append(queryTerms, word)
	}
	sort.Strings(queryTerms)
	type hit struct {
		cap   Capability
		score float64
	}
	hits := []hit{}
	for _, doc := range index.docs {
		score := 0.
		for _, word := range queryTerms {
			tf := doc.terms[word]
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (float64(len(index.docs)-index.df[word])+.5)/(float64(index.df[word])+.5))
			score += idf * (tf * 2.2) / (tf + 1.2*(.25+.75*doc.length/index.avgLength))
		}
		if score > 0 {
			hits = append(hits, hit{doc.cap, score})
		}
	}
	if len(hits) == 0 {
		for _, doc := range index.docs {
			score := 0.
			for _, word := range queryTerms {
				if len([]rune(word)) < 4 {
					continue
				}
				best := 0.
				for _, candidate := range doc.words {
					similarity := trigramSimilarity(word, candidate)
					if similarity > best {
						best = similarity
					}
				}
				if best >= .5 {
					score += best
				}
			}
			if score > 0 {
				hits = append(hits, hit{doc.cap, score})
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if math.Abs(hits[i].score-hits[j].score) < 1e-12 {
			return hits[i].cap.ID < hits[j].cap.ID
		}
		return hits[i].score > hits[j].score
	})
	for _, hit := range hits {
		out = append(out, hit.cap)
	}
	return out
}
func trigramSimilarity(a, b string) float64 {
	grams := func(text string) map[string]bool {
		r := []rune("^" + text + "$")
		out := map[string]bool{}
		for i := 0; i+3 <= len(r); i++ {
			out[string(r[i:i+3])] = true
		}
		return out
	}
	left, right := grams(a), grams(b)
	intersection := 0
	for gram := range left {
		if right[gram] {
			intersection++
		}
	}
	union := len(left) + len(right) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func compactCapability(cap Capability, full bool) Capability {
	cap.InputSchema = nil
	cap.OutputSchema = nil
	if !full {
		cap.Description = compactDescription(cap.Description, 180)
		cap.Annotations = nil
	}
	return cap
}
func compactDescription(text string, maxRunes int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	cut := maxRunes - 1
	for i := cut; i > maxRunes/2; i-- {
		if runes[i] == ' ' {
			cut = i
			break
		}
	}
	return string(runes[:cut]) + "…"
}
