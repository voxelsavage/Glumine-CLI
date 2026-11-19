package main

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

type syntaxSpan struct {
	end   int
	color string
}
type symbol struct {
	row  int
	name string
}
type syntaxReady struct {
	owner   *codeEditor
	version uint64
	spans   []syntaxSpan
	symbols []symbol
}

func fileLexer(path string) chroma.Lexer {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mcmeta":
		return lexers.Get("json")
	case ".kts":
		return lexers.Get("kotlin")
	case ".properties":
		return lexers.Get("properties")
	}
	return lexers.Match(filepath.Base(path))
}
func tokenColor(t chroma.TokenType) string {
	switch t {
	case chroma.NameFunction, chroma.NameClass, chroma.NameBuiltin:
		return "#A5CAFF"
	case chroma.NameTag, chroma.NameAttribute, chroma.NameProperty:
		return "#8FE1C6"
	case chroma.KeywordConstant, chroma.NameConstant:
		return "#ECA77D"
	}
	switch {
	case t.InCategory(chroma.Comment):
		return "#8A9DAA"
	case t.InCategory(chroma.Keyword):
		return "#C5AEF4"
	case t.SubCategory() == chroma.LiteralString:
		return "#B7DCA2"
	case t.SubCategory() == chroma.LiteralNumber:
		return "#F2BF86"
	case t.InCategory(chroma.Operator), t.InCategory(chroma.Punctuation):
		return "#A0B0C3"
	default:
		return ink
	}
}

var outlinePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\s*(?:export\s+)?(?:(?:public|private|protected|static|async|abstract)\s+)*(?:func(?:\s+\([^)]*\))?|def|class|interface|struct|function|fn|type)\s+([\pL_][\pL\d_.$]*)`),
	regexp.MustCompile(`^\s*\[+([^\[\]]+)\]+\s*$`),
	regexp.MustCompile(`^\s*"([^"]+)"\s*:\s*[[{]`),
	regexp.MustCompile(`^([\pL_][\pL\d_.-]*)\s*[:=]`),
	regexp.MustCompile(`^#{1,6}\s+(.+)$`),
}

func scanSyntax(e *codeEditor, version uint64, text string, lexer chroma.Lexer) (r syntaxReady) {
	r = syntaxReady{owner: e, version: version}
	row := 0
	for start := 0; start < len(text); row++ {
		n := strings.IndexAny(text[start:], "\r\n")
		if n < 0 {
			n = len(text) - start
		}
		line := text[start : start+n]
		for _, pat := range outlinePatterns {
			if match := pat.FindStringSubmatch(line); match != nil {
				r.symbols = append(r.symbols, symbol{row, match[1]})
				break
			}
		}
		start += n
		if start < len(text) {
			if strings.HasPrefix(text[start:], "\r\n") {
				start++
			}
			start++
		}
	}
	if lexer == nil {
		return
	}
	iter, err := lexer.Tokenise(&chroma.TokeniseOptions{State: "root"}, text)
	if err != nil {
		return
	}
	at := 0
	for t := iter(); t != chroma.EOF; t = iter() {
		n := min(len(t.Value), len(text)-at)
		if n == 0 {
			continue
		}
		if t.Value[:n] != text[at:at+n] {
			break
		}
		at += n
		color := tokenColor(t.Type)
		if len(r.spans) > 0 && r.spans[len(r.spans)-1].color == color {
			r.spans[len(r.spans)-1].end = at
		} else {
			r.spans = append(r.spans, syntaxSpan{at, color})
		}
	}
	return
}
