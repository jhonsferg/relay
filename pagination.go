package relay

import (
	"context"
	"regexp"
)

// PageFunc is called for each page of results. It receives the response for
// that page. Return (true, nil) to continue to the next page, (false, nil)
// to stop, or (false, err) to abort with an error.
type PageFunc func(resp *Response) (more bool, err error)

// NextPageFunc extracts the URL for the next page from a response.
// Return an empty string to signal the last page.
type NextPageFunc func(resp *Response) string

// linkNextRe matches a Link header entry with rel="next".
// Compiled once at package init to avoid repeated regexp compilation.
var linkNextRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// linkHeaderNextPage extracts the next page URL from a Link response header
// per RFC 5988: Link: <https://api.example.com/items?page=2>; rel="next"
func linkHeaderNextPage(resp *Response) string {
	link := resp.Headers.Get("Link")
	if link == "" {
		return ""
	}
	m := linkNextRe.FindStringSubmatch(link)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// Paginate iterates through pages of results, calling fn for each page.
// It follows the Link header (rel="next") by default. Use PaginateWith for
// custom next-page extraction.
//
//	err := client.Paginate(ctx, client.Get("/items"), func(resp *relay.Response) (bool, error) {
//	    var items []Item
//	    if err := resp.JSON(&items); err != nil {
//	        return false, err
//	    }
//	    process(items)
//	    return true, nil
//	})
func (c *Client) Paginate(ctx context.Context, req *Request, fn PageFunc) error {
	return c.PaginateWith(ctx, req, linkHeaderNextPage, fn)
}

// PaginateWith iterates through pages of results using a custom next-page
// extractor. nextFn is called after each page to get the URL for the next
// page; an empty string signals the end.
func (c *Client) PaginateWith(ctx context.Context, req *Request, nextFn NextPageFunc, fn PageFunc) error {
	req = req.WithContext(ctx)
	for {
		resp, err := c.Execute(req)
		if err != nil {
			return err
		}
		more, fnErr := fn(resp)
		if fnErr != nil {
			return fnErr
		}
		if !more {
			return nil
		}
		nextURL := nextFn(resp)
		if nextURL == "" {
			return nil
		}
		req = c.Get(nextURL)
		req = req.WithContext(ctx)
	}
}
