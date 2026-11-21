package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestValidateRejectsInvalidConfig pins the shape checks that protect
// Forward from malformed -weights files. Before these checks, n_head=0
// divided by zero and a missing final LayerNorm ran off the end of a slice.
func TestValidateRejectsInvalidConfig(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Weights)
	}{
		{"n_head zero", func(w *Weights) { w.Config.NHead = 0 }},
		{"n_head not a divisor of d_model", func(w *Weights) { w.Config.NHead = 5 }},
		{"vocab is not byte-sized", func(w *Weights) { w.Config.Vocab = 128 }},
		{"d_model zero", func(w *Weights) { w.Config.DModel = 0 }},
		{"d_model negative", func(w *Weights) { w.Config.DModel = -96 }},
		{"n_layer zero", func(w *Weights) { w.Config.NLayer = 0 }},
		{"d_ff zero", func(w *Weights) { w.Config.DFF = 0 }},
		{"ctx zero", func(w *Weights) { w.Config.Ctx = 0 }},
		{"lnf_g missing", func(w *Weights) { w.LNFG = nil }},
		{"lnf_b short", func(w *Weights) { w.LNFB = w.LNFB[:len(w.LNFB)-1] }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := Random(DefaultConfig, 1)
			tc.mutate(w)
			if err := w.validate(); err == nil {
				t.Fatal("validate accepted malformed weights")
			}
		})
	}
}

// TestParseRejectsEmptyConfig covers a JSON file with a config object that
// omits every shape field; it must fail before Forward can divide by zero.
func TestParseRejectsEmptyConfig(t *testing.T) {
	if _, err := Parse([]byte(`{"config":{"vocab":256}}`), "test"); err == nil {
		t.Fatal("Parse accepted a weights file with no model shape")
	}
}

