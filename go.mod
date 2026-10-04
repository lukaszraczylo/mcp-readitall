module github.com/lukaszraczylo/mcp-readitall

go 1.26.4

require (
	github.com/JohannesKaufmann/html-to-markdown/v2 v2.5.2
	github.com/chromedp/cdproto v0.157.2
	github.com/chromedp/chromedp v0.16.0
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/stretchr/testify v1.12.1
)

require (
	github.com/JohannesKaufmann/dom v0.3.1 // indirect
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/go-json-experiment/json v0.0.0-20260820222146-c27c302e5fc3 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/orisano/pixelmatch v0.0.0-20230914042517-fa304d1dc785 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/oauth2 v0.37.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/time v0.16.0 // indirect
)

// chromedp v0.16 builds against this cdproto; the v0.157 tag changed the input API.
replace github.com/chromedp/cdproto => github.com/chromedp/cdproto v0.0.0-20260714215040-dc233986426f
