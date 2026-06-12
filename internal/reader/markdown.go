package reader

import (
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
)

// newConverter returns a Converter preconfigured with the plugins that match
// typical web content. The domain (for resolving relative URLs) is applied
// per-call via WithDomain, so the converter itself stays domain-agnostic.
func newConverter() *converter.Converter {
	return converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(
				commonmark.WithStrongDelimiter("**"),
				commonmark.WithEmDelimiter("_"),
				commonmark.WithBulletListMarker("-"),
				commonmark.WithHeadingStyle(commonmark.HeadingStyleATX),
				commonmark.WithCodeBlockFence("```"),
				commonmark.WithHorizontalRule("---"),
			),
			strikethrough.NewStrikethroughPlugin(),
			table.NewTablePlugin(
				table.WithHeaderPromotion(true),
				table.WithSkipEmptyRows(true),
			),
		),
		converter.WithEscapeMode(converter.EscapeModeSmart),
	)
}

// HTMLToMarkdown converts a full HTML document or fragment to markdown,
// resolving relative URLs against baseURL. An empty baseURL leaves relative
// URLs untouched. For convenience a base set of plugins (commonmark, tables,
// strikethrough) is always enabled.
func HTMLToMarkdown(html, baseURL string) (string, error) {
	conv := newConverter()
	if baseURL == "" {
		return conv.ConvertString(html)
	}
	return conv.ConvertString(html, converter.WithDomain(baseURL))
}

// Ensure the htmltomarkdown import is referenced for IDE/links; the package
// alias above keeps us flexible if we want to switch to htmltomarkdown.* APIs.
var _ = htmltomarkdown.ConvertString