// TestParseErrorsNameTheActualProblem checks how a bad weights file reads, not
// just that it is rejected. main.go calls log.Fatalf on a -weights load error, so
// this string is the only thing the reader ever sees of the failure.
func TestParseErrorsNameTheActualProblem(t *testing.T) {
	cases := []struct {
		name string
		body string
		// want appears in the message
		want []string
		// wantNot must not appear: these were the old, misleading phrasings
		wantNot []string
	}{
		{
			name:    "empty file",
			body:    "",
			want:    []string{"empty", "weights.json"},
			wantNot: []string{"unexpected end"},
		},
		{
			name:    "truncated json",
			body:    `{"config":`,
			want:    []string{"not valid JSON", "byte 10"},
			wantNot: []string{"empty"},
		},
		{
			name: "no recognisable keys at all",
			body: `{"my_config":{"vocab":256},"my_tok_embed":[]}`,
			want: []string{"no configuration", "config", "tok_embed"},
			// "vocab is 0" reads as a wrong value when the whole schema is wrong.
			wantNot: []string{"vocab is 0"},
		},
		{
			name: "complete config, no tensors",
			body: `{"config":{"vocab":256,"d_model":96,"n_layer":2,"n_head":4,"d_ff":384,"ctx":64}}`,
			// The largest missing tensor is named first, with its expected size.
			want:    []string{"tok_embed", "24576", "vocab x d_model"},
			wantNot: []string{"lnf_g"},
		},
		{
			name:    "wrong vocab",
			body:    `{"config":{"vocab":100,"d_model":96,"n_layer":2,"n_head":4,"d_ff":384,"ctx":64}}`,
			want:    []string{"vocab is 100", "want 256"},
			wantNot: []string{"empty"},
		},
	}
	dir := t.TempDir()
	for _, c := range cases {
		p := filepath.Join(dir, "weights.json")
		if err := os.WriteFile(p, []byte(c.body), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Load(p)
		if err == nil {
			t.Fatalf("%s: accepted a file it should reject", c.name)
		}
		msg := err.Error()
		for _, w := range c.want {
			if !strings.Contains(msg, w) {
				t.Errorf("%s: message lacks %q: %s", c.name, w, msg)
			}
		}
		for _, w := range c.wantNot {
			if strings.Contains(msg, w) {
				t.Errorf("%s: message still says %q: %s", c.name, w, msg)
			}
		}
		// No assertion that every message names the file: validate() has no
		// path to name, and main.go already prefixes "weights <path>: " when it
		// reports a -weights failure. Only the parse errors carry it, because
		// Parse is what a library caller would see.
	}
}

// TestLoadErrorsSayWhatWasBeingLoaded covers the one error path Load produces on its own.
//
// Load returns os.ReadFile's error untouched for a missing or unreadable file, and that error
// names the path but not what the program was doing with it: "open /x.json: no such file or
// directory" would be printed for a weights file, an image and a CSV alike. main.go used to
// supply the missing context by prefixing the path itself, which then doubled the prefix on
// every error Parse returns — "weights /x.json: weights: the file parsed but has no
// configuration" — so the prefix could not simply stay there.
//
// Every error this function returns now names the operation, which is what lets a caller print
// it verbatim. The test pins both halves: the read failure says "read weights", and the parse
// failures keep their own single prefix.
func TestLoadErrorsSayWhatWasBeingLoaded(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.json")
	_, err := Load(missing)
	if err == nil {
		t.Fatal("Load accepted a path that does not exist")
	}
	msg := err.Error()
	if !strings.Contains(msg, "read weights") {
		t.Fatalf("the error does not say what was being read: %q", msg)
	}
	if !strings.Contains(msg, missing) {
		t.Fatalf("the error does not name the path: %q", msg)
	}
	// And exactly one level of context: the doubled prefix is what this fix removed, so a
	// message that counts "weights" twice has regressed.
	if n := strings.Count(msg, "weights"); n != 1 {
		t.Fatalf("the error names weights %d times, so the caller's prefix has come back: %q", n, msg)
	}

	// The parse side is unchanged: one prefix, produced by Parse rather than by the caller.
	empty := filepath.Join(dir, "empty.json")
	if writeErr := os.WriteFile(empty, nil, 0o644); writeErr != nil {
		t.Fatal(writeErr)
	}
	_, err = Load(empty)
	if err == nil {
		t.Fatal("Load accepted an empty file")
	}
	if got := strings.Count(err.Error(), "weights"); got != 1 {
		t.Fatalf("the empty-file error names weights %d times: %q", got, err.Error())
	}
}

// TestWhichConfigFieldsWeightsCanVary records the reachability of every config dimension the app
// reasons about, because two of its guards rest on claims about it.
//
// internal/app has one "no stage names the bundle's <field>" guard per config dimension, each
// sweeping the app by rendering it on a modified copy. This test answers the prior question for
// every field: can that copy be loaded? Round-tripping each through json.Marshal and Parse:
//
//	d_model, d_ff, ctx, n_head, n_layer  → nil error, so those copies are states -weights can
//	                                        produce and their sweeps check something a reader sees
//	vocab                                → rejected by Weights.validate
//
// vocab is the exception, and it is the one the app tests had wrong: TestNoStageNamesTheBundleVocabulary
// sweeps a 128-wide vocabulary that no reader can arrive at, because the tokenizer is byte-level and
// prompt ids only run 0..255. That guard is still worth keeping — it stops one card saying the same
// quantity two ways — but its comment now says the state is unreachable, and this test is what keeps
// that claim honest in both directions.
//
// A failure here means a comment elsewhere needs rewriting, so each case names the direction:
// a reachable field that stopped loading makes some app sweep moot, and a lifted vocab pin makes
// the vocab guard's caveat false. Anyone who gives the tokenizer a real vocabulary will see the
// vocab case fail, which is the intended signal to go and rewrite that comment.
func TestWhichConfigFieldsWeightsCanVary(t *testing.T) {
	base, err := Bundled()
	if err != nil {
		t.Skipf("no bundled weights: %v", err)
	}

	// vocab: rejected, and the message has to name both numbers so a reader knows what to fix.
	w := *base
	w.Config.Vocab = base.Config.Vocab / 2
	w.TokEmbed = base.TokEmbed[:w.Config.Vocab*base.Config.DModel]
	data, err := json.Marshal(&w)
	if err != nil {
		t.Fatal(err)
	}
	_, perr := Parse(data, "test")
	if perr == nil {
		t.Fatalf("Parse accepted vocab %d; the byte-level tokenizer only produces ids 0..%d, so "+
			"either the tokenizer learned a real vocabulary or this restriction was lifted — "+
			"in both cases the vocab sweep in internal/app is reachable now and its comment "+
			"needs rewriting", w.Config.Vocab, ByteVocab-1)
	}
	for _, want := range []string{strconv.Itoa(w.Config.Vocab), strconv.Itoa(ByteVocab)} {
		if !strings.Contains(perr.Error(), want) {
			t.Fatalf("the vocab error does not name %s: %v", want, perr)
		}
	}

	// The four fields the app sweeps that a reader *can* load. Kept here as one list so the
	// asymmetry is stated in one place: adding a reachable field to the app's guards means
	// adding it here, and lifting the vocab pin means deleting from this list or moving vocab
	// into it.
	//
	// n_layer and d_model are listed too. Neither has a "no stage names the bundle's <field>"
	// sweep — TestCardsThatCompareLayersNeedTwoOfThem explains why n_layer cannot have one, and
	// d_model's sweep is on the list above — but both are states -weights can produce, so a
	// future guard for either would be checking something real. Measured rather than assumed:
	// both return a nil error here.
	reachable := []struct {
		name string
		make func(*Weights) *Weights
	}{
		{"n_layer", func(b *Weights) *Weights {
			// A one-layer checkpoint: drop the second layer's tensors along with the count.
			w := *b
			w.Config = b.Config
			w.Config.NLayer = 1
			w.Layers = b.Layers[:1]
			return &w
		}},
		{"ctx", func(b *Weights) *Weights {
			w := *b
			w.Config = b.Config
			w.Config.Ctx = b.Config.Ctx / 2
			w.PosEmbed = b.PosEmbed[:w.Config.Ctx*b.Config.DModel]
			return &w
		}},
		{"n_head", func(b *Weights) *Weights {
			w := *b
			w.Config = b.Config
			w.Config.NHead = 2
			return &w
		}},
		{"d_ff", func(b *Weights) *Weights {
			w := *b
			w.Config = b.Config
			w.Config.DFF = b.Config.DFF / 2
			w.Layers = nil
			for _, l := range b.Layers {
				c := l
				c.W1 = make([]float32, w.Config.DModel*w.Config.DFF)
				c.W2 = make([]float32, w.Config.DFF*w.Config.DModel)
				copy(c.W1, l.W1)
				copy(c.W2, l.W2)
				w.Layers = append(w.Layers, c)
			}
			return &w
		}},
		{"d_model", func(b *Weights) *Weights {
			// Every tensor is resliced, which is what narrowWeights does for the app sweep.
			d := b.Config.DModel / 2
			cut := func(src []float32, rows, old, row int) []float32 {
				out := make([]float32, rows*row)
				for i := 0; i < rows; i++ {
					copy(out[i*row:(i+1)*row], src[i*old:i*old+row])
				}
				return out
			}
			w := *b
			w.Config = b.Config
			w.Config.DModel = d
			w.TokEmbed = cut(b.TokEmbed, b.Config.Vocab, b.Config.DModel, d)
			w.PosEmbed = cut(b.PosEmbed, b.Config.Ctx, b.Config.DModel, d)
			w.LNFG = append([]float32{}, b.LNFG[:d]...)
			w.LNFB = append([]float32{}, b.LNFB[:d]...)
			w.Layers = nil
			for _, l := range b.Layers {
				c := l
				c.LN1G = append([]float32{}, l.LN1G[:d]...)
				c.LN1B = append([]float32{}, l.LN1B[:d]...)
				c.LN2G = append([]float32{}, l.LN2G[:d]...)
				c.LN2B = append([]float32{}, l.LN2B[:d]...)
				c.WQ = cut(l.WQ, d, b.Config.DModel, d)
				c.WK = cut(l.WK, d, b.Config.DModel, d)
				c.WV = cut(l.WV, d, b.Config.DModel, d)
				c.WO = cut(l.WO, d, b.Config.DModel, d)
				c.W1 = cut(l.W1, d, b.Config.DModel, b.Config.DFF)
				c.W2 = make([]float32, b.Config.DFF*d)
				for r := 0; r < b.Config.DFF; r++ {
					copy(c.W2[r*d:(r+1)*d], l.W2[r*b.Config.DModel:])
				}
				w.Layers = append(w.Layers, c)
			}
			return &w
		}},
	}
	for _, c := range reachable {
		data, err := json.Marshal(c.make(base))
		if err != nil {
			t.Fatalf("%s: marshal: %v", c.name, err)
		}
		if _, err := Parse(data, "test"); err != nil {
			t.Fatalf("Parse rejected a %s copy (%v), so the app's %s sweep renders a state no "+
				"reader can load; its guard comment needs the same caveat the vocab one now "+
				"carries", c.name, err, c.name)
		}
	}
}

// TestValidateReportsTheSameTensorEveryTime pins the error's determinism.
//
// The per-layer tensor check used to iterate a map, so a file whose layer carried no tensors at
// all reported whichever name the runtime reached first: measured, 200 parses produced ten
// different messages, "w2 has 0 values, want 36864" on one run and "ln1_b has 0 values, want 96"
// on the next. A reader uses the message to decide what to fix, so it cannot depend on map
// ordering — and the useful answer for "everything is missing" is the largest tensor, which is
// what the fixed order reports.
func TestValidateReportsTheSameTensorEveryTime(t *testing.T) {
	w, err := Bundled()
	if err != nil {
		t.Fatalf("load the bundled weights: %v", err)
	}
	raw, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	layers, ok := doc["layers"].([]any)
	if !ok || len(layers) == 0 {
		t.Fatal("the marshalled weights have no layers to damage")
	}
	first := layers[0].(map[string]any)
	for _, k := range []string{
		"ln1_g", "ln1_b", "wq", "wk", "wv", "wo", "ln2_g", "ln2_b", "w1", "w2",
	} {
		first[k] = []any{}
	}
	bad, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}

	// The same input, parsed many times: one message, and it names the largest tensor.
	const runs = 50
	seen := map[string]bool{}
	for i := 0; i < runs; i++ {
		_, err := Parse(bad, "damaged")
		if err == nil {
			t.Fatal("a layer with no tensors parsed without error")
		}
		seen[err.Error()] = true
	}
	if len(seen) != 1 {
		msgs := make([]string, 0, len(seen))
		for m := range seen {
			msgs = append(msgs, m)
		}
		sort.Strings(msgs)
		t.Fatalf("%d parses of the same file gave %d different messages:\n  %s",
			runs, len(seen), strings.Join(msgs, "\n  "))
	}
	got := ""
	for m := range seen {
		got = m
	}
	if !strings.Contains(got, " w1 has ") {
		t.Fatalf("the message names a tensor other than the largest (%q); a file missing every "+
			"tensor should point at the one whose absence matters most", got)
	}
}
