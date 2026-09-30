package policy

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const MaxHTMLBytes = 512 * 1024

var blockedTags = map[string]bool{
	"iframe": true, "object": true, "embed": true,
	"applet": true, "base": true,
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

// HTMLOptions adjusts the document policy. Site bundles serve their own scripts
// from the draft origin, so they may reference same-origin script paths.
type HTMLOptions struct {
	AllowRelativeScripts bool
}

func ValidateHTML(content []byte) HTMLResult {
	return ValidateHTMLWithOptions(content, HTMLOptions{})
}

func ValidateHTMLWithOptions(content []byte, options HTMLOptions) HTMLResult {
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
	mobile := mobileSignals{}
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
				if src, ok := attrs["src"]; ok && !isHTTPSURL(src) && !(options.AllowRelativeScripts && isRelativeURL(src)) {
					result.Errors = append(result.Errors, "External script sources must use HTTPS.")
				}
				typ := strings.ToLower(strings.TrimSpace(attrs["type"]))
				if typ != "" && typ != "text/javascript" && typ != "application/javascript" && typ != "module" {
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
				if name == "style" {
					mobile.css = append(mobile.css, value)
					if formControlTags[tag] && hasSmallFontSize(value) {
						mobile.smallFormControlFont = true
					}
				}
				if tag == "html" && name == "lang" && value != "" {
					mobile.htmlLang = true
				}
			}
			if tag == "style" {
				css := textContent(node)
				if unsafeCSS(css) {
					result.Errors = append(result.Errors, "Blocked unsafe inline CSS.")
				}
				mobile.css = append(mobile.css, css)
				if hasSmallFormControlRule(css) {
					mobile.smallFormControlFont = true
				}
			}
			if tag == "meta" && strings.EqualFold(strings.TrimSpace(attrs["http-equiv"]), "refresh") {
				result.Errors = append(result.Errors, "Blocked meta refresh tag found.")
			}
			if tag == "meta" && strings.EqualFold(strings.TrimSpace(attrs["name"]), "viewport") {
				mobile.viewports = append(mobile.viewports, attrs["content"])
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
	result.Warnings = append(result.Warnings, mobileWarnings(mobile)...)
	result.Errors = unique(result.Errors)
	result.Warnings = unique(result.Warnings)
	for host := range hosts {
		result.ExternalImageHosts = append(result.ExternalImageHosts, host)
	}
	sort.Strings(result.ExternalImageHosts)
	return result
}

// Mobile checks are advisory. They mirror server/src/html-policy.ts in the
// Worker so `pageferry validate` and the upload API report the same warnings.
type mobileSignals struct {
	viewports            []string
	css                  []string
	smallFormControlFont bool
	htmlLang             bool
}

var formControlTags = map[string]bool{"input": true, "select": true, "textarea": true}

const fixedWidthPx = 600

const (
	WarningNoViewport           = `No <meta name="viewport"> found; phones will render the page at desktop width. Add <meta name="viewport" content="width=device-width, initial-scale=1">.`
	WarningZoomDisabled         = "Viewport disables zooming (user-scalable=no or maximum-scale below 5); readers cannot zoom in."
	WarningNoInitialScale       = "Viewport lacks initial-scale=1; iOS Safari can stay zoomed out after rotation."
	WarningFixedWidth           = "CSS sets a fixed width of 600px or more; the page will scroll sideways on phones. Prefer max-width or relative units."
	WarningSmallFormControlFont = "Form controls use a font-size below 16px; iOS Safari zooms in when they are focused."
	WarningViewportHeight       = "CSS uses 100vh without an svh or dvh alternative; full-height elements overflow under the iOS Safari toolbar."
	WarningNoLang               = "No <html lang> attribute found; screen readers and translation cannot detect the document language."
)

var (
	cssCommentPattern     = regexp.MustCompile(`/\*[\s\S]*?\*/`)
	fixedWidthPattern     = regexp.MustCompile(`(?i)(?:^|[\s;{])(?:min-)?width\s*:\s*(\d+(?:\.\d+)?)px`)
	cssRulePattern        = regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)
	formControlSelector   = regexp.MustCompile(`(?i)(?:^|[^\w-])(?:input|select|textarea)(?:$|[^\w-])`)
	fontSizePattern       = regexp.MustCompile(`(?i)(?:^|[\s;{])font(?:-size)?\s*:[^;]*?(\d*\.?\d+)(px|rem|em|pt)\b`)
	viewportHeightPattern = regexp.MustCompile(`(?i)(?:^|[^\w.])100vh\b`)
	smallViewportPattern  = regexp.MustCompile(`(?i)\d[sd]vh\b`)
)

func mobileWarnings(signals mobileSignals) []string {
	var warnings []string
	if len(signals.viewports) == 0 {
		warnings = append(warnings, WarningNoViewport)
	}
	for _, content := range signals.viewports {
		viewport := parseViewport(content)
		userScalable := viewport["user-scalable"]
		maximumScale, maximumErr := strconv.ParseFloat(viewport["maximum-scale"], 64)
		if userScalable == "no" || userScalable == "0" || (maximumErr == nil && maximumScale < 5) {
			warnings = append(warnings, WarningZoomDisabled)
		}
		if initialScale, err := strconv.ParseFloat(viewport["initial-scale"], 64); err != nil || initialScale != 1 {
			warnings = append(warnings, WarningNoInitialScale)
		}
	}
	css := cssCommentPattern.ReplaceAllString(strings.Join(signals.css, "\n"), "")
	if hasFixedWidth(css) {
		warnings = append(warnings, WarningFixedWidth)
	}
	if signals.smallFormControlFont {
		warnings = append(warnings, WarningSmallFormControlFont)
	}
	if viewportHeightPattern.MatchString(css) && !smallViewportPattern.MatchString(css) {
		warnings = append(warnings, WarningViewportHeight)
	}
	if !signals.htmlLang {
		warnings = append(warnings, WarningNoLang)
	}
	return warnings
}

func parseViewport(content string) map[string]string {
	result := make(map[string]string)
	for _, part := range strings.FieldsFunc(content, func(r rune) bool { return r == ',' || r == ';' }) {
		key, value, _ := strings.Cut(part, "=")
		name := strings.ToLower(strings.TrimSpace(key))
		if name != "" {
			result[name] = strings.ToLower(strings.TrimSpace(value))
		}
	}
	return result
}

func hasFixedWidth(css string) bool {
	for _, match := range fixedWidthPattern.FindAllStringSubmatch(css, -1) {
		if width, err := strconv.ParseFloat(match[1], 64); err == nil && width >= fixedWidthPx {
			return true
		}
	}
	return false
}

func hasSmallFormControlRule(css string) bool {
	for _, match := range cssRulePattern.FindAllStringSubmatch(cssCommentPattern.ReplaceAllString(css, ""), -1) {
		if formControlSelector.MatchString(match[1]) && hasSmallFontSize(match[2]) {
			return true
		}
	}
	return false
}

// hasSmallFontSize matches font-size and the size component of the font shorthand.
func hasSmallFontSize(declarations string) bool {
	for _, match := range fontSizePattern.FindAllStringSubmatch(declarations, -1) {
		size, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(match[2]) {
		case "px":
			if size < 16 {
				return true
			}
		case "rem", "em":
			if size < 1 {
				return true
			}
		case "pt":
			if size < 12 {
				return true
			}
		}
	}
	return false
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

func isHTTPSURL(value string) bool {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "//") {
		value = "https:" + value
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func isRelativeURL(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "//") || strings.Contains(value, `\`) {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "" && parsed.Host == ""
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
