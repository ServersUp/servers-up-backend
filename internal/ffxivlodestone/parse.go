package ffxivlodestone

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// IconStatus is the Lodestone world status icon (not character creation).
type IconStatus int

const (
	IconOnline IconStatus = iota + 1
	IconPartialMaintenance
	IconMaintenance
)

func (s IconStatus) String() string {
	switch s {
	case IconOnline:
		return "online"
	case IconPartialMaintenance:
		return "partial_maintenance"
	case IconMaintenance:
		return "maintenance"
	default:
		return "unknown"
	}
}

// WorldEntry is one world row parsed from the world status page.
type WorldEntry struct {
	Name   string
	Region string // na, eu, jp, oce
	Icon   IconStatus
}

// dataRegionToSlug maps Lodestone data-region on tab panels to catalog regions.
var dataRegionToSlug = map[string]string{
	"1": "jp",
	"2": "na",
	"3": "eu",
	"4": "oce",
}

// ParseWorldStatusHTML extracts worlds from a Lodestone world status HTML document.
func ParseWorldStatusHTML(htmlBody []byte) ([]WorldEntry, error) {
	doc, err := html.Parse(bytes.NewReader(htmlBody))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	var items []*html.Node
	var collect func(*html.Node)
	collect = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "world-list__item") {
			items = append(items, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collect(c)
		}
	}
	collect(doc)

	if len(items) == 0 {
		return nil, fmt.Errorf("no world-list__item elements found")
	}

	var out []WorldEntry
	seen := make(map[string]struct{})
	for _, item := range items {
		region, err := regionForWorldItem(item)
		if err != nil {
			return nil, err
		}
		name, err := worldNameFromItem(item)
		if err != nil {
			return nil, err
		}
		icon, err := statusIconFromItem(item)
		if err != nil {
			return nil, err
		}
		key := region + "\x00" + name
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, WorldEntry{Name: name, Region: region, Icon: icon})
	}
	return out, nil
}

func regionForWorldItem(item *html.Node) (string, error) {
	for p := item.Parent; p != nil; p = p.Parent {
		if p.Type != html.ElementNode || p.Data != "div" {
			continue
		}
		if !hasClass(p, "js--tab-content") {
			continue
		}
		raw := attr(p, "data-region")
		region, ok := dataRegionToSlug[raw]
		if !ok {
			return "", fmt.Errorf("unknown data-region %q on tab panel", raw)
		}
		return region, nil
	}
	return "", fmt.Errorf("world item not under js--tab-content panel")
}

func worldNameFromItem(item *html.Node) (string, error) {
	nameDiv := findDescendant(item, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "world-list__world_name")
	})
	if nameDiv == nil {
		return "", fmt.Errorf("missing world-list__world_name")
	}
	text := strings.TrimSpace(textContent(nameDiv))
	if text == "" {
		return "", fmt.Errorf("empty world name")
	}
	return text, nil
}

func statusIconFromItem(item *html.Node) (IconStatus, error) {
	iconDiv := findDescendant(item, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "world-list__status_icon")
	})
	if iconDiv == nil {
		return 0, fmt.Errorf("missing world-list__status_icon")
	}
	var iEl *html.Node
	for c := iconDiv.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "i" {
			iEl = c
			break
		}
	}
	if iEl == nil {
		return 0, fmt.Errorf("missing status i element")
	}
	cls := attr(iEl, "class")
	switch {
	case hasToken(cls, "world-ic__1"):
		return IconOnline, nil
	case hasToken(cls, "world-ic__2"):
		return IconPartialMaintenance, nil
	case hasToken(cls, "world-ic__3"):
		return IconMaintenance, nil
	default:
		return 0, fmt.Errorf("unrecognized status icon class %q", cls)
	}
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	return hasToken(attr(n, "class"), class)
}

func hasToken(classAttr, token string) bool {
	for _, t := range strings.Fields(classAttr) {
		if t == token {
			return true
		}
	}
	return false
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func findDescendant(root *html.Node, pred func(*html.Node) bool) *html.Node {
	if pred(root) {
		return root
	}
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if found := findDescendant(c, pred); found != nil {
			return found
		}
	}
	return nil
}
