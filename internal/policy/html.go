package policy

import (
	"bytes"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const MaxHTMLBytes = 512 * 1024

var blockedTags = map[string]bool{
	"form": true, "iframe": true, "object": true, "embed": true,
	"applet": true, "base": true, "link": true,
}

var urlAttributes = map[string]bool{
	"href": true, "src": true, "action": true, "formaction": true,
	"poster": true, "srcdoc": true, "xlink:href": true,
}

type HTMLResult struct {
	Title              string
	HasInlineScript    bool
	ExternalImageHosts []string
	Warnings           []string
	Errors             []string
}

func ValidateHTML(content []byte) HTMLResult {
	result := HTMLResult{}
	if !utf8.Valid(content) {
		result.Errors = append(result.Errors, "HTML document is not valid UTF-8.")
		return result
	}
	if strings.TrimSpace(string(content)) == "" {
		result.Errors = append(result.Errors, "HTML document is empty.")
		return result
	}
	if len(content) > MaxHTMLBytes {
		result.Errors = append(result.Errors, fmt.Sprintf("HTML document is %d bytes; maximum is %d bytes.", len(content), MaxHTMLBytes))
	}
	document, err := html.Parse(bytes.NewReader(content))
	if err != nil {
		result.Errors = append(result.Errors, "HTML document could not be parsed.")
		return result
	}
	hosts := make(map[string]bool)
	stack := []nodeDepth{{document, 0}}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current.depth > 512 {
			result.Errors = append(result.Errors, "HTML is nested more than 512 levels deep.")
			continue
		}
		node := current.node
		if node.Type == html.ElementNode {
			tag := strings.ToLower(node.Data)
			if blockedTags[tag] {
				result.Errors = append(result.Errors, fmt.Sprintf("Blocked <%s> tag found.", tag))
			}
			attrs := attributes(node)
			if tag == "script" {
				result.HasInlineScript = true
				if _, ok := attrs["src"]; ok {
					result.Errors = append(result.Errors, "External script sources are not allowed.")
				}
				typ := strings.ToLower(strings.TrimSpace(attrs["type"]))
				if typ != "" && typ != "text/javascript" && typ != "application/javascript" {
					result.Errors = append(result.Errors, fmt.Sprintf("Unsupported script type %q found.", typ))
				}
			}
			for name, value := range attrs {
				if strings.HasPrefix(name, "on") {
					result.Errors = append(result.Errors, fmt.Sprintf("Blocked inline event handler attribute %q found.", name))
				}
				if name == "srcdoc" {
					result.Errors = append(result.Errors, "Blocked \"srcdoc\" attribute found.")
				}
				if urlAttributes[name] && unsafeURL(value) {
					result.Errors = append(result.Errors, fmt.Sprintf("Blocked unsafe URL in %q attribute.", name))
				}
				if name == "style" && unsafeCSS(value) {
					result.Errors = append(result.Errors, "Blocked unsafe inline CSS.")
				}
			}
			if tag == "style" && unsafeCSS(textContent(node)) {
				result.Errors = append(result.Errors, "Blocked unsafe inline CSS.")
			}
			if tag == "meta" && strings.EqualFold(strings.TrimSpace(attrs["http-equiv"]), "refresh") {
				result.Errors = append(result.Errors, "Blocked meta refresh tag found.")
			}
			if tag == "img" {
				if host := externalHost(attrs["src"]); host != "" {
					hosts[host] = true
				}
			}
			if tag == "title" && result.Title == "" {
				result.Title = truncateRunes(strings.TrimSpace(textContent(node)), 140)
			}
		}
		for child := node.LastChild; child != nil; child = child.PrevSibling {
			stack = append(stack, nodeDepth{child, current.depth + 1})
		}
	}
	if result.Title == "" {
		result.Warnings = append(result.Warnings, "No <title> found; PageFerry will use a generic title.")
	}
	result.Errors = unique(result.Errors)
	result.Warnings = unique(result.Warnings)
	for host := range hosts {
		result.ExternalImageHosts = append(result.ExternalImageHosts, host)
	}
	sort.Strings(result.ExternalImageHosts)
	return result
}

type nodeDepth struct {
	node  *html.Node
	depth int
}

func attributes(node *html.Node) map[string]string {
	result := make(map[string]string, len(node.Attr))
	for _, attribute := range node.Attr {
		result[strings.ToLower(attribute.Key)] = strings.TrimSpace(attribute.Val)
	}
	return result
}

func textContent(node *html.Node) string {
	var builder strings.Builder
	var visit func(*html.Node)
	visit = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return builder.String()
}

func unsafeURL(value string) bool {
	normalized := strings.Map(func(r rune) rune {
		if r <= 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.ToLower(value))
	return strings.HasPrefix(normalized, "javascript:") || strings.HasPrefix(normalized, "vbscript:") || strings.HasPrefix(normalized, "file:")
}

func unsafeCSS(value string) bool {
	normalized := strings.ToLower(value)
	compact := strings.Map(func(r rune) rune {
		if r <= 0x20 {
			return -1
		}
		return r
	}, normalized)
	return strings.Contains(compact, "behavior:") || strings.Contains(compact, "expression(") || strings.Contains(compact, "url(javascript:")
}

func externalHost(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "//") {
		value = "https:" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
