package pruning

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"

	"github.com/sentrysurface/surface-proxy/internal/config"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// TelemetryRecorder is satisfied by telemetry.Ledger and allows the pruner
// to emit metrics without importing the telemetry package directly (avoiding
// potential import cycles if telemetry ever imports pruning types).
type TelemetryRecorder interface {
	RecordPrune(sessionID string, rawBytes, prunedBytes int)
}

type Pruner struct {
	cfg       atomic.Pointer[config.PruningConfig]
	telemetry TelemetryRecorder // nil = disabled
}

func NewPruner(cfg config.PruningConfig) *Pruner {
	p := &Pruner{}
	p.UpdateConfig(cfg)
	return p
}

// SetTelemetry attaches a telemetry recorder. Thread-safe; can be called after construction.
func (p *Pruner) SetTelemetry(t TelemetryRecorder) {
	p.telemetry = t
}

func (p *Pruner) UpdateConfig(cfg config.PruningConfig) {
	p.cfg.Store(&cfg)
}

type SemanticNode struct {
	Tag        string            `json:"tag"`
	Text       string            `json:"text,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Prune tokenizes htmlData and returns a semantic Markdown or JSON representation.
// sessionID is used to associate telemetry with a session; pass empty string to skip.
func (p *Pruner) Prune(htmlData []byte) ([]byte, error) {
	return p.PruneWithSession(htmlData, "")
}

// PruneWithSession is like Prune but records telemetry under the given session ID.
func (p *Pruner) PruneWithSession(htmlData []byte, sessionID string) ([]byte, error) {
	cfg := p.cfg.Load()
	stripTags := make(map[string]bool)
	outputFormat := "markdown"
	if cfg != nil {
		outputFormat = strings.ToLower(cfg.OutputFormat)
		for _, tag := range cfg.StripTags {
			stripTags[strings.ToLower(tag)] = true
		}
	}

	tokenizer := html.NewTokenizer(bytes.NewReader(htmlData))

	var result []byte
	var err error
	if outputFormat == "json" {
		result, err = p.pruneToJSON(tokenizer, stripTags)
	} else {
		result, err = p.pruneToMarkdown(tokenizer, stripTags)
	}

	if err == nil && p.telemetry != nil && sessionID != "" {
		p.telemetry.RecordPrune(sessionID, len(htmlData), len(result))
	}

	return result, err
}

func (p *Pruner) pruneToJSON(tokenizer *html.Tokenizer, stripTags map[string]bool) ([]byte, error) {
	var nodes []SemanticNode
	var skipTagDepth int
	var currentSkipTag string

	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			err := tokenizer.Err()
			if err == io.EOF {
				break
			}
			return nil, err
		}

		token := tokenizer.Token()
		tagName := strings.ToLower(token.Data)

		if skipTagDepth > 0 {
			if tt == html.EndTagToken && tagName == currentSkipTag {
				skipTagDepth--
				if skipTagDepth == 0 {
					currentSkipTag = ""
				}
			} else if tt == html.StartTagToken && tagName == currentSkipTag {
				skipTagDepth++
			}
			continue
		}

		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			if stripTags[tagName] {
				skipTagDepth = 1
				currentSkipTag = tagName
				continue
			}

			isInteractive := isInteractiveTag(token.DataAtom) || hasInteractiveAttrs(token.Attr)
			if isInteractive || isTextWrapperTag(token.DataAtom) {
				node := SemanticNode{
					Tag: tagName,
				}
				if len(token.Attr) > 0 {
					node.Attributes = make(map[string]string)
					for _, attr := range token.Attr {
						k := strings.ToLower(attr.Key)
						if k == "id" || k == "name" || k == "href" || k == "placeholder" || k == "value" || k == "type" || strings.HasPrefix(k, "data-") {
							node.Attributes[attr.Key] = attr.Val
						}
					}
				}
				// Gather next text if available
				nextTT := tokenizer.Next()
				if nextTT == html.TextToken {
					textToken := tokenizer.Token()
					trimmed := strings.TrimSpace(textToken.Data)
					if trimmed != "" {
						node.Text = trimmed
					}
				}
				nodes = append(nodes, node)
			}
		}
	}

	return json.Marshal(nodes)
}

func (p *Pruner) pruneToMarkdown(tokenizer *html.Tokenizer, stripTags map[string]bool) ([]byte, error) {
	outBuf := GetBuffer()
	defer PutBuffer(outBuf)

	var skipTagDepth int
	var currentSkipTag string

	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			err := tokenizer.Err()
			if err == io.EOF {
				break
			}
			return nil, err
		}

		token := tokenizer.Token()
		tagName := strings.ToLower(token.Data)

		if skipTagDepth > 0 {
			if tt == html.EndTagToken && tagName == currentSkipTag {
				skipTagDepth--
				if skipTagDepth == 0 {
					currentSkipTag = ""
				}
			} else if tt == html.StartTagToken && tagName == currentSkipTag {
				skipTagDepth++
			}
			continue
		}

		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			if stripTags[tagName] {
				skipTagDepth = 1
				currentSkipTag = tagName
				continue
			}

			isInteractive := isInteractiveTag(token.DataAtom) || hasInteractiveAttrs(token.Attr)

			if isInteractive {
				writeInteractiveStart(outBuf, token)
			} else if isTextWrapperTag(token.DataAtom) {
				writeTextWrapperStart(outBuf, token.DataAtom)
			}
		} else if tt == html.EndTagToken {
			if isTextWrapperTag(token.DataAtom) {
				writeTextWrapperEnd(outBuf, token.DataAtom)
			} else if isInteractiveTag(token.DataAtom) {
				outBuf.WriteString(" ")
			}
		} else if tt == html.TextToken {
			trimmed := strings.TrimSpace(token.Data)
			if trimmed != "" {
				outBuf.WriteString(trimmed)
				outBuf.WriteString(" ")
			}
		}
	}

	res := make([]byte, outBuf.Len())
	copy(res, outBuf.Bytes())
	return res, nil
}

func isInteractiveTag(a atom.Atom) bool {
	switch a {
	case atom.A, atom.Button, atom.Input, atom.Select, atom.Textarea, atom.Form, atom.Option, atom.Label:
		return true
	}
	return false
}

func isTextWrapperTag(a atom.Atom) bool {
	switch a {
	case atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Div, atom.Li, atom.Ul, atom.Ol:
		return true
	}
	return false
}

func hasInteractiveAttrs(attrs []html.Attribute) bool {
	for _, attr := range attrs {
		name := strings.ToLower(attr.Key)
		if name == "onclick" || name == "href" || strings.HasPrefix(name, "data-") || name == "role" {
			return true
		}
	}
	return false
}

func writeInteractiveStart(buf *bytes.Buffer, token html.Token) {
	buf.WriteString("\n[")
	buf.WriteString(token.Data)
	for _, attr := range token.Attr {
		k := strings.ToLower(attr.Key)
		if k == "id" || k == "name" || k == "href" || k == "placeholder" || k == "value" || k == "type" {
			buf.WriteString(" ")
			buf.WriteString(attr.Key)
			buf.WriteString("=\"")
			buf.WriteString(attr.Val)
			buf.WriteString("\"")
		}
	}
	buf.WriteString("] ")
}

func writeTextWrapperStart(buf *bytes.Buffer, a atom.Atom) {
	switch a {
	case atom.H1:
		buf.WriteString("\n# ")
	case atom.H2:
		buf.WriteString("\n## ")
	case atom.H3:
		buf.WriteString("\n### ")
	case atom.H4, atom.H5, atom.H6:
		buf.WriteString("\n#### ")
	case atom.P, atom.Div:
		buf.WriteString("\n")
	case atom.Li:
		buf.WriteString("\n- ")
	}
}

func writeTextWrapperEnd(buf *bytes.Buffer, a atom.Atom) {
	buf.WriteString("\n")
}
